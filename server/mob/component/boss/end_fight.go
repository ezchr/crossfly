//go:build crossfly_full

package boss

import (
	"encoding/binary"
	"log/slog"
	"math"
	"math/rand/v2"
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
)

// EndFight is BDS EndDragonFight, one per End world: the dragon appears when
// a player first comes near the centre of the End (with the obsidian spikes
// and their crystals, which Dragonfly does not generate), its death opens
// the exit portal, lays the dragon egg the first time and opens a gateway,
// and four end crystals on the exit portal summon it again. It is saved with
// the world as Bedrock saves it (TheEnd, DragonFight).
type EndFight struct {
	w *world.World

	dragonID      int64
	dragon        *world.EntityHandle
	dragonKilled  bool
	dragonSpawned bool
	prevKilled    bool
	// fresh: the world had no End fight data, so the spikes may be missing.
	fresh   bool
	started bool

	exitPortal cube.Pos
	hasExit    bool
	gateways   []int

	respawnStage int
	respawnTime  int
	// crystals are the four end crystals of a respawn.
	crystals []*world.EntityHandle

	// numCrystals is _updateCrystalCount: the crystals on the spikes.
	numCrystals int
	crystalTick int
	portalTick  int
	missing     int
	// settle are new gateways whose age is set to 200 (no more spawn beam)
	// once 200 ticks passed.
	settle []settleGateway
}

type settleGateway struct {
	pos  cube.Pos
	tick int64
}

// Respawn stages (EndDragonFight::RespawnAnimation).
const (
	respawnNone = iota
	respawnStart
	respawnPreparing
	respawnPillars
	respawnSummoning
	respawnEnd
)

// dragonSpawnPos is where the fight's dragon appears and the respawn beams
// meet (EndDragonFight, 0 128 0).
var dragonSpawnPos = cube.Pos{0, 128, 0}

var fights sync.Map // *world.World -> *EndFight

var enableFight sync.Once

// EnableEndFight registers the world tick hook the End fight runs from
// (vanilla.Register calls it with the mobs on): until then the End has no
// dragon fight and nothing of it runs.
func EnableEndFight() {
	enableFight.Do(func() {
		world.RegisterTickHook(endFightTick)
		block.SetEndGatewayLinker(linkGateway)
	})
}

// FightOf returns the End fight of w, an End world, loading it on first use.
func FightOf(w *world.World) *EndFight {
	if v, ok := fights.Load(w); ok {
		return v.(*EndFight)
	}
	f := &EndFight{w: w, dragonID: -1}
	f.load()
	v, _ := fights.LoadOrStore(w, f)
	return v.(*EndFight)
}

// EndFightWorld decides which worlds have the dragon fight. By default it is
// the End that end portals lead to (its portal destination is another
// world): worlds of the End dimension a server makes for its own use, such
// as arenas, get no dragon and no spikes.
var EndFightWorld = func(w *world.World) bool {
	return w.Dimension() == world.End && w.PortalDestination(world.End) != w
}

func endFightTick(tx *world.Tx) {
	if w := tx.World(); w.Dimension() == world.End && EndFightWorld(w) {
		FightOf(w).tick(tx)
	}
}

// DragonKilled reports whether the fight's dragon is dead (and not summoned
// again yet).
func (f *EndFight) DragonKilled() bool { return f.dragonKilled }

// ExitPortal returns the position of the exit portal (the level of its portal
// blocks), if it was built.
func (f *EndFight) ExitPortal() (cube.Pos, bool) { return f.exitPortal, f.hasExit }

// Dragon returns the fight's dragon, if one is alive and loaded.
func (f *EndFight) Dragon() *world.EntityHandle { return f.dragon }

// Respawning reports whether the dragon is being summoned again.
func (f *EndFight) Respawning() bool { return f.respawnStage != respawnNone }

// Spikes returns the spikes of the End, laid out by the level seed.
func (f *EndFight) Spikes() []EndSpike { return EndSpikes(worldSeed(f.w)) }

// Gateways returns how many gateways are still to open before the order
// starts again.
func (f *EndFight) Gateways() int { return len(f.gateways) }

// tick is EndDragonFight::tick.
func (f *EndFight) tick(tx *world.Tx) {
	if !f.playerNear(tx) {
		return
	}
	if !f.started {
		f.started = true
		if f.fresh {
			// The End of a new world: put up the spikes BDS generates with it.
			if spikes := EndSpikes(worldSeed(f.w)); spikesMissing(tx, spikes) {
				for _, s := range spikes {
					placeSpike(tx, s, false, nil)
				}
			}
		}
	}
	f.settleGateways(tx)
	for x := -1; x <= 1; x++ {
		for z := -1; z <= 1; z++ {
			if _, ok := tx.BlockLoaded(cube.Pos{x * 16, 64, z * 16}); !ok {
				return
			}
		}
	}
	if f.respawnStage != respawnNone {
		if len(f.crystals) == 0 {
			f.respawnStage, f.respawnTime = respawnNone, 0
			f.tryRespawn(tx)
			if f.respawnStage == respawnNone {
				f.save()
				return
			}
		}
		if f.respawnCrystalDestroyed(tx) {
			return
		}
		t := f.respawnTime
		f.respawnTime++
		f.tickRespawn(tx, t)
		return
	}
	if !f.dragonKilled {
		f.findOrCreateDragon(tx)
		if f.crystalTick++; f.crystalTick >= 100 {
			f.crystalTick = 0
			f.updateCrystalCount(tx)
		}
		return
	}
	// The exit portal is rebuilt if it is gone; four crystals placed on it
	// start a respawn (EndCrystalItem::useOn calls tryRespawn: here it is
	// looked for once a second).
	if f.portalTick++; f.portalTick%20 == 0 {
		f.tryRespawn(tx)
	}
	if f.portalTick >= 100 {
		f.portalTick = 0
		if !f.exitPortalActive(tx) {
			f.spawnExitPortal(tx, true)
		}
	}
}

// playerNear: a player is within 200 of the centre of the End.
func (f *EndFight) playerNear(tx *world.Tx) bool {
	for p := range tx.Players() {
		pos := p.Position()
		if math.Abs(pos[0]) <= 201 && math.Abs(pos[1]) <= 201 && math.Abs(pos[2]) <= 201 {
			return true
		}
	}
	return false
}

func handleID(h *world.EntityHandle) int64 {
	id := h.UUID()
	return int64(binary.BigEndian.Uint64(id[8:]))
}

// findOrCreateDragon finds the fight's dragon (by its id, then any dragon
// within 200 of the centre) or makes a new one with the exit portal. A
// dragon that was spawned before but is not found is waited for 10 seconds,
// as its chunk may still be loading (BDS asks its autonomous actor
// manager), before a new one is made.
func (f *EndFight) findOrCreateDragon(tx *world.Tx) {
	if f.dragon != nil {
		if _, ok := f.dragon.Entity(tx); ok {
			f.missing = 0
			return
		}
		f.dragon = nil
	}
	box := cube.Box(-200, -200, -200, 201, 201, 201)
	for e := range tx.EntitiesWithin(box) {
		if e.H().Type().EncodeEntity() != DragonID {
			continue
		}
		if m, ok := mob.Of(e); ok {
			f.initializeDragon(m)
			return
		}
	}
	if f.dragonSpawned {
		if f.missing++; f.missing < 200 {
			return
		}
	}
	f.missing = 0
	f.createNewDragon(tx)
	f.spawnExitPortal(tx, false)
}

// createNewDragon is _createNewDragon: a dragon at 0 128 0 facing a random
// way.
func (f *EndFight) createNewDragon(tx *world.Tx) {
	yaw := rand.Float64() * 360
	m, err := mob.Spawn(tx, DragonID, dragonSpawnPos.Vec3(), mob.SpawnOptions{Rotation: cube.Rotation{yaw, 0}})
	if err != nil {
		slog.Default().Warn("mob: the End fight could not make a dragon", "err", err)
		return
	}
	f.initializeDragon(m)
}

// initializeDragon is _initializeDragon: the dragon learns whether one died
// before (its experience) and reports its death to the fight.
func (f *EndFight) initializeDragon(m *mob.Mob) {
	d := DragonOf(m)
	d.fight, d.prevKilled = f, f.prevKilled
	id := handleID(m.H())
	changed := f.dragonID != id || !f.dragonSpawned
	f.dragon, f.dragonID, f.dragonSpawned = m.H(), id, true
	if changed {
		f.save()
	}
}

// onDragonKilled is the dragon killed callback set in _initializeDragon, run
// when the death animation ends: the exit portal opens, a gateway pair
// appears, and the first dragon of the world lays its egg on the portal.
func (f *EndFight) onDragonKilled(tx *world.Tx, m *mob.Mob) {
	if handleID(m.H()) != f.dragonID {
		return
	}
	f.spawnExitPortal(tx, true)
	f.spawnNewGatewayPair(tx)
	if !f.prevKilled {
		y := AboveTopSolid(tx, 0, tx.Range()[1], 0)
		tx.SetBlock(cube.Pos{0, y, 0}, block.DragonEgg{}, nil)
	}
	f.dragonKilled, f.prevKilled = true, true
	f.dragon = nil
	f.save()
}

// spawnExitPortal is _spawnExitPortal: the podium at the exit portal
// location, which is found the first time on top of the highest solid
// block of column 0 0. A column without blocks (a void End) gets it at y
// 64 (Crossfly: BDS would build it at the bottom of the world).
func (f *EndFight) spawnExitPortal(tx *world.Tx, active bool) {
	if !f.hasExit {
		r := tx.Range()
		y := AboveTopSolid(tx, 0, r[1], 0)
		for isBlock[block.Bedrock](tx, cube.Pos{0, y, 0}) && y > r[0] {
			y--
		}
		if y <= r[0] {
			y = 64
		}
		f.exitPortal, f.hasExit = cube.Pos{0, y, 0}, true
		f.save()
	}
	placePodium(tx, f.exitPortal, active)
}

// exitPortalActive reports whether end portal blocks fill the exit portal
// (BDS looks for any end portal block actor near the centre).
func (f *EndFight) exitPortalActive(tx *world.Tx) bool {
	if !f.hasExit {
		return false
	}
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			if isBlock[block.EndPortal](tx, f.exitPortal.Add(cube.Pos{dx, 0, dz})) {
				return true
			}
		}
	}
	return false
}

// updateCrystalCount is _updateCrystalCount: the crystals standing on the
// spikes, told to the dragon.
func (f *EndFight) updateCrystalCount(tx *world.Tx) {
	n := 0
	for _, s := range EndSpikes(worldSeed(f.w)) {
		n += len(crystalsIn(tx, s.topBox()))
	}
	f.numCrystals = n
	if f.dragon != nil {
		if e, ok := f.dragon.Entity(tx); ok {
			if m, ok := mob.Of(e); ok {
				DragonOf(m).NumCrystals = n
			}
		}
	}
}

// crystalsIn returns the end crystals whose box meets box
// (BlockSource::fetchEntities).
func crystalsIn(tx *world.Tx, box cube.BBox) []world.Entity {
	var out []world.Entity
	for e := range tx.EntitiesWithin(box.Grow(2)) {
		if e.H().Type() != entity.EndCrystalType {
			continue
		}
		if e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) {
			out = append(out, e)
		}
	}
	return out
}

// resetSpikeCrystals is resetSpikeCrystals: the crystals on the spikes lose
// their beams and invulnerability.
func (f *EndFight) resetSpikeCrystals(tx *world.Tx) {
	for _, s := range EndSpikes(worldSeed(f.w)) {
		for _, e := range crystalsIn(tx, s.topBox()) {
			entity.SetEndCrystalInvulnerable(e, false)
			entity.SetEndCrystalBeam(e, cube.Pos{}, false)
		}
	}
}

// tryRespawn is tryRespawn: with the dragon dead and no respawn running, an
// end crystal on each side of the exit portal (3 out, one above it) starts
// the respawn and closes the portal.
func (f *EndFight) tryRespawn(tx *world.Tx) {
	if !f.dragonKilled || f.respawnStage != respawnNone {
		return
	}
	if !f.hasExit {
		f.spawnExitPortal(tx, true)
	}
	above := f.exitPortal.Add(cube.Pos{0, 1, 0})
	var found []*world.EntityHandle
	for _, face := range []cube.Face{cube.FaceNorth, cube.FaceSouth, cube.FaceWest, cube.FaceEast} {
		p := above
		for range 3 {
			p = p.Side(face)
		}
		box := cube.Box(float64(p[0]), float64(p[1]), float64(p[2]), float64(p[0]+1), float64(p[1]+1), float64(p[2]+1))
		in := crystalsIn(tx, box)
		if len(in) == 0 {
			return
		}
		for _, e := range in {
			found = append(found, e.H())
		}
	}
	f.crystals = found
	f.respawnStage, f.respawnTime = respawnStart, 0
	f.spawnExitPortal(tx, false)
	f.save()
}

// respawnCrystalDestroyed is onCrystalDestroyed for the crystals of the
// respawn: one of them gone stops it and opens the portal again.
func (f *EndFight) respawnCrystalDestroyed(tx *world.Tx) bool {
	for _, h := range f.crystals {
		if _, ok := h.Entity(tx); ok {
			continue
		}
		f.respawnStage, f.respawnTime = respawnNone, 0
		f.crystals = nil
		f.resetSpikeCrystals(tx)
		f.spawnExitPortal(tx, true)
		f.save()
		return true
	}
	return false
}

// setRespawnStage is _setRespawnStage: the END stage brings the dragon back.
func (f *EndFight) setRespawnStage(tx *world.Tx, stage int) {
	f.respawnTime = 0
	if stage != respawnEnd {
		f.respawnStage = stage
		return
	}
	f.respawnStage = respawnNone
	f.dragonKilled = false
	f.createNewDragon(tx)
	f.save()
}

// beamCrystals points the respawn crystals' beams at target.
func (f *EndFight) beamCrystals(tx *world.Tx, target cube.Pos, ok bool) {
	for _, h := range f.crystals {
		if e, alive := h.Entity(tx); alive {
			entity.SetEndCrystalBeam(e, target, ok)
		}
	}
}

// growl is the dragon's growl (LevelSoundEvent Mad) at the spawn point.
func growl(tx *world.Tx) {
	tx.PlaySound(dragonSpawnPos.Vec3(), mob.Sound{Event: "mad", Identifier: DragonID})
}

// tickRespawn is _tickRespawnAnimation at time t of the stage.
func (f *EndFight) tickRespawn(tx *world.Tx, t int) {
	switch f.respawnStage {
	case respawnStart:
		// The crystals beam at the spawn point.
		f.beamCrystals(tx, dragonSpawnPos, true)
		f.setRespawnStage(tx, respawnPreparing)
	case respawnPreparing:
		if t >= 100 {
			f.setRespawnStage(tx, respawnPillars)
		} else if t == 0 || (t >= 50 && t <= 52) || t >= 95 {
			growl(tx)
		}
	case respawnPillars:
		// Every 40 ticks a spike: the beams point at it, at 38 it blows up
		// and at 39 it stands again with an invulnerable crystal beaming at
		// the spawn point.
		spikes := EndSpikes(worldSeed(f.w))
		n, step := t/40, t%40
		if step != 0 && step != 38 && step != 39 {
			return
		}
		if n >= len(spikes) {
			if step == 0 {
				f.setRespawnStage(tx, respawnSummoning)
			}
			return
		}
		s := spikes[n]
		switch step {
		case 0:
			f.beamCrystals(tx, cube.Pos{s.X, s.Height + 1, s.Z}, true)
		case 38:
			for x := s.X - 10; x <= s.X+10; x++ {
				for y := s.Height - 10; y <= s.Height+10; y++ {
					for z := s.Z - 10; z <= s.Z+10; z++ {
						if p := (cube.Pos{x, y, z}); !p.OutOfBounds(tx.Range()) && !isBlock[block.Air](tx, p) {
							tx.SetBlock(p, nil, nil)
						}
					}
				}
			}
			// Level::explode with no source, power 5 at the spike top.
			block.ExplosionConfig{SuppressUnderwaterImpact: true}.Explode(tx, world.BlockExplosionSource{
				Pos: cube.Pos{s.X, s.Height, s.Z}, ExplosionSize: 5,
			})
		case 39:
			target := dragonSpawnPos
			placeSpike(tx, s, true, &target)
		}
	case respawnSummoning:
		switch {
		case t >= 100:
			f.setRespawnStage(tx, respawnEnd)
			f.resetSpikeCrystals(tx)
			for _, h := range f.crystals {
				if e, ok := h.Entity(tx); ok {
					// Each crystal explodes with power 6, itself the source.
					entity.SetEndCrystalBeam(e, cube.Pos{}, false)
					_ = e.(interface{ Close() error }).Close()
					block.ExplosionConfig{SuppressUnderwaterImpact: true}.Explode(tx, world.EntityExplosionSource{Entity: e, ExplosionSize: 6})
				}
			}
			f.crystals = nil
		case t >= 80:
			growl(tx)
		case t == 0:
			f.beamCrystals(tx, dragonSpawnPos, true)
		case t < 5:
			growl(tx)
		}
	}
}

// gatewayPos is the position of gateway i on the ring of 96 around the
// centre, at y 75. Java fallback, no Bedrock data: BDS computes the angle in
// registers the dump does not show; Java's 2(-pi + 0.15707963 i) is used.
func gatewayPos(i int) cube.Pos {
	a := 2 * (-math.Pi + 0.15707963267948966*float64(i))
	return cube.Pos{int(math.Floor(96 * math.Cos(a))), 75, int(math.Floor(96 * math.Sin(a)))}
}

// spawnNewGatewayPair is _spawnNewGatewayPair + _placeAndLinkNewGatewayPair:
// the next of the 20 gateways (taken from the end of the list, which is
// refilled and shuffled by the level seed once empty) and its partner out
// on the islands, linked to each other.
func (f *EndFight) spawnNewGatewayPair(tx *world.Tx) {
	if len(f.gateways) == 0 {
		f.gateways = make([]int, 20)
		for i := range f.gateways {
			f.gateways[i] = i
		}
		newMT(worldSeed(f.w)).shuffle(f.gateways)
	}
	i := f.gateways[len(f.gateways)-1]
	f.gateways = f.gateways[:len(f.gateways)-1]
	inner := gatewayPos(i)
	placeGatewayPair(tx, inner, findExitPortal(tx, inner))
	f.settle = append(f.settle, settleGateway{pos: inner, tick: tx.CurrentTick() + 200})
}

// placeGatewayPair is _placeAndLinkNewGatewayPair: the outer gateway stands
// 10 above the highest block near the end stone found around exit (an
// island is made there if there is none); each gateway leads to ground near
// the other.
func placeGatewayPair(tx *world.Tx, inner, exit cube.Pos) cube.Pos {
	at, ok := findValidSpawnAround(tx, exit, true, 1)
	if !ok {
		at = cube.Pos{exit[0], 75, exit[2]}
		placeIsland(tx, at, newMT(hash3(at)))
	}
	outer := findTallestBlock(tx, at, 16, true).Add(cube.Pos{0, 10, 0})
	placeGateway(tx, inner, nil)
	placeGateway(tx, outer, nil)
	ground, _ := findValidSpawnAround(tx, outer, false, 1)
	setGatewayExit(tx, inner, findTallestBlock(tx, ground, 5, false).Add(cube.Pos{0, 1, 0}))
	back, _ := findValidSpawnAround(tx, inner, false, 3)
	setGatewayExit(tx, outer, back)
	return outer
}

// linkGateway finds the exit of a gateway in the End that has none
// (EndGatewayBlockActor::tick -> spawnNewGatewayChunks): the outer gateway
// for one near the centre, else ground near the centre.
func linkGateway(tx *world.Tx, pos cube.Pos) (cube.Pos, bool) {
	if !EndFightWorld(tx.World()) {
		return cube.Pos{}, false
	}
	if math.Hypot(float64(pos[0]), float64(pos[2])) < 512 {
		outer := placeGatewayPair(tx, pos, findExitPortal(tx, pos))
		_ = outer
		if g, ok := tx.Block(pos).(block.EndGateway); ok && g.HasExit {
			return g.Exit, true
		}
		return cube.Pos{}, false
	}
	back, ok := findValidSpawnAround(tx, cube.Pos{0, 75, 0}, false, 3)
	return back, ok
}

// settleGateways sets new gateways to age 200 once 200 ticks passed, so
// clients loading them later show no spawn beam.
func (f *EndFight) settleGateways(tx *world.Tx) {
	if len(f.settle) == 0 {
		return
	}
	now := tx.CurrentTick()
	kept := f.settle[:0]
	for _, s := range f.settle {
		if now < s.tick {
			kept = append(kept, s)
			continue
		}
		if g, ok := tx.Block(s.pos).(block.EndGateway); ok {
			g.Age = 200
			tx.SetBlock(s.pos, g, nil)
		}
	}
	f.settle = kept
}

// --- Saving (EndDragonFight::saveData / loadData) ---

// fightVersion is DragonFightVersion.
const fightVersion = 1

func (f *EndFight) load() {
	lp, ok := f.w.Provider().(levelDataProvider)
	if !ok {
		f.fresh = true
		return
	}
	data, err := lp.LevelData(theEndKey)
	if err != nil {
		slog.Default().Warn("mob: read the End fight", "err", err)
	}
	d, _ := data["data"].(map[string]any)
	fight, ok := d["DragonFight"].(map[string]any)
	if !ok {
		f.fresh = true
		return
	}
	flag := func(k string) bool { v, _ := fight[k].(byte); return v != 0 }
	if v, ok := fight["DragonUUID"].(int64); ok {
		f.dragonID = v
	}
	f.dragonKilled, f.dragonSpawned, f.prevKilled = flag("DragonKilled"), flag("DragonSpawned"), flag("PreviouslyKilled")
	if _, ok := fight["DragonFightVersion"]; !ok {
		// Written by an earlier Crossfly, which kept only that a dragon died
		// anywhere: the fight never ran, so its dragon is still to come.
		f.dragonKilled, f.dragonSpawned, f.fresh = false, false, true
	}
	if flag("IsRespawning") {
		f.respawnStage = respawnStart
	}
	if l, ok := fight["ExitPortalLocation"].([]int32); ok && len(l) == 3 {
		f.exitPortal, f.hasExit = cube.Pos{int(l[0]), int(l[1]), int(l[2])}, true
	} else if l, ok := fight["ExitPortalLocation"].([]any); ok && len(l) == 3 {
		x, _ := l[0].(int32)
		y, _ := l[1].(int32)
		z, _ := l[2].(int32)
		f.exitPortal, f.hasExit = cube.Pos{int(x), int(y), int(z)}, true
	}
	switch g := fight["Gateways"].(type) {
	case []int32:
		for _, v := range g {
			f.gateways = append(f.gateways, int(v))
		}
	case []any:
		for _, v := range g {
			if i, ok := v.(int32); ok {
				f.gateways = append(f.gateways, int(i))
			}
		}
	}
	if f.prevKilled {
		killedDragons.Store(dragonKillKey(f.w), true)
	}
}

func boolByte(v bool) byte {
	if v {
		return 1
	}
	return 0
}

// save writes the fight to the world's database at once, keeping the rest of
// the End's data.
func (f *EndFight) save() {
	if f.prevKilled {
		killedDragons.Store(dragonKillKey(f.w), true)
	}
	lp, ok := f.w.Provider().(levelDataProvider)
	if !ok {
		return
	}
	data, err := lp.LevelData(theEndKey)
	if err != nil {
		slog.Default().Warn("mob: read the End fight", "err", err)
		return
	}
	if data == nil {
		data = map[string]any{}
	}
	d, _ := data["data"].(map[string]any)
	if d == nil {
		d = map[string]any{}
		data["data"] = d
	}
	// Lists of ints (ListTag), not int arrays, as BDS writes them.
	gateways := make([]any, len(f.gateways))
	for i, g := range f.gateways {
		gateways[i] = int32(g)
	}
	fight := map[string]any{
		"DragonUUID":         f.dragonID,
		"DragonKilled":       boolByte(f.dragonKilled),
		"DragonSpawned":      boolByte(f.dragonSpawned),
		"PreviouslyKilled":   boolByte(f.prevKilled),
		"IsRespawning":       boolByte(f.respawnStage != respawnNone),
		"DragonFightVersion": byte(fightVersion),
		"Gateways":           gateways,
	}
	if f.hasExit {
		fight["ExitPortalLocation"] = []any{int32(f.exitPortal[0]), int32(f.exitPortal[1]), int32(f.exitPortal[2])}
	}
	d["DragonFight"] = fight
	if err := lp.SetLevelData(theEndKey, data); err != nil {
		slog.Default().Warn("mob: save the End fight", "err", err)
	}
}

// dragonDied runs the fight's callback for a dragon whose death animation
// ended.
func (d *Dragon) dragonDied(m *mob.Mob) {
	if d.fight != nil {
		d.fight.onDragonKilled(m.Tx(), m)
	}
}
