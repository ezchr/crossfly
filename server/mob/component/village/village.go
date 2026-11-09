//go:build crossfly_full

package village

import (
	"math"
	"slices"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// Villages, POIs and dwellers (BDS VillageManager, Village, POIInstance,
// DwellerComponent). One Manager per world, ticked once per world tick from
// a world tick hook. Villages are not saved: dwellers find their POIs again
// after a reload and found the villages anew (BDS saves them in the level
// database).

// POIType is POIInstance's type: bed, meeting area (bell) or job site.
type POIType int

const (
	POIBed POIType = iota
	POIMeeting
	POIJob
	poiTypes
)

// Role is DwellerComponent's dweller_role.
type Role int

const (
	RoleInhabitant Role = iota
	RoleDefender
	RoleHostile
	RolePassive
	roleCount
)

func parseRole(s string) Role {
	switch s {
	case "defender":
		return RoleDefender
	case "hostile":
		return RoleHostile
	case "passive":
		return RolePassive
	}
	return RoleInhabitant
}

// Village constants (BDS Village / VillageManager statics).
const (
	villageStartXZ          = 32 // VILLAGE_START_XZ_BOUNDS
	villageStartHeight      = 12 // VILLAGE_START_HEIGHT
	unclaimedPOICap         = 32 // VILLAGE_UNCLAIMED_POI_CAP
	maxPOITolerance         = 64 // MAX_POI_TOLERANCE_DIST
	maxPOIQueries           = 64 // MAX_POI_QUERIES
	maxQueryScanIterations  = 1024
	uninitialisedLifetime   = 120 // UNINITIALIZED_VILLAGE_LIFETIME_MAX_TICKS
	noBreedTicks            = 3600
	aggressorMemoryTicks    = 300
	inactiveDwellerTicks    = 24000
	inactiveDwellerDistance = 48
	newVillageDistance      = 16
)

// POI is one point of interest (BDS POIInstance).
type POI struct {
	Pos       cube.Pos
	Type      POIType
	Name      string
	Radius    float64
	Capacity  int
	Owners    int
	Weight    int
	InitEvent string
	EndEvent  string
	Sound     string
	village   *Village
	removed   bool
	failures  int
}

// Full reports whether every owner slot is taken.
func (p *POI) Full() bool { return p.Capacity > 0 && p.Owners >= p.Capacity }

// Pathable is POIInstance::isPathable (fewer than 3 arrival failures).
func (p *POI) Pathable() bool { return p.failures < 3 }

// ArrivalFailed counts a failed attempt to walk to the POI
// (MoveToPOIGoal).
func (p *POI) ArrivalFailed() { p.failures++ }

// Arrived resets the arrival failures.
func (p *POI) Arrived() { p.failures = 0 }

// Valid reports whether the POI is still registered.
func (p *POI) Valid() bool { return p != nil && !p.removed }

// Centre is the POI's block centre.
func (p *POI) Centre() mgl64.Vec3 { return p.Pos.Vec3Centre() }

type dwellerData struct {
	lastSeen int64
	pos      mgl64.Vec3
}

// Village is a BDS Village.
type Village struct {
	ID      uuid.UUID
	m       *Manager
	bounds  cube.BBox
	hasBox  bool
	removed bool

	unclaimed [poiTypes][]*POI
	claimed   map[*world.EntityHandle]*[poiTypes]*POI
	dwellers  [roleCount]map[*world.EntityHandle]*dwellerData
	// order keeps insertion order of inhabitants for deterministic claims.
	order []*world.EntityHandle

	aggressors map[*world.EntityHandle]int64
	workStamps map[*world.EntityHandle]int64
	standing   map[*world.EntityHandle]int

	initialized  bool
	uninitTicks  int
	noBreedUntil int64
	nextCat      int64
	nextBell     int64
	lastActive   int64

	raid *Raid
}

func newVillage(m *Manager, pos cube.Pos) *Village {
	v := &Village{ID: uuid.New(), m: m, claimed: map[*world.EntityHandle]*[poiTypes]*POI{},
		aggressors: map[*world.EntityHandle]int64{}, workStamps: map[*world.EntityHandle]int64{}, standing: map[*world.EntityHandle]int{}}
	for i := range v.dwellers {
		v.dwellers[i] = map[*world.EntityHandle]*dwellerData{}
	}
	v.resetBounds(pos)
	v.lastActive = m.now
	return v
}

func (v *Village) resetBounds(p cube.Pos) {
	c := p.Vec3()
	v.bounds = cube.Box(c[0]-villageStartXZ, c[1]-villageStartHeight, c[2]-villageStartXZ, c[0]+villageStartXZ, c[1]+villageStartHeight, c[2]+villageStartXZ)
	v.hasBox = true
}

// Removed reports whether the village no longer exists.
func (v *Village) Removed() bool { return v == nil || v.removed }

// Bounds is the village's box (BDS mBounds).
func (v *Village) Bounds() cube.BBox { return v.bounds }

// Center is the centre of the bounds.
func (v *Village) Center() mgl64.Vec3 {
	mn, mx := v.bounds.Min(), v.bounds.Max()
	return mn.Add(mx).Mul(0.5)
}

// ApproximateRadius is Village::getApproximateRadius.
func (v *Village) ApproximateRadius() float64 {
	mn, mx := v.bounds.Min(), v.bounds.Max()
	sx, sz := mx[0]-mn[0], mx[2]-mn[2]
	hx, hz := sx/2, sz/2
	if math.Abs(sx-sz) >= 1.19e-7 {
		return (hx + hz) * 0.6
	}
	return math.Sqrt(hx*hx + hz*hz)
}

// Within is Village::withinVillageBounds(pos, tolerance).
func (v *Village) Within(p mgl64.Vec3, tol float64) bool {
	if !v.hasBox {
		return false
	}
	mn, mx := v.bounds.Min(), v.bounds.Max()
	for i := range 3 {
		if p[i] < mn[i]-tol || p[i] > mx[i]+tol {
			return false
		}
	}
	return true
}

// Villagers is the number of inhabitants.
func (v *Village) Villagers() int { return len(v.dwellers[RoleInhabitant]) }

// Dwellers returns the handles of the dwellers with a role.
func (v *Village) Dwellers(r Role) []*world.EntityHandle {
	if r == RoleInhabitant {
		return slices.Clone(v.order)
	}
	out := make([]*world.EntityHandle, 0, len(v.dwellers[r]))
	for h := range v.dwellers[r] {
		out = append(out, h)
	}
	return out
}

// HasDweller reports whether h dwells here with role r.
func (v *Village) HasDweller(r Role, h *world.EntityHandle) bool {
	_, ok := v.dwellers[r][h]
	return ok
}

// BedCount is Village::getBedPOICount: unclaimed beds plus claimed ones.
func (v *Village) BedCount() int {
	n := len(v.unclaimed[POIBed])
	for _, c := range v.claimed {
		if c[POIBed].Valid() {
			n++
		}
	}
	return n
}

// NeedsMoreVillagers is Village::checkNeedMoreVillagers (breeding).
func (v *Village) NeedsMoreVillagers() bool {
	if v.m.now < v.noBreedUntil {
		return false
	}
	return v.Villagers() < max(1, v.BedCount())
}

// ResetNoBreedTimer blocks breeding for 3600 ticks.
func (v *Village) ResetNoBreedTimer() { v.noBreedUntil = v.m.now + noBreedTicks }

// OwnedPOI returns the POI of a type h claimed (Village::fetchOwnedPOI).
func (v *Village) OwnedPOI(h *world.EntityHandle, t POIType) (*POI, bool) {
	c, ok := v.claimed[h]
	if !ok || !c[t].Valid() {
		return nil, false
	}
	return c[t], true
}

// ClosestPOI returns the claimed or unclaimed POI of a type closest to pos
// (Village::getClosestPOI).
func (v *Village) ClosestPOI(t POIType, pos mgl64.Vec3) (*POI, bool) {
	var best *POI
	bd := math.Inf(1)
	try := func(p *POI) {
		if !p.Valid() {
			return
		}
		if d := p.Centre().Sub(pos).LenSqr(); d < bd {
			best, bd = p, d
		}
	}
	for _, p := range v.unclaimed[t] {
		try(p)
	}
	for _, h := range v.order {
		if c := v.claimed[h]; c != nil {
			try(c[t])
		}
	}
	return best, best != nil
}

// POIs returns every valid POI of a type (claimed and unclaimed).
func (v *Village) POIs(t POIType) []*POI {
	var out []*POI
	seen := map[*POI]bool{}
	for _, p := range v.unclaimed[t] {
		if p.Valid() && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, h := range v.order {
		if c := v.claimed[h]; c != nil && c[t].Valid() && !seen[c[t]] {
			seen[c[t]] = true
			out = append(out, c[t])
		}
	}
	return out
}

// AddAggressor remembers e attacked a dweller (Village::addAggressor).
func (v *Village) AddAggressor(e world.Entity) { v.aggressors[e.H()] = v.m.now }

// ClosestAggressor is Village::getClosestAggressor: the nearest live
// aggressor or hostile dweller.
func (v *Village) ClosestAggressor(tx *world.Tx, pos mgl64.Vec3) (world.Entity, bool) {
	var best world.Entity
	bd := math.Inf(1)
	try := func(h *world.EntityHandle) {
		e, ok := h.Entity(tx)
		if !ok || dead(e) {
			return
		}
		if d := e.Position().Sub(pos).LenSqr(); d < bd {
			best, bd = e, d
		}
	}
	for h := range v.aggressors {
		try(h)
	}
	for h := range v.dwellers[RoleHostile] {
		try(h)
	}
	return best, best != nil
}

// Standing values (Village::modifyStanding; Java-like values, the BDS
// statics are not in the dump).
const (
	StandingSmallPenalty  = -5
	StandingMediumPenalty = -10
	StandingLargePenalty  = -15
	StandingGolemDeath    = -50
	StandingSmallReward   = 1
	StandingBad           = -15
)

// ModifyStanding changes a player's standing in the village.
func (v *Village) ModifyStanding(p world.Entity, delta int) {
	v.standing[p.H()] = max(-30, min(30, v.standing[p.H()]+delta))
}

// Standing returns a player's standing.
func (v *Village) Standing(p world.Entity) int { return v.standing[p.H()] }

// ClosestBadStandingPlayer returns the nearest player with standing <= -15.
func (v *Village) ClosestBadStandingPlayer(tx *world.Tx, pos mgl64.Vec3) (world.Entity, bool) {
	var best world.Entity
	bd := math.Inf(1)
	for h, s := range v.standing {
		if s > StandingBad {
			continue
		}
		e, ok := h.Entity(tx)
		if !ok || dead(e) {
			continue
		}
		if d := e.Position().Sub(pos).LenSqr(); d < bd {
			best, bd = e, d
		}
	}
	return best, best != nil
}

// SetWorkTimestamp is Village::trySetVillagerWorkTimestamp (WorkGoal).
func (v *Village) SetWorkTimestamp(h *world.EntityHandle) { v.workStamps[h] = v.m.now }

// Raid returns the village's raid, if any.
func (v *Village) Raid() (*Raid, bool) { return v.raid, v.raid != nil }

// addActor is Village::addActorToVillage.
func (v *Village) addActor(tx *world.Tx, r Role, h *world.EntityHandle, pos mgl64.Vec3) {
	if _, ok := v.dwellers[r][h]; ok {
		return
	}
	v.dwellers[r][h] = &dwellerData{lastSeen: v.m.now, pos: pos}
	if r != RoleInhabitant {
		return
	}
	v.order = append(v.order, h)
	c := &[poiTypes]*POI{}
	v.claimed[h] = c
	// Claim the first unclaimed bed at once.
	if m, ok := mob.OfHandle(tx, h); ok {
		for len(v.unclaimed[POIBed]) > 0 {
			p := v.unclaimed[POIBed][0]
			if !p.Valid() || p.Full() {
				v.unclaimed[POIBed] = v.unclaimed[POIBed][1:]
				continue
			}
			v.claim(m, p)
			if p.Full() {
				v.unclaimed[POIBed] = v.unclaimed[POIBed][1:]
			}
			break
		}
	}
	v.initialized = true
}

// removeActor is Village::removeActorFromVillage.
func (v *Village) removeActor(h *world.EntityHandle) {
	for r := range v.dwellers {
		delete(v.dwellers[r], h)
	}
	if c, ok := v.claimed[h]; ok {
		for t, p := range c {
			if p.Valid() {
				p.Owners = max(0, p.Owners-1)
				v.addUnclaimed(POIType(t), p)
			}
		}
		delete(v.claimed, h)
		v.calcBounds()
	}
	if i := slices.Index(v.order, h); i >= 0 {
		v.order = slices.Delete(v.order, i, i+1)
	}
	delete(v.workStamps, h)
	if v.raid != nil {
		v.raid.removeRaider(h)
	}
}

func (v *Village) addUnclaimed(t POIType, p *POI) bool {
	if slices.Contains(v.unclaimed[t], p) {
		return true
	}
	if len(v.unclaimed[t]) >= unclaimedPOICap {
		return false
	}
	v.unclaimed[t] = append(v.unclaimed[t], p)
	return true
}

// addPOI is Village::addPOI.
func (v *Village) addPOI(p *POI) bool {
	if p.Type == POIBed && len(v.claimedPOIs()) == 0 && len(v.unclaimed[POIBed]) == 0 {
		v.resetBounds(p.Pos)
	}
	if !v.addUnclaimed(p.Type, p) {
		return false
	}
	p.village = v
	return true
}

func (v *Village) claimedPOIs() []*POI {
	var out []*POI
	for _, h := range v.order {
		for _, p := range v.claimed[h] {
			if p.Valid() {
				out = append(out, p)
			}
		}
	}
	return out
}

// calcBounds is Village::_calcPOIDist: the first claimed POI +-(32,12,32)
// grown to contain every claimed POI. Without claims the bounds stay.
func (v *Village) calcBounds() {
	ps := v.claimedPOIs()
	if len(ps) == 0 {
		return
	}
	v.resetBounds(ps[0].Pos)
	mn, mx := v.bounds.Min(), v.bounds.Max()
	for _, p := range ps[1:] {
		c := p.Pos.Vec3()
		for i := range 3 {
			mn[i], mx[i] = min(mn[i], c[i]), max(mx[i], c[i]+1)
		}
	}
	v.bounds = cube.Box(mn[0], mn[1], mn[2], mx[0], mx[1], mx[2])
}

// claim is Village::_tryAddPoiToVillage.
func (v *Village) claim(m *mob.Mob, p *POI) {
	c := v.claimed[m.H()]
	if c == nil {
		return
	}
	p.village = v
	c[p.Type] = p
	if p.InitEvent != "" {
		m.Fire(p.InitEvent, nil)
	}
	if p.Type == POIJob {
		if d, ok := DwellerOf(m); ok {
			d.trySetPreferredProfession(m, p.Name)
		}
	}
	p.Owners++
	v.calcBounds()
}

// tick is Village::tick.
func (v *Village) tick(tx *world.Tx) {
	now := v.m.now
	if !v.initialized {
		v.uninitTicks++
	}
	for h, t := range v.aggressors {
		if t+aggressorMemoryTicks < now {
			delete(v.aggressors, h)
			continue
		}
		if e, ok := h.Entity(tx); !ok || dead(e) {
			delete(v.aggressors, h)
		}
	}
	v.updateClaimed(tx)
	switch now % 30 {
	case 0:
		for h, t := range v.workStamps {
			if _, ok := v.dwellers[RoleInhabitant][h]; !ok || t+24000 < now {
				delete(v.workStamps, h)
			}
		}
	case 15:
		v.updateInactive(tx)
		v.updateUnclaimed(tx)
		v.claimUnclaimed(tx)
	}
	if spawningAllowed(tx) {
		v.trySpawnDefenders(tx)
		if now >= v.nextCat {
			v.nextCat = now + 1200
			v.spawnPassive(tx)
		}
	}
	if v.raid != nil && v.raid.State == RaidPre && now > v.nextBell {
		v.nextBell = now + 60 + int64(v.m.rand(40))
	}
}

// updateClaimed is Village::_updateClaimedPOIs: claimed POIs whose block
// went away, changed or that could not be reached three times are lost.
func (v *Village) updateClaimed(tx *world.Tx) {
	changed := false
	for _, h := range v.order {
		c := v.claimed[h]
		for t, p := range c {
			if p == nil {
				continue
			}
			if p.Valid() {
				b, loaded := tx.BlockLoaded(p.Pos)
				if !loaded {
					continue
				}
				if bp, ok := blueprintOf(b); ok && bp.name == p.Name && p.Pathable() && v.m.pois[p.Type][p.Pos] == p {
					continue
				}
			}
			c[t] = nil
			changed = true
			if p.Valid() {
				v.m.removePOI(p)
			}
			if m, ok := mob.OfHandle(tx, h); ok {
				if d, ok := DwellerOf(m); ok && !d.HasPreferredProfession(m) {
					d.preferred = ""
					if p.EndEvent != "" {
						m.Fire(p.EndEvent, nil)
					}
				}
			}
		}
	}
	if changed {
		v.calcBounds()
	}
}

// updateUnclaimed is Village::_updateUnclaimedPOIs.
func (v *Village) updateUnclaimed(tx *world.Tx) {
	for t := range v.unclaimed {
		v.unclaimed[t] = slices.DeleteFunc(v.unclaimed[t], func(p *POI) bool {
			if !p.Valid() || p.Full() {
				return true
			}
			b, loaded := tx.BlockLoaded(p.Pos)
			if !loaded {
				return false
			}
			if bp, ok := blueprintOf(b); ok && bp.name == p.Name {
				return false
			}
			v.m.removePOI(p)
			return true
		})
	}
}

// updateInactive is Village::_updateAndRemoveInactiveDwellers.
func (v *Village) updateInactive(tx *world.Tx) {
	for r := range v.dwellers {
		for h, d := range v.dwellers[r] {
			if d.lastSeen+inactiveDwellerTicks > v.m.now {
				continue
			}
			e, ok := h.Entity(tx)
			if !ok {
				if h.Closed() {
					v.removeActor(h)
				}
				continue
			}
			if !v.Within(e.Position(), inactiveDwellerDistance) || dead(e) {
				v.removeActor(h)
				continue
			}
			d.lastSeen = v.m.now
		}
	}
}

// claimUnclaimed is Village::_claimUnclaimedPOIs.
func (v *Village) claimUnclaimed(tx *world.Tx) {
	for _, h := range slices.Clone(v.order) {
		c := v.claimed[h]
		if c == nil {
			continue
		}
		m, ok := mob.OfHandle(tx, h)
		if !ok {
			continue
		}
		for t := range poiTypes {
			if c[t].Valid() || len(v.unclaimed[t]) == 0 {
				continue
			}
			if POIType(t) == POIJob {
				v.claimJob(m)
				continue
			}
			for len(v.unclaimed[t]) > 0 {
				p := v.unclaimed[t][0]
				if !p.Valid() || p.Full() {
					v.unclaimed[t] = v.unclaimed[t][1:]
					continue
				}
				v.claim(m, p)
				if p.Full() {
					v.unclaimed[t] = v.unclaimed[t][1:]
				}
				break
			}
		}
	}
}

// claimJob is the job site branch of Village::_findAvailablePOI.
func (v *Village) claimJob(m *mob.Mob) {
	if m.HasComponent("minecraft:is_baby") || m.HasFamily("nitwit") {
		return
	}
	d, ok := DwellerOf(m)
	if !ok || d.role != RoleInhabitant {
		return
	}
	list := v.unclaimed[POIJob]
	if d.HasPreferredProfession(m) {
		for i, p := range list {
			if p.Valid() && !p.Full() && p.Name == d.preferred {
				v.claim(m, p)
				if p.Full() {
					v.unclaimed[POIJob] = slices.Delete(list, i, i+1)
				}
				return
			}
		}
		return
	}
	list = slices.DeleteFunc(list, func(p *POI) bool { return !p.Valid() || p.Full() })
	v.unclaimed[POIJob] = list
	total := 0
	for _, p := range list {
		total += max(p.Weight, 0)
	}
	if total <= 0 {
		return
	}
	r := m.Rand().IntN(total)
	for i, p := range list {
		r -= max(p.Weight, 0)
		if r < 0 {
			v.claim(m, p)
			if p.Full() {
				v.unclaimed[POIJob] = slices.Delete(list, i, i+1)
			}
			return
		}
	}
}

// UnlinkMismatchedJobSite drops a job site whose name differs from the
// villager's profession, without an event (Village::unlinkMismatchedJobsites).
func (v *Village) UnlinkMismatchedJobSite(m *mob.Mob, profession string) {
	c := v.claimed[m.H()]
	if c == nil || !c[POIJob].Valid() || c[POIJob].Name == profession {
		return
	}
	p := c[POIJob]
	c[POIJob] = nil
	p.Owners = max(0, p.Owners-1)
	v.addUnclaimed(POIJob, p)
	v.calcBounds()
}

// trySpawnDefenders is Village::_trySpawnDefenderDwellers: an iron golem
// when 75% of the villagers worked in the last day, there are at least
// max(villagers, 20) beds and fewer golems than villagers/10, at 1/700 a
// tick.
func (v *Village) trySpawnDefenders(tx *world.Tx) {
	n := v.Villagers()
	if n == 0 {
		return
	}
	worked := 0
	for _, t := range v.workStamps {
		if t > v.m.now-24000 {
			worked++
		}
	}
	if float64(worked) < 0.75*float64(n) || v.BedCount() < max(n, 20) || len(v.dwellers[RoleDefender]) >= n/10 {
		return
	}
	if v.m.rand(700) != 0 {
		return
	}
	if m, ok := v.m.trySpawnMob(tx, "minecraft:iron_golem", "minecraft:from_village", v.Center(), 10, 8, 6); ok {
		v.addActor(tx, RoleDefender, m.H(), m.Position())
		if d, ok := DwellerOf(m); ok {
			d.village = v
			d.hasJoined = true
		}
	}
}

// spawnPassive is Village::_spawnPassiveDwellers (cats, every 1200 ticks).
func (v *Village) spawnPassive(tx *world.Tx) {
	maxCats := v.Villagers() / 4
	if maxCats == 0 {
		return
	}
	c := v.Center()
	box := cube.Box(c[0]-48, c[1]-48, c[2]-48, c[0]+48, c[1]+48, c[2]+48)
	n := 0
	for e := range tx.EntitiesWithin(box) {
		if m, ok := mob.Of(e); ok && m.Identifier() == "minecraft:cat" {
			n++
		}
	}
	if n >= maxCats || n >= 5 {
		return
	}
	event := "minecraft:spawn_from_village"
	if t := tx.World().Time() % 24000; t > 13000 && t < 23000 && v.m.rand(2) == 0 {
		event = "minecraft:spawn_midnight_cat"
	}
	if m, ok := v.m.trySpawnMob(tx, "minecraft:cat", event, c, 10, 8, 6); ok {
		v.addActor(tx, RolePassive, m.H(), m.Position())
	}
}

// RewardAllPlayers gives experience to the players in the village
// (first_founding_reward).
func (v *Village) RewardAllPlayers(tx *world.Tx, xp int) {
	if xp <= 0 {
		return
	}
	for e := range tx.Players() {
		if v.Within(e.Position(), 0) {
			if p, ok := e.(interface{ AddExperience(int) int }); ok {
				p.AddExperience(xp)
			}
		}
	}
}

// Manager is a world's BDS VillageManager.
type Manager struct {
	w        *world.World
	now      int64
	villages []*Village
	pois     [poiTypes]map[cube.Pos]*POI

	queue   []*world.EntityHandle
	cursor  int
	pending []*POI

	trader traderScheduler
	omens  map[*world.EntityHandle]*omenState
	rest   map[*world.EntityHandle]int64
	bars   map[*world.EntityHandle]*Raid
}

var managers sync.Map

// ManagerOf returns the village manager of tx's world.
func ManagerOf(tx *world.Tx) *Manager {
	w := tx.World()
	if v, ok := managers.Load(w); ok {
		return v.(*Manager)
	}
	m := &Manager{w: w, omens: map[*world.EntityHandle]*omenState{}, rest: map[*world.EntityHandle]int64{}, bars: map[*world.EntityHandle]*Raid{}}
	for i := range m.pois {
		m.pois[i] = map[cube.Pos]*POI{}
	}
	m.trader.days = 0
	v, _ := managers.LoadOrStore(w, m)
	return v.(*Manager)
}

// Now is the manager's tick counter (ticks the world ran since the manager
// was made).
func (m *Manager) Now() int64 { return m.now }

func (m *Manager) rand(n int) int {
	if n <= 0 {
		return 0
	}
	return int(uint64(m.now*2654435761+int64(len(m.villages))*40503+int64(m.cursor)) % uint64(n))
}

// Villages returns the live villages.
func (m *Manager) Villages() []*Village { return slices.Clone(m.villages) }

// submitQuery is VillageManager::submitFindPOIQuery.
func (m *Manager) submitQuery(h *world.EntityHandle) {
	if len(m.queue) >= maxPOIQueries || slices.Contains(m.queue, h) {
		return
	}
	m.queue = append(m.queue, h)
}

func (m *Manager) tick(tx *world.Tx) {
	m.now++
	m.villages = slices.DeleteFunc(m.villages, func(v *Village) bool {
		raiding := v.raid != nil && v.raid.State != RaidDone
		if (!v.initialized && v.uninitTicks > uninitialisedLifetime && v.Villagers() == 0) ||
			(v.initialized && (v.BedCount() == 0 || v.Villagers() == 0) && !raiding) {
			m.removeVillage(tx, v)
			return true
		}
		return false
	})
	m.scan(tx)
	for _, p := range m.pending {
		if p.Type == POIBed {
			m.assignOrCreate(p)
		} else {
			m.assignOnly(p)
		}
	}
	m.pending = m.pending[:0]
	for _, v := range slices.Clone(m.villages) {
		if v.removed {
			continue
		}
		if m.now-v.lastActive < 200 || v.raid != nil {
			v.tick(tx)
		}
		if v.raid != nil {
			v.tickRaid(tx)
		}
	}
	m.tickPlayers(tx)
	m.trader.tick(tx, m)
}

// scan is VillageManager::_addPOIWithinRadius, resumable: 1024 blocks per
// tick of the 32x8x32 box around the front query's actor.
func (m *Manager) scan(tx *world.Tx) {
	for len(m.queue) > 0 {
		h := m.queue[0]
		e, ok := h.Entity(tx)
		if !ok {
			m.queue, m.cursor = m.queue[1:], 0
			continue
		}
		c := cube.PosFromVec3(e.Position())
		const sx, sy, sz = 32, 8, 32
		end := min(m.cursor+maxQueryScanIterations, sx*sy*sz)
		for i := m.cursor; i < end; i++ {
			x, y, z := i/(sy*sz), (i/sz)%sy, i%sz
			pos := cube.Pos{c[0] - 16 + x, c[1] - 4 + y, c[2] - 16 + z}
			b, loaded := tx.BlockLoaded(pos)
			if !loaded {
				continue
			}
			bp, ok := blueprintOf(b)
			if !ok {
				continue
			}
			if m.registered(pos) {
				continue
			}
			m.pending = append(m.pending, bp.instance(pos))
		}
		m.cursor = end
		if end >= sx*sy*sz {
			m.queue, m.cursor = m.queue[1:], 0
		}
		return
	}
}

func (m *Manager) registered(pos cube.Pos) bool {
	for _, mp := range m.pois {
		if p, ok := mp[pos]; ok && p.Valid() {
			return true
		}
	}
	for _, p := range m.pending {
		if p.Pos == pos {
			return true
		}
	}
	return false
}

func (m *Manager) insertPOI(p *POI) { m.pois[p.Type][p.Pos] = p }

func (m *Manager) removePOI(p *POI) {
	p.removed = true
	p.village = nil
	if m.pois[p.Type][p.Pos] == p {
		delete(m.pois[p.Type], p.Pos)
	}
}

// assignOrCreate is VillageManager::_tryAssignPOIOrCreateVillage (beds
// found villages).
func (m *Manager) assignOrCreate(p *POI) {
	if p.Full() {
		return
	}
	v := m.ClosestVillage(p.Pos.Vec3Centre(), maxPOITolerance)
	if v != nil && v.addPOI(p) {
		m.insertPOI(p)
		return
	}
	if v == nil || p.Pos.Vec3Centre().Sub(v.Center()).Len()-v.ApproximateRadius() > newVillageDistance {
		nv := newVillage(m, p.Pos)
		if nv.addPOI(p) {
			m.insertPOI(p)
			m.villages = append(m.villages, nv)
		}
	}
}

// assignOnly is VillageManager::_assignPOIOnly (bells, job sites).
func (m *Manager) assignOnly(p *POI) {
	if v := m.ClosestVillage(p.Pos.Vec3Centre(), maxPOITolerance); v != nil && v.addPOI(p) {
		m.insertPOI(p)
	}
}

// ClosestVillage is VillageManager::fetchClosestVillage: villages whose
// bounds grown by rng contain pos; one containing pos beats one that does
// not, else the one with the smaller distance past its radius.
func (m *Manager) ClosestVillage(pos mgl64.Vec3, rng float64) *Village {
	var best *Village
	var bestIn bool
	var bestD float64
	for _, v := range m.villages {
		if v.removed || !v.Within(pos, rng) {
			continue
		}
		in := v.Within(pos, 0)
		d := pos.Sub(v.Center()).Len() - v.ApproximateRadius()
		if best == nil || (in && !bestIn) || (in == bestIn && d < bestD) {
			best, bestIn, bestD = v, in, d
		}
	}
	return best
}

// closestSuitable is fetchClosestVillageMostSuitableForDweller: villages
// with free beds first.
func (m *Manager) closestSuitable(pos mgl64.Vec3, rng float64) *Village {
	var best *Village
	var bestBeds, bestIn bool
	var bestD float64
	for _, v := range m.villages {
		if v.removed || !v.Within(pos, rng) {
			continue
		}
		beds := len(v.unclaimed[POIBed]) > 0
		in := v.Within(pos, 0)
		d := pos.Sub(v.Center()).Len() - v.ApproximateRadius()
		switch {
		case best == nil, beds && !bestBeds:
		case beds != bestBeds:
			continue
		case in && !bestIn:
		case in == bestIn && d < bestD:
		default:
			continue
		}
		best, bestBeds, bestIn, bestD = v, beds, in, d
	}
	return best
}

// VillageAt returns the village whose bounds contain pos, if any (the
// is_in_village test).
func (m *Manager) VillageAt(pos mgl64.Vec3) *Village {
	for _, v := range m.villages {
		if !v.removed && v.Within(pos, 0) {
			return v
		}
	}
	return nil
}

func (m *Manager) removeVillage(tx *world.Tx, v *Village) {
	v.removed = true
	for _, h := range slices.Clone(v.order) {
		if c := v.claimed[h]; c != nil {
			for _, p := range c {
				if p.Valid() {
					m.removePOI(p)
				}
			}
		}
	}
	for _, l := range v.unclaimed {
		for _, p := range l {
			if p.Valid() {
				m.removePOI(p)
			}
		}
	}
	if v.raid != nil {
		v.raid.end(tx)
	}
}

// trySpawnMob is MobSpawnUtils::trySpawnMob: up to tries positions within
// xz horizontally of origin, snapped to a standing spot within y blocks.
func (m *Manager) trySpawnMob(tx *world.Tx, id, event string, origin mgl64.Vec3, tries, xz, y int) (*mob.Mob, bool) {
	o := cube.PosFromVec3(origin)
	for i := range tries {
		dx := m.rand(2*xz+1) - xz
		dz := (m.rand(2*xz+1+i)+i*7)%(2*xz+1) - xz
		p, ok := standingSpot(tx, cube.Pos{o[0] + dx, o[1], o[2] + dz}, y)
		if !ok {
			continue
		}
		mm, err := mob.Spawn(tx, id, p.Vec3Middle(), mob.SpawnOptions{Event: event})
		if err == nil {
			return mm, true
		}
	}
	return nil, false
}

// standingSpot finds a block within dy of p with a solid block under it and
// two free blocks.
func standingSpot(tx *world.Tx, p cube.Pos, dy int) (cube.Pos, bool) {
	for d := 0; d <= dy; d++ {
		for _, s := range []int{d, -d} {
			c := p.Add(cube.Pos{0, s, 0})
			if solid(tx, c.Side(cube.FaceDown)) && !solid(tx, c) && !solid(tx, c.Side(cube.FaceUp)) {
				if _, ok := tx.Liquid(c); ok {
					continue
				}
				return c, true
			}
			if d == 0 {
				break
			}
		}
	}
	return p, false
}
