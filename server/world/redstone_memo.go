package world

import (
	"reflect"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// RedstoneShaper may be implemented by blocks whose state changes often in ways that do not change how they connect
// to and conduct redstone (the power of dust, the progress of a moving block): RedstoneShape returns a key of the
// rest of the state. Two states of the same type with the same RedstoneShape must have the same model faces, the same
// encoded name or one that conducts the same, and connect to redstone the same.
type RedstoneShaper interface {
	RedstoneShape() uint64
}

// redstoneShapeKept reports whether before and after differ only in state RedstoneShape leaves out. Nothing kept
// in redstoneMemo or redstoneListMemo depends on that state, so such a change is not logged.
func redstoneShapeKept(before, after Block) bool {
	a, ok := before.(RedstoneShaper)
	if !ok {
		return false
	}
	b, ok := after.(RedstoneShaper)
	return ok && reflect.TypeOf(before) == reflect.TypeOf(after) && a.RedstoneShape() == b.RedstoneShape()
}

// changeLog logs the block changes of a world: the positions of the latest ones, and when a block of each chunk
// section last changed. A value worked out from the blocks around a position holds until a block near the position
// changes (unchangedSince).
type changeLog struct {
	// seq counts the changes logged. epoch is seq at the last change of everything (a chunk loaded, a structure).
	seq, epoch uint64
	ring       [changeLogSize]cube.Pos
	// sections holds the seq after the last change in each chunk section (sectionKey).
	sections map[uint64]uint64
}

const (
	// changeLogSize is how many of the latest changes are kept.
	changeLogSize = 64
	// changeLogScan is the most latest changes looked through to find out if a value holds. Past that the chunk
	// sections around the value are looked at.
	changeLogScan = 32
	// changeLogSections is the most chunk sections followed. Past that every value is worked out again.
	changeLogSections = 1 << 16
)

// sectionKey packs the chunk section of the block at x, y, z, false if it is too far out.
func sectionKey(x, y, z int) (uint64, bool) {
	const xz, sy = 1 << 23, 1 << 9
	x, y, z = x>>4, y>>4, z>>4
	if x < -xz || x >= xz || z < -xz || z >= xz || y < -sy || y >= sy {
		return 0, false
	}
	return uint64(x+xz)<<34 | uint64(z+xz)<<10 | uint64(y+sy), true
}

// blockChangedAt logs a change of the block at pos.
func (w *World) blockChangedAt(pos cube.Pos) {
	l := &w.changes
	l.ring[l.seq%changeLogSize] = pos
	l.seq++
	key, ok := sectionKey(pos[0], pos[1], pos[2])
	if !ok {
		// Too far out to follow: everything is worked out again.
		l.epoch = l.seq
		return
	}
	if l.sections == nil {
		l.sections = make(map[uint64]uint64)
	} else if len(l.sections) >= changeLogSections {
		clear(l.sections)
		l.epoch = l.seq
	}
	l.sections[key] = l.seq
}

// everyBlockChanged logs a change that may affect any block: a chunk loaded or unloaded, a structure built.
func (w *World) everyBlockChanged() {
	w.changes.seq++
	w.changes.epoch = w.changes.seq
}

// unchangedSince reports whether no block within r blocks of pos (on each axis) changed after seq. With few
// changes since, they are looked through; with more, no block of the chunk sections within r of pos may have changed.
func (l *changeLog) unchangedSince(seq uint64, pos cube.Pos, r int) bool {
	if seq < l.epoch {
		return false
	}
	if l.seq-seq <= changeLogScan {
		for i := seq; i < l.seq; i++ {
			q := l.ring[i%changeLogSize]
			if abs(q[0]-pos[0]) <= r && abs(q[1]-pos[1]) <= r && abs(q[2]-pos[2]) <= r {
				return false
			}
		}
		return true
	}
	for x := (pos[0] - r) >> 4; x <= (pos[0]+r)>>4; x++ {
		for y := (pos[1] - r) >> 4; y <= (pos[1]+r)>>4; y++ {
			for z := (pos[2] - r) >> 4; z <= (pos[2]+r)>>4; z++ {
				key, ok := sectionKey(x<<4, y<<4, z<<4)
				if !ok || l.sections[key] > seq {
					return false
				}
			}
		}
	}
	return true
}

// redstoneMemo holds values worked out from the blocks around a position, such as what redstone dust links to or
// which faces of a block conduct power, while no block near the position changes (changeLog). Redstone reads the
// same blocks many times between changes: it works them out once.
type redstoneMemo struct {
	m [redstoneMemoKinds]map[uint64]redstoneMemoEntry
}

// redstoneMemoEntry is a value and the changeLog.seq it was worked out (or last found to hold) at.
type redstoneMemoEntry struct {
	seq, v uint64
}

// RedstoneMemoKind is a kind of value kept by Tx.RedstoneMemo.
type RedstoneMemoKind uint8

const (
	// RedstoneMemoWireLinks is what redstone dust at a position links to through each horizontal face. It depends on
	// the blocks up to two blocks away.
	RedstoneMemoWireLinks RedstoneMemoKind = iota
	// redstoneMemoConductor is which faces of the block at a position conduct strong power (conductorFaces).
	redstoneMemoConductor
	redstoneMemoKinds
)

// redstoneMemoReach is, for each kind, how far from its position the blocks a value is worked out from may be, with
// a block to spare: a change further away keeps the value.
var redstoneMemoReach = [redstoneMemoKinds]int{RedstoneMemoWireLinks: 3, redstoneMemoConductor: 2}

// redstoneMemoLimit is the most values of one kind kept. Values of positions that are not read again stay until
// the limit is reached and all are dropped.
const redstoneMemoLimit = 1 << 15

// packMemoPos packs pos into a map key, false if it is too far out.
func packMemoPos(pos cube.Pos) (uint64, bool) {
	const xz, y = 1 << 25, 1 << 11
	if pos[0] < -xz || pos[0] >= xz || pos[2] < -xz || pos[2] >= xz || pos[1] < -y || pos[1] >= y {
		return 0, false
	}
	return uint64(pos[0]+xz)<<38 | uint64(pos[2]+xz)<<12 | uint64(pos[1]+y), true
}

// get returns the value of kind at pos if it still holds.
func (m *redstoneMemo) get(l *changeLog, kind RedstoneMemoKind, pos cube.Pos) (uint64, bool) {
	key, ok := packMemoPos(pos)
	if !ok {
		return 0, false
	}
	e, ok := m.m[kind][key]
	if !ok || e.seq == l.seq {
		return e.v, ok
	}
	if !l.unchangedSince(e.seq, pos, redstoneMemoReach[kind]) {
		return 0, false
	}
	// It holds now: later lookups need not look through the changes up to now again.
	m.m[kind][key] = redstoneMemoEntry{seq: l.seq, v: e.v}
	return e.v, true
}

// set stores the value of kind at pos worked out from the blocks as they were at seq.
func (m *redstoneMemo) set(kind RedstoneMemoKind, pos cube.Pos, seq, v uint64) {
	key, ok := packMemoPos(pos)
	if !ok {
		return
	}
	mm := m.m[kind]
	if mm == nil {
		mm = make(map[uint64]redstoneMemoEntry)
		m.m[kind] = mm
	} else if len(mm) >= redstoneMemoLimit {
		clear(mm)
	}
	mm[key] = redstoneMemoEntry{seq: seq, v: v}
}

// RedstoneMemo returns the value of kind stored for pos with SetRedstoneMemo, if no block near pos changed since.
// seq is the value to pass to SetRedstoneMemo after working the value out: if a block near pos changes while it is
// worked out, the stored value is not used.
func (tx *Tx) RedstoneMemo(kind RedstoneMemoKind, pos cube.Pos) (v uint64, ok bool, seq uint64) {
	w := tx.World()
	v, ok = w.memo.get(&w.changes, kind, pos)
	return v, ok, w.changes.seq
}

// SetRedstoneMemo stores the value of kind for pos, worked out from the blocks as they were at seq.
func (tx *Tx) SetRedstoneMemo(kind RedstoneMemoKind, pos cube.Pos, seq, v uint64) {
	tx.World().memo.set(kind, pos, seq, v)
}

// conductorFaces returns which faces of the loaded block b at pos conduct strong power, a bit for each face
// (redstoneStrongPowerConductor), reading the blocks around it from tx.
func (w *World) conductorFaces(tx *Tx, pos cube.Pos, b Block) uint8 {
	if v, ok := w.memo.get(&w.changes, redstoneMemoConductor, pos); ok {
		return uint8(v)
	}
	seq := w.changes.seq
	name, _ := b.EncodeBlock()
	var faces uint8
	for _, face := range cube.Faces() {
		if redstoneNamedStrongPowerConductor(pos, b, name, tx, face) {
			faces |= 1 << face
		}
	}
	w.memo.set(redstoneMemoConductor, pos, seq, uint64(faces))
	return faces
}

// redstoneListMemo keeps lists of positions graph compilation worked out from the blocks around a position, while
// no block near the position changes (changeLog).
type redstoneListMemo struct {
	m [redstoneListKinds]map[uint64]redstoneListEntry
}

// redstoneListEntry is a list and the changeLog.seq it was worked out (or last found to hold) at.
type redstoneListEntry struct {
	seq  uint64
	list []cube.Pos
}

const (
	// redstoneListAdjacent is redstoneCompile.adjacentRelevant.
	redstoneListAdjacent = iota
	// redstoneListRelayerNeighbours is redstoneRelayerNeighbourPositions of a relayer.
	redstoneListRelayerNeighbours
	// redstoneListRelayerConnected is redstoneCompile.relayerConnectedPositions.
	redstoneListRelayerConnected
	redstoneListKinds
)

// redstoneListReach is, for each kind of list, how far from its position the blocks it is worked out from may be,
// with a block to spare: relayer neighbours are read from blocks up to two away, the blocks next to a position
// conduct by the blocks up to two away from them.
var redstoneListReach = [redstoneListKinds]int{redstoneListAdjacent: 4, redstoneListRelayerNeighbours: 3, redstoneListRelayerConnected: 4}

// get returns the list of kind at pos if it still holds.
func (m *redstoneListMemo) get(l *changeLog, kind int, pos cube.Pos) ([]cube.Pos, bool) {
	key, ok := packMemoPos(pos)
	if !ok {
		return nil, false
	}
	e, ok := m.m[kind][key]
	if !ok || e.seq == l.seq {
		return e.list, ok
	}
	if !l.unchangedSince(e.seq, pos, redstoneListReach[kind]) {
		return nil, false
	}
	m.m[kind][key] = redstoneListEntry{seq: l.seq, list: e.list}
	return e.list, true
}

// set stores the list of kind at pos worked out from the blocks as they were at seq. The list must not be changed
// afterwards.
func (m *redstoneListMemo) set(kind int, pos cube.Pos, seq uint64, list []cube.Pos) {
	key, ok := packMemoPos(pos)
	if !ok {
		return
	}
	mm := m.m[kind]
	if mm == nil {
		mm = make(map[uint64]redstoneListEntry)
		m.m[kind] = mm
	} else if len(mm) >= redstoneMemoLimit {
		clear(mm)
	}
	mm[key] = redstoneListEntry{seq: seq, list: list}
}

// posSet is a set of positions, kept as a bitmap for each 16x16x16 section of the world: sets of positions close to
// each other, as graph compilation builds, cost a bit operation a position.
type posSet struct {
	sections map[[3]int]*[64]uint64
	// last caches the bitmap of the section looked up last.
	lastKey [3]int
	last    *[64]uint64
	n       int
	spare   []*[64]uint64
}

// sectionOf returns the section key of pos and the index of pos in its bitmap.
func sectionOf(pos cube.Pos) (key [3]int, bit uint) {
	key = [3]int{pos[0] >> 4, pos[1] >> 4, pos[2] >> 4}
	bit = uint(pos[0]&15)<<8 | uint(pos[2]&15)<<4 | uint(pos[1]&15)
	return key, bit
}

// bitmap returns the bitmap of the section with key, creating it if create is true.
func (s *posSet) bitmap(key [3]int, create bool) *[64]uint64 {
	if s.last != nil && s.lastKey == key {
		return s.last
	}
	b, ok := s.sections[key]
	if !ok {
		if !create {
			return nil
		}
		if s.sections == nil {
			s.sections = make(map[[3]int]*[64]uint64)
		}
		if n := len(s.spare); n > 0 {
			b, s.spare = s.spare[n-1], s.spare[:n-1]
		} else {
			b = new([64]uint64)
		}
		s.sections[key] = b
	}
	s.lastKey, s.last = key, b
	return b
}

// has reports whether pos is in the set.
func (s *posSet) has(pos cube.Pos) bool {
	key, bit := sectionOf(pos)
	b := s.bitmap(key, false)
	return b != nil && b[bit>>6]&(1<<(bit&63)) != 0
}

// add adds pos and reports whether it was not in the set yet.
func (s *posSet) add(pos cube.Pos) bool {
	key, bit := sectionOf(pos)
	b := s.bitmap(key, true)
	if b[bit>>6]&(1<<(bit&63)) != 0 {
		return false
	}
	b[bit>>6] |= 1 << (bit & 63)
	s.n++
	return true
}

// len returns the number of positions in the set.
func (s *posSet) len() int {
	return s.n
}

// clear empties the set, keeping its bitmaps for reuse.
func (s *posSet) clear() {
	for _, b := range s.sections {
		*b = [64]uint64{}
		s.spare = append(s.spare, b)
	}
	clear(s.sections)
	s.last, s.n = nil, 0
}
