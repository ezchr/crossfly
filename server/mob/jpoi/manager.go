package jpoi

import (
	"math"
	"sort"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Record is a PoI at a position (Java PoiRecord) with its free tickets.
type Record struct {
	Pos  cube.Pos
	Type *Type
	free int
}

// FreeTickets returns how many more mobs may claim the record.
func (r *Record) FreeTickets() int { return r.free }

// HasSpace is PoiRecord.hasSpace.
func (r *Record) HasSpace() bool { return r.free > 0 }

// IsOccupied is PoiRecord.isOccupied.
func (r *Record) IsOccupied() bool { return r.free != r.Type.MaxTickets }

func (r *Record) acquire() bool {
	if r.free <= 0 {
		return false
	}
	r.free--
	return true
}

func (r *Record) release() bool {
	if r.free >= r.Type.MaxTickets {
		return false
	}
	r.free++
	return true
}

// Occupancy is PoiManager.Occupancy.
type Occupancy uint8

const (
	HasSpace Occupancy = iota
	IsOccupied
	AnyOccupancy
)

func (o Occupancy) test(r *Record) bool {
	switch o {
	case HasSpace:
		return r.HasSpace()
	case IsOccupied:
		return r.IsOccupied()
	}
	return true
}

type sectionPos [3]int

func sectionOf(p cube.Pos) sectionPos { return sectionPos{p[0] >> 4, p[1] >> 4, p[2] >> 4} }

// section is a PoiSection: the records of one chunk section in the order
// they were added.
type section struct {
	records []*Record
}

func (s *section) find(p cube.Pos) (int, *Record) {
	for i, r := range s.records {
		if r.Pos == p {
			return i, r
		}
	}
	return -1, nil
}

// column is what the manager knows about a chunk column: when its blocks
// were last read.
type column struct {
	seq  uint64
	tick int64
}

// rescanTicks is how long a scanned column is trusted without looking at the
// change log's verdict again from scratch (the log may forget old changes,
// and BuildStructure changes are not logged per section).
const rescanTicks = 600

// Manager is Java's PoiManager for one world: the PoIs found in its blocks
// and their claims. Use it only inside a transaction of its world.
type Manager struct {
	sections map[sectionPos]*section
	columns  map[world.ChunkPos]*column
	// centers counts the occupied village PoIs per section (isVillageCenter).
	centers map[sectionPos]int
	minY    int
	maxY    int
	now     int64
	// Scan reads the PoI blocks of a chunk column; tests replace it.
	scan func(tx *world.Tx, c world.ChunkPos) map[cube.Pos]*Type
}

var managers sync.Map // *world.World -> *Manager

// For returns the manager of tx's world, making it on first use.
func For(tx *world.Tx) *Manager {
	w := tx.World()
	if m, ok := managers.Load(w); ok {
		return m.(*Manager)
	}
	r := w.Range()
	m := newManager(r[0], r[1])
	actual, _ := managers.LoadOrStore(w, m)
	return actual.(*Manager)
}

// Forget drops the manager of w (when the world closes).
func Forget(w *world.World) { managers.Delete(w) }

func newManager(minY, maxY int) *Manager {
	return &Manager{sections: map[sectionPos]*section{}, columns: map[world.ChunkPos]*column{}, centers: map[sectionPos]int{}, minY: minY, maxY: maxY, scan: scanColumn}
}

// NewTestManager returns a manager not bound to a world whose blocks come
// from scan (tests).
func NewTestManager(minY, maxY int, scan func(tx *world.Tx, c world.ChunkPos) map[cube.Pos]*Type) *Manager {
	m := newManager(minY, maxY)
	m.scan = scan
	return m
}

// SetTime tells the manager the game time (for the periodic rescans).
func (m *Manager) SetTime(t int64) { m.now = t }

func scanColumn(tx *world.Tx, c world.ChunkPos) map[cube.Pos]*Type {
	out := map[cube.Pos]*Type{}
	center := cube.Pos{int(c[0])<<4 + 8, 0, int(c[1])<<4 + 8}
	for p := range tx.BlocksWithin(center, 8, states()...) {
		if t, ok := TypeOf(tx.Block(p)); ok {
			out[p] = t
		}
	}
	return out
}

// refresh makes the records of column c match its blocks if they may have
// changed since the last scan (PoiSection.refresh / checkConsistency and
// ServerLevel.onBlockStateChange -> PoiManager.add/remove).
func (m *Manager) refresh(tx *world.Tx, c world.ChunkPos) {
	col := m.columns[c]
	if col != nil && m.now-col.tick < rescanTicks {
		if tx == nil {
			return
		}
		changed := false
		for sy := m.minY >> 4; sy <= m.maxY>>4 && !changed; sy++ {
			changed = tx.SectionChangedSince(cube.Pos{int(c[0]) << 4, sy << 4, int(c[1]) << 4}, col.seq)
		}
		if !changed {
			return
		}
	}
	if col == nil {
		col = &column{}
		m.columns[c] = col
	}
	if tx != nil {
		col.seq = tx.BlockChangeSeq()
	}
	col.tick = m.now
	found := m.scan(tx, c)
	// Remove records whose block is gone or changed type.
	for sy := m.minY >> 4; sy <= m.maxY>>4; sy++ {
		sp := sectionPos{int(c[0]), sy, int(c[1])}
		s := m.sections[sp]
		if s == nil {
			continue
		}
		for i := 0; i < len(s.records); i++ {
			r := s.records[i]
			if t, ok := found[r.Pos]; ok && t == r.Type {
				continue
			}
			m.removeAt(sp, s, i)
			i--
		}
	}
	// Add new ones in a stable order (y, z, x, like a section walk).
	add := make([]cube.Pos, 0, len(found))
	for p, t := range found {
		if _, r := m.lookup(p); r == nil || r.Type != t {
			add = append(add, p)
		}
	}
	sort.Slice(add, func(i, j int) bool {
		a, b := add[i], add[j]
		if a[1] != b[1] {
			return a[1] < b[1]
		}
		if a[2] != b[2] {
			return a[2] < b[2]
		}
		return a[0] < b[0]
	})
	for _, p := range add {
		m.Add(p, found[p])
	}
}

func (m *Manager) lookup(p cube.Pos) (*section, *Record) {
	s := m.sections[sectionOf(p)]
	if s == nil {
		return nil, nil
	}
	_, r := s.find(p)
	return s, r
}

func (m *Manager) removeAt(sp sectionPos, s *section, i int) {
	r := s.records[i]
	if IsVillage(r.Type) && r.IsOccupied() {
		m.centerDelta(sp, -1)
	}
	s.records = append(s.records[:i], s.records[i+1:]...)
	if len(s.records) == 0 {
		delete(m.sections, sp)
	}
}

func (m *Manager) centerDelta(sp sectionPos, d int) {
	n := m.centers[sp] + d
	if n <= 0 {
		delete(m.centers, sp)
	} else {
		m.centers[sp] = n
	}
}

// Add is PoiManager.add: a new record with all tickets free. A record already
// at p is replaced.
func (m *Manager) Add(p cube.Pos, t *Type) *Record {
	sp := sectionOf(p)
	s := m.sections[sp]
	if s == nil {
		s = &section{}
		m.sections[sp] = s
	}
	if i, _ := s.find(p); i >= 0 {
		m.removeAt(sp, s, i)
		if m.sections[sp] == nil {
			s = &section{}
			m.sections[sp] = s
		}
	}
	r := &Record{Pos: p, Type: t, free: t.MaxTickets}
	s.records = append(s.records, r)
	return r
}

// Remove is PoiManager.remove.
func (m *Manager) Remove(p cube.Pos) {
	sp := sectionOf(p)
	if s := m.sections[sp]; s != nil {
		if i, _ := s.find(p); i >= 0 {
			m.removeAt(sp, s, i)
		}
	}
}

// EnsureLoadedAndValid is PoiManager.ensureLoadedAndValid: bring the chunks
// within radius blocks (rounded down to chunks) of center up to date.
func (m *Manager) EnsureLoadedAndValid(tx *world.Tx, center cube.Pos, radius int) {
	cx, cz := center[0]>>4, center[2]>>4
	r := floorDiv(radius, 16)
	for x := cx - r; x <= cx+r; x++ {
		for z := cz - r; z <= cz+r; z++ {
			m.refresh(tx, world.ChunkPos{int32(x), int32(z)})
		}
	}
}

func floorDiv(a, b int) int {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

// Get returns the record at p, refreshing its column first.
func (m *Manager) Get(tx *world.Tx, p cube.Pos) (*Record, bool) {
	m.refresh(tx, world.ChunkPos{int32(p[0] >> 4), int32(p[2] >> 4)})
	_, r := m.lookup(p)
	return r, r != nil
}

// Exists is PoiManager.exists.
func (m *Manager) Exists(tx *world.Tx, p cube.Pos, pred func(*Type) bool) bool {
	r, ok := m.Get(tx, p)
	return ok && pred(r.Type)
}

// Type is PoiManager.getType.
func (m *Manager) Type(tx *world.Tx, p cube.Pos) (*Type, bool) {
	r, ok := m.Get(tx, p)
	if !ok {
		return nil, false
	}
	return r.Type, true
}

// Release is PoiManager.release: give back one ticket of the record at p.
func (m *Manager) Release(tx *world.Tx, p cube.Pos) bool {
	r, ok := m.Get(tx, p)
	if !ok {
		return false
	}
	wasOcc := r.IsOccupied()
	ok = r.release()
	if IsVillage(r.Type) && wasOcc && !r.IsOccupied() {
		m.centerDelta(sectionOf(p), -1)
	}
	return ok
}

// Claim takes a ticket of the record at p if it has one free (used to
// restore the claims of mobs loaded from disk: Java saves tickets with the
// PoI data).
func (m *Manager) Claim(tx *world.Tx, p cube.Pos, pred func(*Type) bool) bool {
	r, ok := m.Get(tx, p)
	if !ok || !pred(r.Type) {
		return false
	}
	return m.acquire(r)
}

func (m *Manager) acquire(r *Record) bool {
	wasOcc := r.IsOccupied()
	if !r.acquire() {
		return false
	}
	if IsVillage(r.Type) && !wasOcc && r.IsOccupied() {
		m.centerDelta(sectionOf(r.Pos), 1)
	}
	return true
}

// search is PoiAccess.findAnyPoiRecords (Paper/Moonrise): sections walked by
// z, x, y; records within radius on x and z and within maxDistSq.
func (m *Manager) search(tx *world.Tx, pred func(*Type) bool, filter func(*Type, cube.Pos) bool, center cube.Pos, radius int, maxDistSq float64, occ Occupancy, limit int) []*Record {
	lowerX, upperX := (center[0]-radius)>>4, (center[0]+radius)>>4
	lowerZ, upperZ := (center[2]-radius)>>4, (center[2]+radius)>>4
	lowerY, upperY := max(m.minY>>4, (center[1]-radius)>>4), min(m.maxY>>4, (center[1]+radius)>>4)
	for x := lowerX; x <= upperX; x++ {
		for z := lowerZ; z <= upperZ; z++ {
			m.refresh(tx, world.ChunkPos{int32(x), int32(z)})
		}
	}
	var out []*Record
	for z := lowerZ; z <= upperZ; z++ {
		for x := lowerX; x <= upperX; x++ {
			for y := lowerY; y <= upperY; y++ {
				s := m.sections[sectionPos{x, y, z}]
				if s == nil {
					continue
				}
				// PoiSection.byType: grouped by type in registration order.
				for _, t := range AllTypes {
					if !pred(t) {
						continue
					}
					for _, r := range s.records {
						if r.Type != t || !occ.test(r) {
							continue
						}
						p := r.Pos
						if abs(p[0]-center[0]) > radius || abs(p[2]-center[2]) > radius || distSq(p, center) > maxDistSq {
							continue
						}
						if filter != nil && !filter(r.Type, p) {
							continue
						}
						out = append(out, r)
						if len(out) >= limit {
							return out
						}
					}
				}
			}
		}
	}
	return out
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func distSq(a, b cube.Pos) float64 {
	dx, dy, dz := float64(a[0]-b[0]), float64(a[1]-b[1]), float64(a[2]-b[2])
	return dx*dx + dy*dy + dz*dz
}

// InRange is PoiManager.getInRange (radius on x/z and in distance).
func (m *Manager) InRange(tx *world.Tx, pred func(*Type) bool, center cube.Pos, radius int, occ Occupancy) []*Record {
	return m.search(tx, pred, nil, center, radius, float64(radius)*float64(radius), occ, math.MaxInt)
}

// InSquare is PoiManager.getInSquare.
func (m *Manager) InSquare(tx *world.Tx, pred func(*Type) bool, center cube.Pos, radius int, occ Occupancy) []*Record {
	return m.search(tx, pred, nil, center, radius, math.MaxFloat64, occ, math.MaxInt)
}

// CountInRange is PoiManager.getCountInRange.
func (m *Manager) CountInRange(tx *world.Tx, pred func(*Type) bool, center cube.Pos, radius int, occ Occupancy) int {
	return len(m.InRange(tx, pred, center, radius, occ))
}

// Found is a type and position pair.
type Found struct {
	Type *Type
	Pos  cube.Pos
}

// FindAllClosestFirstWithType is PoiManager.findAllClosestFirstWithType.
func (m *Manager) FindAllClosestFirstWithType(tx *world.Tx, pred func(*Type) bool, filter func(cube.Pos) bool, center cube.Pos, radius int, occ Occupancy) []Found {
	var f func(*Type, cube.Pos) bool
	if filter != nil {
		f = func(_ *Type, p cube.Pos) bool { return filter(p) }
	}
	recs := m.search(tx, pred, f, center, radius, float64(radius)*float64(radius), occ, math.MaxInt)
	sort.SliceStable(recs, func(i, j int) bool { return distSq(recs[i].Pos, center) < distSq(recs[j].Pos, center) })
	out := make([]Found, len(recs))
	for i, r := range recs {
		out[i] = Found{Type: r.Type, Pos: r.Pos}
	}
	return out
}

// FindAll is PoiManager.findAll.
func (m *Manager) FindAll(tx *world.Tx, pred func(*Type) bool, filter func(cube.Pos) bool, center cube.Pos, radius int, occ Occupancy) []cube.Pos {
	var f func(*Type, cube.Pos) bool
	if filter != nil {
		f = func(_ *Type, p cube.Pos) bool { return filter(p) }
	}
	recs := m.search(tx, pred, f, center, radius, float64(radius)*float64(radius), occ, math.MaxInt)
	out := make([]cube.Pos, len(recs))
	for i, r := range recs {
		out[i] = r.Pos
	}
	return out
}

// Find is PoiManager.find: any matching record.
func (m *Manager) Find(tx *world.Tx, pred func(*Type) bool, filter func(cube.Pos) bool, center cube.Pos, radius int, occ Occupancy) (cube.Pos, bool) {
	var f func(*Type, cube.Pos) bool
	if filter != nil {
		f = func(_ *Type, p cube.Pos) bool { return filter(p) }
	}
	recs := m.search(tx, pred, f, center, radius, float64(radius)*float64(radius), occ, 1)
	if len(recs) == 0 {
		return cube.Pos{}, false
	}
	return recs[0].Pos, true
}

// FindClosest is PoiManager.findClosest (PoiAccess.findNearestPoiRecord:
// the smallest squared distance, the first found on ties).
func (m *Manager) FindClosest(tx *world.Tx, pred func(*Type) bool, filter func(cube.Pos) bool, center cube.Pos, radius int, occ Occupancy) (Found, bool) {
	var f func(*Type, cube.Pos) bool
	if filter != nil {
		f = func(_ *Type, p cube.Pos) bool { return filter(p) }
	}
	recs := m.search(tx, pred, f, center, radius, float64(radius)*float64(radius), occ, math.MaxInt)
	var best *Record
	bestD := math.MaxFloat64
	for _, r := range recs {
		if d := distSq(r.Pos, center); d < bestD {
			best, bestD = r, d
		}
	}
	if best == nil {
		return Found{}, false
	}
	return Found{Type: best.Type, Pos: best.Pos}, true
}

// Take is PoiManager.take: claim a ticket of the first record with space
// that passes filter.
func (m *Manager) Take(tx *world.Tx, pred func(*Type) bool, filter func(*Type, cube.Pos) bool, center cube.Pos, radius int) (cube.Pos, bool) {
	recs := m.search(tx, pred, filter, center, radius, float64(radius)*float64(radius), HasSpace, 1)
	if len(recs) == 0 {
		return cube.Pos{}, false
	}
	m.acquire(recs[0])
	return recs[0].Pos, true
}

// Random is PoiManager.getRandom.
func (m *Manager) Random(tx *world.Tx, pred func(*Type) bool, filter func(cube.Pos) bool, occ Occupancy, center cube.Pos, radius int, intn func(int) int) (cube.Pos, bool) {
	var f func(*Type, cube.Pos) bool
	if filter != nil {
		f = func(_ *Type, p cube.Pos) bool { return filter(p) }
	}
	recs := m.search(tx, pred, f, center, radius, float64(radius)*float64(radius), occ, math.MaxInt)
	if len(recs) == 0 {
		return cube.Pos{}, false
	}
	return recs[intn(len(recs))].Pos, true
}

// SectionsToVillage is PoiManager.sectionsToVillage: the 26-way section
// distance (Chebyshev) to the nearest section holding an occupied village
// PoI, up to 6; 7 means none in reach.
func (m *Manager) SectionsToVillage(tx *world.Tx, p cube.Pos) int {
	sp := sectionOf(p)
	// The distance tracker only knows sections that were loaded: bring the
	// columns around up to date.
	for x := sp[0] - 6; x <= sp[0]+6; x++ {
		for z := sp[2] - 6; z <= sp[2]+6; z++ {
			if tx != nil {
				m.refresh(tx, world.ChunkPos{int32(x), int32(z)})
			}
		}
	}
	best := 7
	for c := range m.centers {
		d := max(abs(c[0]-sp[0]), abs(c[1]-sp[1]), abs(c[2]-sp[2]))
		if d < best {
			best = d
		}
	}
	return best
}

// IsVillage is ServerLevel.isVillage(BlockPos): within one section of a
// village centre.
func (m *Manager) IsVillage(tx *world.Tx, p cube.Pos) bool { return m.IsCloseToVillage(tx, p, 1) }

// IsCloseToVillage is ServerLevel.isCloseToVillage.
func (m *Manager) IsCloseToVillage(tx *world.Tx, p cube.Pos, sections int) bool {
	return sections <= 6 && m.SectionsToVillage(tx, p) <= sections
}
