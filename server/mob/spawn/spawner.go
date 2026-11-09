//go:build crossfly_full

package spawn

import (
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Cap is a population cap of one category for the 9x9 chunks around a spawn
// attempt (BDS Dimension::getPopCap): surface and underground mobs are
// counted and capped separately.
type Cap struct{ Surface, Underground int }

// DefaultCaps are the BDS caps per population_control (Dimension::Dimension;
// the End adds 2 to the surface monster cap).
var DefaultCaps = map[string]Cap{
	"ambient":      {Surface: 0, Underground: 2},
	"animal":       {Surface: 4, Underground: 4},
	"cat":          {Surface: 4, Underground: 0},
	"monster":      {Surface: 8, Underground: 16},
	"pillager":     {Surface: 8, Underground: 8},
	"water_animal": {Surface: 40, Underground: 40},
}

// Config configures natural spawning in one world. The zero value is the
// vanilla Bedrock behaviour.
type Config struct {
	// SimulationDistance is the simulation distance in chunks (default 4):
	// chunks within it around players are spawned in. With 4, mobs spawn at
	// most 44 blocks from a player; with more, up to a rule's max distance
	// (128) but not in chunks at the edge. Below 4 nothing spawns (BDS).
	SimulationDistance int
	// MaxAttemptsPerTick bounds the spawn attempts (one column each) per
	// tick (default 8). Each ticked chunk rolls 11/2000 per tick for an
	// attempt as in BDS; rolls above the budget are dropped.
	MaxAttemptsPerTick int
	// GlobalCap is the most spawnable mobs ticked in the world before
	// natural spawning stops (default 200).
	GlobalCap int
	// Caps overrides DefaultCaps per population_control.
	Caps map[string]Cap
	// Categories, if not empty, limits spawning to these population_control
	// pools (e.g. []string{"animal"}).
	Categories []string
	// DisabledMobEvents turns off mob events ("minecraft:pillager_patrols_event",
	// "minecraft:wandering_trader_event", ...); all are on by default.
	DisabledMobEvents []string
	// ManualTick stops Enable from starting its own ticker: the caller then
	// calls Tick once per world tick (tests, or a server with a tick hook).
	ManualTick bool
	// Seed seeds the world's spawn random source (0: random).
	Seed uint64
}

func (c Config) withDefaults() Config {
	if c.SimulationDistance <= 0 {
		c.SimulationDistance = 4
	}
	if c.MaxAttemptsPerTick <= 0 {
		c.MaxAttemptsPerTick = 8
	}
	if c.GlobalCap <= 0 {
		c.GlobalCap = 200
	}
	return c
}

func (c *Config) cap(pool string, surface bool, end bool) int {
	cp, ok := c.Caps[pool]
	if !ok {
		cp, ok = DefaultCaps[pool]
	}
	if !ok {
		return 0
	}
	if surface {
		if end && pool == "monster" {
			return cp.Surface + 2
		}
		return cp.Surface
	}
	return cp.Underground
}

func (c *Config) categoryEnabled(pool string) bool {
	return len(c.Categories) == 0 || slices.Contains(c.Categories, pool)
}

func (c *Config) mobEventDisabled(ev string) bool {
	return slices.Contains(c.DisabledMobEvents, ev)
}

// worldState is the spawner state of one world. Everything but cfg, the
// flags and the driver is only touched from the world's goroutine.
type worldState struct {
	w      *world.World
	cfg    atomic.Pointer[Config] // nil: natural spawning off
	noDesp atomic.Bool

	rnd      *rand.Rand
	lastTick int64
	ran      bool

	playersTick int64
	playersOK   bool
	players     []mgl64.Vec3
	ticked      map[world.ChunkPos]struct{}
	chunks      []world.ChunkPos

	countTick          int64
	countCur, countOld int

	delays map[string]uint64

	// Stats, for debugging and tests.
	Attempts, Spawned, Dropped int
}

var states sync.Map // *world.World -> *worldState

func stateOf(w *world.World) *worldState {
	if v, ok := states.Load(w); ok {
		return v.(*worldState)
	}
	v, _ := states.LoadOrStore(w, &worldState{w: w, ticked: map[world.ChunkPos]struct{}{}, delays: map[string]uint64{}, lastTick: -1, playersTick: -1})
	return v.(*worldState)
}

// defaultConfig is used by despawning in worlds without natural spawning.
var defaultConfig = Config{}.withDefaults()

func (st *worldState) config() *Config {
	if c := st.cfg.Load(); c != nil {
		return c
	}
	return &defaultConfig
}

// Enable turns natural spawning on in w (it is off in every world until a
// server enables it). Spawning then runs once per world tick from a world
// tick hook (world.RegisterTickHook), unless cfg.ManualTick is set: then the
// caller runs Tick itself. It may be called again to change the config.
func Enable(w *world.World, cfg Config) {
	cfg = cfg.withDefaults()
	stateOf(w).cfg.Store(&cfg)
}

// Disable turns natural spawning off in w. Mobs already there stay (and
// still despawn by their own rules).
func Disable(w *world.World) { stateOf(w).cfg.Store(nil) }

// Enabled reports whether natural spawning is on in w.
func Enabled(w *world.World) bool { return stateOf(w).cfg.Load() != nil }

// SetDespawning turns the minecraft:despawn rules on (the default, in every
// world) or off in w. Off, no mob of w despawns by distance or inactivity.
func SetDespawning(w *world.World, on bool) { stateOf(w).noDesp.Store(!on) }

// worldTick is the world tick hook: natural spawning in every world that
// enabled it without ManualTick.
func worldTick(tx *world.Tx) {
	v, ok := states.Load(tx.World())
	if !ok {
		return
	}
	if cfg := v.(*worldState).cfg.Load(); cfg != nil && !cfg.ManualTick {
		Tick(tx)
	}
}

// Tick runs one tick of natural spawning in tx's world, if it is enabled
// there. The world tick hook calls it; with Config.ManualTick the caller does,
// once per world tick (extra calls in the same tick do nothing).
func Tick(tx *world.Tx) {
	st := stateOf(tx.World())
	cfg := st.cfg.Load()
	if cfg == nil {
		return
	}
	tick := tx.CurrentTick()
	if st.ran && tick == st.lastTick {
		return
	}
	st.ran, st.lastTick = true, tick
	if st.rnd == nil {
		seed := cfg.Seed
		if seed == 0 {
			seed = rand.Uint64()
		}
		st.rnd = rand.New(rand.NewPCG(seed, seed^0x5851f42d4c957f2d))
	}
	st.refreshPlayers(tx, cfg)
	if len(st.players) == 0 {
		return
	}
	e := st.newEnv(tx, cfg)
	attempts := 0
	n := len(st.chunks)
	start := st.rnd.IntN(n)
	for i := 0; i < n; i++ {
		c := st.chunks[(start+i)%n]
		// Spawner::tick: each ticked chunk tries with 11 in 2000 per tick.
		if st.rnd.IntN(2000) > 10 {
			continue
		}
		if attempts >= cfg.MaxAttemptsPerTick {
			st.Dropped++
			continue
		}
		if _, ok := tx.BlockLoaded(cube.Pos{int(c[0]) << 4, e.rng[0], int(c[1]) << 4}); !ok {
			continue
		}
		attempts++
		st.Attempts++
		st.spawnInChunk(e, cfg, c)
	}
}

func (st *worldState) newEnv(tx *world.Tx, cfg *Config) *env {
	w := tx.World()
	diff, _ := world.DifficultyID(w.Difficulty())
	t := w.Time()
	rain, thunder := tx.Raining(), tx.Thundering()
	return &env{
		tx: tx, st: st, cfg: cfg, rnd: st.rnd, difficulty: diff,
		ageSeconds: uint64(max(tx.CurrentTick(), 0)) / 20,
		skyDarken:  skyDarken(t, rain, thunder), thunder: rain && thunder,
		moon: moonBrightness(t), rng: tx.Range(),
	}
}

// refreshPlayers caches the interactable players (not spectators, alive)
// and the chunks ticked around them, once per tick.
func (st *worldState) refreshPlayers(tx *world.Tx, cfg *Config) {
	tick := tx.CurrentTick()
	if st.playersOK && st.playersTick == tick {
		return
	}
	st.playersOK, st.playersTick = true, tick
	st.players = st.players[:0]
	clear(st.ticked)
	st.chunks = st.chunks[:0]
	r := cfg.SimulationDistance
	for e := range tx.Players() {
		p, ok := e.(*player.Player)
		if !ok || p.Dead() || !p.GameMode().HasCollision() {
			continue
		}
		pos := p.Position()
		st.players = append(st.players, pos)
		pc := chunkOf(cube.PosFromVec3(pos))
		for dx := -r; dx <= r; dx++ {
			for dz := -r; dz <= r; dz++ {
				if dx*dx+dz*dz > r*r {
					continue
				}
				c := world.ChunkPos{pc[0] + int32(dx), pc[1] + int32(dz)}
				if _, ok := st.ticked[c]; !ok {
					st.ticked[c] = struct{}{}
					st.chunks = append(st.chunks, c)
				}
			}
		}
	}
	slices.SortFunc(st.chunks, func(a, b world.ChunkPos) int {
		if a[0] != b[0] {
			return int(a[0] - b[0])
		}
		return int(a[1] - b[1])
	})
}

// untickedNeighbour is BlockSource::hasUntickedNeighborChunk(c, 1).
func (st *worldState) untickedNeighbour(c world.ChunkPos) bool {
	for dx := int32(-1); dx <= 1; dx++ {
		for dz := int32(-1); dz <= 1; dz++ {
			if _, ok := st.ticked[world.ChunkPos{c[0] + dx, c[1] + dz}]; !ok {
				return true
			}
		}
	}
	return false
}

// nearestPlayer is Dimension::fetchNearestInteractablePlayer.
func (st *worldState) nearestPlayer(pos mgl64.Vec3, maxDist float64) (mgl64.Vec3, bool) {
	best, found := maxDist*maxDist, false
	var out mgl64.Vec3
	for _, p := range st.players {
		if d := p.Sub(pos).LenSqr(); d <= best {
			best, out, found = d, p, true
		}
	}
	return out, found
}

func (st *worldState) anyPlayerWithin(pos mgl64.Vec3, dist float64) bool {
	_, ok := st.nearestPlayer(pos, dist)
	return ok
}

// countSpawnable counts a ticked mob that has spawn rules (Spawner::
// incrementSpawnableTickedMob); the global cap reads the last full tick.
func (st *worldState) countSpawnable(tick int64) {
	if tick != st.countTick {
		if tick == st.countTick+1 {
			st.countOld = st.countCur
		} else {
			st.countOld = 0
		}
		st.countCur, st.countTick = 0, tick
	}
	st.countCur++
}

func (st *worldState) spawnableTicked(tick int64) int {
	switch tick {
	case st.countTick:
		return max(st.countCur, st.countOld)
	case st.countTick + 1:
		return st.countCur
	}
	return 0
}

// mobCounts are Spawner's per-attempt counts of the 9x9 chunks around the
// attempt (Spawner::_updateBaseTypeCount): per category and per
// identifier, for surface (1) and underground (0) mobs.
type mobCounts struct {
	cat [2]map[string]int
	ids [2]map[string]int
}

func idx(surface bool) int {
	if surface {
		return 1
	}
	return 0
}

func (c *mobCounts) add(id string, surface bool) {
	s := idx(surface)
	c.ids[s][id]++
	if pool := PopulationControl(id); pool != "" {
		c.cat[s][pool]++
	}
}

func countAround(tx *world.Tx, c world.ChunkPos) *mobCounts {
	mc := &mobCounts{}
	for i := range 2 {
		mc.cat[i], mc.ids[i] = map[string]int{}, map[string]int{}
	}
	rng := tx.Range()
	box := cube.Box(float64(int(c[0]-4)<<4), float64(rng[0]), float64(int(c[1]-4)<<4),
		float64(int(c[0]+5)<<4), float64(rng[1]+1), float64(int(c[1]+5)<<4))
	for e := range tx.EntitiesWithin(box) {
		m, ok := mob.Of(e)
		if !ok {
			continue
		}
		mc.add(m.Identifier(), infoOf(m).surface)
	}
	return mc
}

// spawnInChunk is the body of Spawner::tick for one chunk: a random column,
// the surface floor first, then every floor below it (caves).
func (st *worldState) spawnInChunk(e *env, cfg *Config, c world.ChunkPos) {
	tx := e.tx
	x, z := int(c[0])<<4+e.rnd.IntN(16), int(c[1])<<4+e.rnd.IntN(16)
	counts := countAround(tx, c)
	top := min(tx.HighestBlock(x, z)+1, e.rng[1])
	pos := cube.Pos{x, top, z}
	found := findNextSpawnBlockUnder(tx, &pos, true)
	cond := conditions{surface: true}
	cond.inWater = isWater(tx, pos.Add(cube.Pos{0, 1})) && isWater(tx, pos.Add(cube.Pos{0, 2}))
	cond.inLava = isLava(tx, pos.Add(cube.Pos{0, 1})) && isLava(tx, pos.Add(cube.Pos{0, 2}))
	if found {
		st.spawnCluster(e, cfg, counts, pos, cond)
	}
	// Below the surface: underground conditions. BDS keeps the surface flag
	// only while the floors are leaves, which are never floors here.
	for findNextSpawnBlockUnder(tx, &pos, false) {
		under := conditions{underground: true}
		under.inWater, under.inLava = liquidAt(tx, pos.Add(cube.Pos{0, 1}))
		st.spawnCluster(e, cfg, counts, pos, under)
	}
}

func isWater(tx *world.Tx, pos cube.Pos) bool { w, _ := liquidAt(tx, pos); return w }
func isLava(tx *world.Tx, pos cube.Pos) bool  { _, l := liquidAt(tx, pos); return l }

// spawnCluster is Spawner::_spawnMobCluster: floor is the block the herd
// stands on.
func (st *worldState) spawnCluster(e *env, cfg *Config, counts *mobCounts, floor cube.Pos, cond conditions) {
	tx := e.tx
	if _, air := tx.Block(floor).(block.Air); air {
		return
	}
	sp := floor.Add(cube.Pos{0, 1})
	cond.pos = sp
	cond.brightness = rawBrightness(tx, sp, e.skyDarken)
	rule := st.pickRule(e, cfg, &cond)
	if rule == nil {
		return
	}
	herd := rule.selectHerd(e.rnd)
	cond.delayEnd = st.delayEnd(rule, e)
	n := rule.spawnCount(&cond, e, herd)
	if n <= 0 || !rule.isSpawnPositionOk(sp, e) {
		return
	}
	if cond.delayEnd != 0 && rule.DelayChance != 100 && e.rnd.IntN(100) > rule.DelayChance {
		st.resetDelay(rule, e)
		return
	}
	n = st.populationCap(rule, cfg, &cond, n, counts, tx.CurrentTick())
	id := rule.Identifier
	if rule.SpawnEvent != "" {
		id += "<" + rule.SpawnEvent + ">"
	}
	end := tx.World().Dimension() == world.End
	var spawned []*mob.Mob
	for ; n > 0; n-- {
		if rule.Pool == "" {
			continue
		}
		limit := cfg.cap(rule.Pool, cond.surface, end)
		if limit <= 0 || counts.cat[idx(cond.surface)][rule.Pool] >= limit {
			continue
		}
		id = rule.permute(id, e.rnd)
		if m := st.spawnOne(e, id, floor, &cond); m != nil {
			spawned = append(spawned, m)
			ident, _ := splitEvent(id)
			counts.add(ident, cond.surface)
		}
	}
	if cond.delayEnd != 0 && len(spawned) > 0 {
		st.resetDelay(rule, e)
	}
	// Spawner::_sendHerdEvents.
	for i, m := range spawned {
		if m.H().Closed() || m.Dead() {
			continue
		}
		if i < herd.InitialEventCount && herd.InitialEvent != "" {
			m.Fire(herd.InitialEvent, nil)
		}
		if i >= herd.EventSkipCount && herd.Event != "" {
			m.Fire(herd.Event, nil)
		}
		if rule.Persistent {
			m.SetPersistent(true)
		}
	}
}

// pickRule is BlockLegacy::getMobToSpawn: a weighted random rule among the
// biome's rules that can spawn in the conditions.
func (st *worldState) pickRule(e *env, cfg *Config, cond *conditions) *Rule {
	cands := rulesFor(e.tx.Biome(cond.pos))
	var ok [32]*Rule
	list := ok[:0]
	total := 0
	for _, r := range cands {
		if !cfg.categoryEnabled(r.Pool) {
			continue
		}
		c := *cond
		c.delayEnd = st.delays[r.DelayID]
		if r.canSpawnInConditions(&c, e) {
			list = append(list, r)
			total += r.Weight
		}
	}
	if len(list) == 0 {
		return nil
	}
	v := 0
	if total > 0 {
		v = e.rnd.IntN(total)
	}
	for _, r := range list {
		if v -= r.Weight; v < 0 {
			return r
		}
	}
	return nil
}

// permute is Spawner::_permuteId.
func (r *Rule) permute(id string, rnd randSource) string {
	if len(r.Permutations) == 0 {
		return id
	}
	v := 0
	if r.permuteWeightTotal > 0 {
		v = rnd.IntN(r.permuteWeightTotal)
	}
	for _, p := range r.Permutations {
		if v -= p.Weight; v < 0 {
			if p.EntityType != "" {
				return p.EntityType
			}
			return id
		}
	}
	return id
}

// populationCap is Spawner::_handlePopulationCap: the global cap and the
// rule's density_limit.
func (st *worldState) populationCap(r *Rule, cfg *Config, cond *conditions, n int, counts *mobCounts, tick int64) int {
	if g := st.spawnableTicked(tick); g+n > cfg.GlobalCap {
		n = max(0, cfg.GlobalCap-g)
	}
	limit := r.DensityUnderground
	if cond.surface {
		limit = r.DensitySurface
	}
	if limit >= 0 {
		if have := counts.ids[idx(cond.surface)][r.Identifier]; have+n >= limit {
			return limit - have
		}
	}
	return n
}

func (st *worldState) delayEnd(r *Rule, e *env) uint64 {
	if r.DelayID == "" {
		return 0
	}
	end, ok := st.delays[r.DelayID]
	if !ok {
		// The first delay starts with the world (ActorSpawnRuleGroup sets
		// every delay when the rules load).
		end = e.ageSeconds + uint64(r.DelayMin+e.rnd.IntN(r.DelayMax-r.DelayMin+1))
		st.delays[r.DelayID] = end
	}
	return end
}

func (st *worldState) resetDelay(r *Rule, e *env) {
	if r.DelayID == "" {
		return
	}
	st.delays[r.DelayID] = e.ageSeconds + uint64(r.DelayMin+e.rnd.IntN(r.DelayMax-r.DelayMin+1))
}

// splitEvent splits "minecraft:pillager<minecraft:spawn_as_patrol_follower>".
func splitEvent(id string) (string, string) {
	if i := strings.IndexByte(id, '<'); i >= 0 && strings.HasSuffix(id, ">") {
		return id[:i], id[i+1 : len(id)-1]
	}
	return id, ""
}

// spawnOne is Spawner::_spawnMobInCluster + spawnMob: the mob spawns at the
// floor block's centre (+0.49, the 1.21 offset) on top of it, if
// Mob::checkSpawnRules passes.
func (st *worldState) spawnOne(e *env, id string, floor cube.Pos, cond *conditions) *mob.Mob {
	ident, ev := splitEvent(id)
	t, ok := mob.TypeFor(ident)
	if !ok {
		return nil
	}
	pos := mgl64.Vec3{float64(floor[0]) + 0.49, float64(floor[1] + 1), float64(floor[2]) + 0.49}
	if f, ok := actorSpawnRules[ident]; ok {
		// The mob's own checkSpawnRules may move it (phantoms).
		if pos, ok = f(e, t, pos); !ok {
			return nil
		}
	} else if !checkSpawnRules(e.tx, t, pos, floor) {
		return nil
	}
	if monsterSpawnTypes[ident] && !monsterDarkEnough(e, floor.Side(cube.FaceUp)) {
		return nil
	}
	surface := cond.surface
	m, err := mob.Spawn(e.tx, ident, pos, mob.SpawnOptions{
		Rotation: cube.Rotation{e.rnd.Float64()*360 - 180, 0},
		Event:    ev,
		Configure: func(m *mob.Mob) {
			in := infoOf(m)
			in.surface, in.natural = surface, true
		},
	})
	if err != nil {
		return nil
	}
	st.Spawned++
	return m
}

// typeInfo is what checkSpawnRules needs of a type before the mob exists:
// the base collision box and scale, and fire immunity.
type typeInfo struct {
	w, h       float64
	fireImmune bool
}

var typeInfos sync.Map // *mob.Type -> typeInfo

func infoForType(t *mob.Type) typeInfo {
	if v, ok := typeInfos.Load(t); ok {
		return v.(typeInfo)
	}
	ti := typeInfo{w: 0.6, h: 1.8}
	scale := 1.0
	for _, c := range t.Definition().NewInstance().Components() {
		switch c.Key {
		case "minecraft:collision_box":
			var b struct{ Width, Height *float64 }
			if jsonUnmarshal(c.Raw, &b) == nil {
				if b.Width != nil {
					ti.w = *b.Width
				}
				if b.Height != nil {
					ti.h = *b.Height
				}
			}
		case "minecraft:scale":
			var s struct{ Value *float64 }
			if jsonUnmarshal(c.Raw, &s) == nil && s.Value != nil {
				scale = *s.Value
			}
		case "minecraft:fire_immune":
			ti.fireImmune = true
		}
	}
	ti.w, ti.h = ti.w*scale, ti.h*scale
	v, _ := typeInfos.LoadOrStore(t, ti)
	return v.(typeInfo)
}

// checkSpawnRules is Mob::checkSpawnRules: the box must not collide with
// blocks, and only fire-immune mobs spawn on magma blocks and campfires.
func checkSpawnRules(tx *world.Tx, t *mob.Type, pos mgl64.Vec3, floor cube.Pos) bool {
	ti := infoForType(t)
	box := cube.Box(pos[0]-ti.w/2, pos[1], pos[2]-ti.w/2, pos[0]+ti.w/2, pos[1]+ti.h, pos[2]+ti.w/2).Grow(-1e-4)
	for _, bb := range entity.BlockBBoxesAround(tx, box) {
		if bb.IntersectsWith(box) {
			return false
		}
	}
	if !ti.fireImmune {
		switch tx.Block(floor).(type) {
		case block.Magma, block.Campfire:
			return false
		}
	}
	return true
}

// randSource is the subset of *rand.Rand the spawner uses.
type randSource interface {
	IntN(n int) int
	Float32() float32
	Float64() float64
}

// Stats returns the natural spawning counters of w since it was first
// ticked: spawn columns tried, mobs spawned and attempts dropped over the
// per-tick budget. Read it from the world goroutine (inside a transaction).
func Stats(w *world.World) (attempts, spawned, dropped int) {
	st := stateOf(w)
	return st.Attempts, st.Spawned, st.Dropped
}
