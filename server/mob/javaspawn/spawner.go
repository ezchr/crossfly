//go:build crossfly_full

package javaspawn

import (
	"math"
	"math/rand/v2"
	"slices"
	"sync"
	"sync/atomic"
	"weak"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Stats are a world's spawning counters since spawning was first ticked
// there. Read them in the world's transaction.
type Stats struct {
	// Spawned counts the naturally spawned mobs per category (phantoms
	// included); Despawned the mobs removed by checkDespawn.
	Spawned, Despawned [numCategories]int
	// Positions counts the spawnCategoryForPosition calls.
	Positions int
}

// playerInfo is a player spawning considers (not a spectator, alive) and its
// per-player mob counts (Paper ServerPlayer.mobCounts).
type playerInfo struct {
	p        *player.Player
	pos      mgl64.Vec3
	chunk    world.ChunkPos
	creative bool
	counts   [numCategories]int
}

type charge struct {
	pos    cube.Pos
	charge float64
}

// worldState is the spawning state of a world. All but cfg and noDespawn is
// only touched from the world's goroutine.
type worldState struct {
	cfg       atomic.Pointer[Config]
	noDespawn atomic.Bool

	rnd      *rand.Rand
	ran      bool
	lastTick int64

	players   []*playerInfo
	chunkSet  map[world.ChunkPos]struct{}
	chunks    []world.ChunkPos
	potential []charge
	tracked   []Mob
	// natural holds the mobs this package spawned, for mobs that do not
	// implement NaturallySpawned.
	natural     map[weak.Pointer[world.EntityHandle]]struct{}
	pruneTick   int64
	phantomNext int

	Stats Stats
}

var states sync.Map // *world.World -> *worldState

func stateOf(w *world.World) *worldState {
	if v, ok := states.Load(w); ok {
		return v.(*worldState)
	}
	v, _ := states.LoadOrStore(w, &worldState{chunkSet: map[world.ChunkPos]struct{}{},
		natural: map[weak.Pointer[world.EntityHandle]]struct{}{}, lastTick: -1})
	return v.(*worldState)
}

var defaultConfig = (&Config{}).withDefaults()

// Enable turns Java natural spawning on in w with cfg (the zero Config is
// Paper's defaults). It may be called again to change the config.
func Enable(w *world.World, cfg Config) { stateOf(w).cfg.Store(cfg.withDefaults()) }

// Disable turns natural spawning off in w. Despawning goes on.
func Disable(w *world.World) { stateOf(w).cfg.Store(nil) }

// Enabled reports whether natural spawning is on in w.
func Enabled(w *world.World) bool { return stateOf(w).cfg.Load() != nil }

// SetDespawning turns despawning (Mob.checkDespawn) on, the default in
// every world once RegisterHook ran, or off in w.
func SetDespawning(w *world.World, on bool) { stateOf(w).noDespawn.Store(!on) }

// WorldStats returns the spawning counters of w. Call it in w's transaction.
func WorldStats(w *world.World) Stats { return stateOf(w).Stats }

var registerHook sync.Once

// RegisterHook registers the world tick hook spawning and despawning run
// from. The Java mob mode calls it; until then, nothing of this package
// runs, so Bedrock mode and mobs=off pay nothing.
func RegisterHook() {
	registerHook.Do(func() { world.RegisterTickHook(worldTick) })
}

func worldTick(tx *world.Tx) {
	st := stateOf(tx.World())
	if cfg := st.cfg.Load(); cfg != nil && cfg.ManualTick {
		return
	}
	Tick(tx)
}

// env is one tick's view of the world.
type env struct {
	tx  *world.Tx
	st  *worldState
	cfg *Config
	rnd randSource
	reg Registry

	dimension          world.Dimension
	dim                *dimensionType
	rng                cube.Range
	difficulty         int
	skyDarken          int
	thundering         bool
	seaLevel           int
	surfaceSlimeChance float32
	gameTime           int64
	dayTime            int
	spawn              cube.Pos
	// spawner is set for the checks of a monster spawner (EntitySpawnReason.SPAWNER): the spawn rules that
	// test EntitySpawnReason.isSpawner skip what they skip for it.
	spawner bool
}

func (e *env) peaceful() bool { return e.difficulty == 0 }

// worldSurface is the WORLD_SURFACE heightmap: the y above the highest
// non-air block, or the minimum y in an empty column.
func (e *env) worldSurface(x, z int) int {
	y := e.tx.HighestBlock(x, z)
	if y <= e.rng[0] && isAir(e.tx, cube.Pos{x, e.rng[0], z}) {
		return e.rng[0]
	}
	return y + 1
}

func (e *env) loaded(pos cube.Pos) bool {
	if pos[1] < e.rng[0] || pos[1] > e.rng[1] {
		return false
	}
	_, ok := e.tx.BlockLoaded(pos)
	return ok
}

func chunkLoaded(tx *world.Tx, c world.ChunkPos) bool {
	_, ok := tx.BlockLoaded(cube.Pos{int(c[0]) << 4, tx.Range()[0], int(c[1]) << 4})
	return ok
}

// Tick runs a tick of spawning and despawning in tx's world. The tick hook
// calls it; with Config.ManualTick the caller does (more calls in the same
// tick do nothing).
func Tick(tx *world.Tx) {
	reg := currentRegistry()
	if reg == nil {
		return
	}
	st := stateOf(tx.World())
	cfg := st.cfg.Load()
	despawn := !st.noDespawn.Load()
	if cfg == nil && !despawn {
		return
	}
	tick := tx.CurrentTick()
	if st.ran && tick == st.lastTick {
		return
	}
	st.ran, st.lastTick = true, tick
	spawning := cfg != nil
	if cfg == nil {
		cfg = defaultConfig
	}
	if st.rnd == nil {
		seed := cfg.RandSeed
		if seed == 0 {
			seed = rand.Uint64()
		}
		st.rnd = rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	}
	e := st.newEnv(tx, cfg, reg)
	st.collectPlayers(tx)
	// ServerChunkCache.tickChunks: count the mobs, spawn, then the entity
	// ticks (Mob.checkDespawn) and the custom spawners.
	st.countMobs(e, spawning)
	if spawning && len(st.players) > 0 {
		st.spawnTick(e)
	}
	if despawn {
		for _, m := range st.tracked {
			st.checkDespawn(e, m)
		}
	}
	clear(st.tracked)
	st.tracked = st.tracked[:0]
	if spawning {
		st.tickPhantoms(e)
	}
	insomniaTick(tx)
	if tick-st.pruneTick >= 6000 {
		st.pruneTick = tick
		for k := range st.natural {
			if k.Value() == nil {
				delete(st.natural, k)
			}
		}
	}
}

func (st *worldState) newEnv(tx *world.Tx, cfg *Config, reg Registry) *env {
	w := tx.World()
	diff, _ := world.DifficultyID(w.Difficulty())
	t := w.Time()
	rain, thunder := tx.Raining(), tx.Thundering()
	d := w.Dimension()
	return &env{
		tx: tx, st: st, cfg: cfg, rnd: st.rnd, reg: reg,
		dimension: d, dim: dimensionTypeOf(d), rng: tx.Range(), difficulty: diff,
		skyDarken: skyDarken(d, t, rain, thunder), thundering: rain && thunder,
		seaLevel: cfg.seaLevel(d), surfaceSlimeChance: surfaceSlimeSpawnChance(t),
		gameTime: tx.CurrentTick(), dayTime: t, spawn: w.Spawn(),
	}
}

// collectPlayers lists the players that affect spawning
// (EntitySelector.PLAYER_AFFECTS_SPAWNING: alive, not spectating) with their
// mob counts reset (ServerChunkCache.tickChunks).
func (st *worldState) collectPlayers(tx *world.Tx) {
	st.players = st.players[:0]
	for ent := range tx.Players() {
		p, ok := ent.(*player.Player)
		if !ok || p.Dead() || p.GameMode() == world.GameModeSpectator {
			continue
		}
		pos := p.Position()
		st.players = append(st.players, &playerInfo{p: p, pos: pos, chunk: chunkOf(cube.PosFromVec3(pos)),
			creative: p.GameMode() == world.GameModeCreative})
	}
}

func chunkOf(pos cube.Pos) world.ChunkPos {
	return world.ChunkPos{int32(pos[0] >> 4), int32(pos[2] >> 4)}
}

func chebyshev(a, b world.ChunkPos) int32 {
	return max(abs32(a[0]-b[0]), abs32(a[1]-b[1]))
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// countMobs is NaturalSpawner.createState with Paper's per-player mob
// spawns: every mob that is not persistent, not misc and naturally spawned
// (count-all-mobs-for-spawning off) counts for each player whose
// simulation distance covers its chunk (ChunkMap.updatePlayerMobTypeMap),
// and adds its spawn cost charge to the spawn potential.
func (st *worldState) countMobs(e *env, spawning bool) {
	st.potential = st.potential[:0]
	for ent := range e.tx.Entities() {
		m, ok := e.reg.Mob(ent)
		if !ok {
			continue
		}
		st.tracked = append(st.tracked, m)
		if !spawning {
			continue
		}
		t, ok := entityTypes[m.JavaType()]
		if !ok || t.Category == CategoryMisc || isPersistent(m) {
			continue
		}
		if !e.cfg.CountAllMobsForSpawning && !st.naturallySpawned(m) {
			continue
		}
		pos := cube.PosFromVec3(m.Position())
		if c, ok := e.biomeAt(pos).settings.Costs[t.ID]; ok {
			st.potential = append(st.potential, charge{pos, c.Charge})
		}
		st.countFor(e, chunkOf(pos), t.Category)
	}
}

// countFor adds a mob in chunk c to the counts of the players around it.
func (st *worldState) countFor(e *env, c world.ChunkPos, cat Category) {
	sim := int32(e.cfg.SimulationDistance)
	for _, p := range st.players {
		if chebyshev(c, p.chunk) <= sim {
			p.counts[cat]++
		}
	}
}

func isPersistent(m Mob) bool {
	if p, ok := m.(persistenceMob); ok && p.PersistenceRequired() {
		return true
	}
	if p, ok := m.(customPersistenceMob); ok && p.RequiresCustomPersistence() {
		return true
	}
	return false
}

func (st *worldState) naturallySpawned(m Mob) bool {
	if n, ok := m.(naturalMob); ok {
		return n.NaturallySpawned()
	}
	_, ok := st.natural[weak.Make(m.H())]
	return ok
}

// spawnTick is the spawning half of ServerChunkCache.tickChunks: the
// categories of this tick (NaturalSpawner.getFilteredSpawningCategories),
// the chunks near players (ChunkMap.collectSpawningChunks) and
// NaturalSpawner.spawnForChunk in each.
func (st *worldState) spawnTick(e *env) {
	var buf [len(spawningCategories)]Category
	cats := st.tickCategories(e, buf[:0])
	if len(cats) == 0 {
		return
	}
	st.collectChunks(e)
	if len(st.chunks) == 0 {
		return
	}
	// Paper does not shuffle the chunks with per-player mob spawns; its
	// order is that of its ticking chunk list. Start at a random chunk so
	// no area is favoured.
	start := e.rnd.IntN(len(st.chunks))
	for i := range st.chunks {
		c := st.chunks[(start+i)%len(st.chunks)]
		for _, cat := range cats {
			// spawnForChunk: the room left under the fullest cap of the
			// players around the chunk.
			limit := st.limit(e, cat)
			minDiff := math.MaxInt
			sim := int32(e.cfg.SimulationDistance)
			for _, p := range st.players {
				if chebyshev(c, p.chunk) <= sim {
					minDiff = min(minDiff, limit-p.counts[cat])
				}
			}
			if minDiff == math.MaxInt || minDiff <= 0 {
				continue
			}
			st.spawnCategoryForChunk(e, cat, c, minDiff)
		}
	}
}

// tickCategories is NaturalSpawner.getFilteredSpawningCategories with
// Paper's per-player mob spawns: the categories whose ticks-per divides the
// game time and whose limit is not 0; monsters only with spawnEnemies
// (difficulty not peaceful, spawn_monsters), creatures (persistent) only on
// the animal-spawns ticks (every 400).
func (st *worldState) tickCategories(e *env, out []Category) []Category {
	spawnEnemies := !e.peaceful() && !e.cfg.NoMonsters
	tpc := e.cfg.ticksPer(CategoryCreature)
	spawnPersistent := tpc > 0 && e.gameTime%int64(tpc) == 0
	for _, c := range spawningCategories {
		tp := e.cfg.ticksPer(c)
		if tp <= 0 || e.gameTime%int64(tp) != 0 || st.limit(e, c) == 0 {
			continue
		}
		if !c.Friendly() && !spawnEnemies {
			continue
		}
		if c.Persistent() && !spawnPersistent {
			continue
		}
		out = append(out, c)
	}
	return out
}

func (st *worldState) limit(e *env, c Category) int {
	l := e.cfg.spawnLimit(c)
	if l < 0 {
		return DefaultSpawnLimits[c]
	}
	return l
}

// collectChunks is ChunkMap.collectSpawningChunks: loaded chunks within the
// simulation distance of a player whose centre is closer to that player
// than min(mob-spawn-range, 8) chunks (isChunkNearPlayer).
func (st *worldState) collectChunks(e *env) {
	clear(st.chunkSet)
	st.chunks = st.chunks[:0]
	r := int32(min(e.cfg.MobSpawnRange, 8))
	blockRange := float64((r << 4) * (r << 4))
	sim := int32(e.cfg.SimulationDistance)
	for _, p := range st.players {
		reach := min(r, sim)
		for dx := -reach; dx <= reach; dx++ {
			for dz := -reach; dz <= reach; dz++ {
				c := world.ChunkPos{p.chunk[0] + dx, p.chunk[1] + dz}
				if _, ok := st.chunkSet[c]; ok {
					continue
				}
				// ChunkMap.euclideanDistanceSquared: chunk centre to
				// player, horizontally.
				cx, cz := float64(int(c[0])<<4+8)-p.pos[0], float64(int(c[1])<<4+8)-p.pos[2]
				if cx*cx+cz*cz >= blockRange || !chunkLoaded(e.tx, c) {
					continue
				}
				st.chunkSet[c] = struct{}{}
				st.chunks = append(st.chunks, c)
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

// spawnCategoryForChunk is NaturalSpawner.spawnCategoryForChunk: a random
// column of the chunk and a random y from the bottom of the world to one
// above the WORLD_SURFACE heightmap.
func (st *worldState) spawnCategoryForChunk(e *env, cat Category, c world.ChunkPos, maxSpawns int) {
	x, z := int(c[0])<<4+e.rnd.IntN(16), int(c[1])<<4+e.rnd.IntN(16)
	// LevelChunk.getHeight(WORLD_SURFACE) + 1 = the heightmap value.
	top := e.worldSurface(x, z)
	y := e.rng[0] + e.rnd.IntN(top-e.rng[0]+1) // Mth.randomBetweenInclusive
	if y < e.rng[0]+1 {
		return
	}
	st.spawnCategoryForPosition(e, cat, c, cube.Pos{x, y, z}, maxSpawns)
}

// nearestPlayer is Level.getNearestPlayer(x, y, z, -1, false): the nearest
// player that is not a spectator, and its squared distance.
func (st *worldState) nearestPlayer(pos mgl64.Vec3) (*playerInfo, float64) {
	var best *playerInfo
	bestD := -1.0
	for _, p := range st.players {
		if d := p.pos.Sub(pos).LenSqr(); bestD < 0 || d < bestD {
			best, bestD = p, d
		}
	}
	return best, bestD
}

// spawnCategoryForPosition is NaturalSpawner.spawnCategoryForPosition: up to
// three packs, each up to ceil(4*rand) attempts (then the spawner data's
// count) wandering ±5 blocks per step from the start, at the start's y.
func (st *worldState) spawnCategoryForPosition(e *env, cat Category, c world.ChunkPos, start cube.Pos, maxSpawns int) {
	st.Stats.Positions++
	if !e.loaded(start) || isRedstoneConductor(e.tx, start, e.tx.Block(start)) {
		return
	}
	y := start[1]
	clusterSize := 0
	for range 3 {
		x, z := start[0], start[2]
		var data *Spawner
		var group any
		maxN := int(math.Ceil(float64(e.rnd.Float32() * 4)))
		groupSize := 0
		for ll := 0; ll < maxN; ll++ {
			x += e.rnd.IntN(6) - e.rnd.IntN(6)
			z += e.rnd.IntN(6) - e.rnd.IntN(6)
			pos := cube.Pos{x, y, z}
			xx, zz := float64(x)+.5, float64(z)+.5
			nearest, d2 := st.nearestPlayer(mgl64.Vec3{xx, float64(y), zz})
			if nearest == nil || !e.loaded(pos) || !e.isRightDistanceToPlayerAndSpawnPoint(c, pos, d2) {
				continue
			}
			if data == nil {
				s, ok := e.getRandomSpawnMobAt(cat, pos)
				if !ok {
					break
				}
				data = &s
				maxN = s.sampleCount(e.rnd)
			}
			if !e.isValidSpawnPositionForType(cat, data, pos, d2) || !st.canSpawn(e, data.Type, pos) {
				continue
			}
			t := entityTypes[data.Type]
			if !e.reg.Has(t.ID) {
				return // getMobForSpawn returned null
			}
			if !e.isValidPositionForMob(t, pos, xx, zz, d2) {
				continue
			}
			yaw := float64(e.rnd.Float32() * 360)
			m, g, ok := e.reg.Spawn(e.tx, t.ID, mgl64.Vec3{xx, float64(y), zz}, yaw, group)
			if !ok || m == nil {
				return
			}
			group = g
			st.afterSpawn(e, m, t)
			clusterSize++
			groupSize++
			if clusterSize >= maxSpawns || clusterSize >= maxSpawnClusterSize(t.ID) {
				return
			}
			if gm, ok := m.(groupSizeMob); ok && gm.IsMaxGroupSizeReached(groupSize) {
				break
			}
		}
	}
}

// afterSpawn is SpawnState.afterSpawn plus Paper's trackEntity
// (updatePlayerMobTypeMap): the new mob's charge and its count for the
// players around it.
func (st *worldState) afterSpawn(e *env, m Mob, t *EntityType) {
	st.natural[weak.Make(m.H())] = struct{}{}
	pos := cube.PosFromVec3(m.Position())
	if c, ok := e.biomeAt(pos).settings.Costs[t.ID]; ok {
		st.potential = append(st.potential, charge{pos, c.Charge})
	}
	st.countFor(e, chunkOf(pos), t.Category)
	st.Stats.Spawned[t.Category]++
}

// canSpawn is SpawnState.canSpawn: types with a spawn cost in the biome
// only spawn while the potential energy change stays in its budget.
func (st *worldState) canSpawn(e *env, typ string, pos cube.Pos) bool {
	c, ok := e.biomeAt(pos).settings.Costs[typ]
	if !ok {
		return true
	}
	if c.Charge == 0 {
		return 0 <= c.EnergyBudget
	}
	change := 0.0
	for _, p := range st.potential {
		dx, dy, dz := float64(p.pos[0]-pos[0]), float64(p.pos[1]-pos[1]), float64(p.pos[2]-pos[2])
		d := dx*dx + dy*dy + dz*dz
		if d == 0 {
			change = math.Inf(1)
			break
		}
		change += p.charge / math.Sqrt(d)
	}
	return change*c.Charge <= c.EnergyBudget
}

// isRightDistanceToPlayerAndSpawnPoint is NaturalSpawner.isRightDistanceToPlayerAndSpawnPoint.
func (e *env) isRightDistanceToPlayerAndSpawnPoint(c world.ChunkPos, pos cube.Pos, d2 float64) bool {
	if d2 <= 576 {
		return false
	}
	if e.dimension == world.Overworld {
		// The respawn point: closer than 24 blocks to its block centre.
		dx := float64(pos[0]) + .5 - (float64(e.spawn[0]) + .5)
		dy := float64(pos[1]) - (float64(e.spawn[1]) + .5)
		dz := float64(pos[2]) + .5 - (float64(e.spawn[2]) + .5)
		if dx*dx+dy*dy+dz*dz < 576 {
			return false
		}
	}
	pc := chunkOf(pos)
	return pc == c || chunkLoaded(e.tx, pc)
}

// getRandomSpawnMobAt is NaturalSpawner.getRandomSpawnMobAt.
func (e *env) getRandomSpawnMobAt(cat Category, pos cube.Pos) (Spawner, bool) {
	if cat == CategoryWaterAmbient && e.biomeAt(pos).tags["reduce_water_ambient_spawns"] && e.rnd.Float32() < .98 {
		return Spawner{}, false
	}
	return weightedPick(e.mobsAt(cat, pos), e.rnd)
}

// isValidSpawnPositionForType is NaturalSpawner.isValidSpawnPostitionForType.
func (e *env) isValidSpawnPositionForType(cat Category, data *Spawner, pos cube.Pos, d2 float64) bool {
	t, ok := entityTypes[data.Type]
	if !ok || t.Category == CategoryMisc || !t.CanSummon {
		return false
	}
	if dd := float64(t.Category.DespawnDistance()); !t.CanSpawnFarFromPlayer && d2 > dd*dd {
		return false
	}
	if !slices.Contains(e.mobsAt(cat, pos), *data) { // canSpawnMobAt
		return false
	}
	if !e.isSpawnPositionOk(t, pos) || !e.checkSpawnRules(t, pos) {
		return false
	}
	box := spawnAABB(t, float64(pos[0])+.5, float64(pos[1]), float64(pos[2])+.5)
	return e.noBlockCollision(box.Grow(-1e-7))
}

// isValidPositionForMob is NaturalSpawner.isValidPositionForMob for the
// fresh mob at (xx, y, zz): far mobs must despawn-proof, then
// Mob.checkSpawnRules and Mob.checkSpawnObstruction.
func (e *env) isValidPositionForMob(t *EntityType, pos cube.Pos, xx, zz, d2 float64) bool {
	if dd := float64(t.Category.DespawnDistance()); d2 > dd*dd && defaultRemoveWhenFarAway(t.ID, true) {
		return false
	}
	box := spawnAABB(t, xx, float64(pos[1]), zz)
	return e.mobCheckSpawnRules(t, pos) && e.checkSpawnObstruction(t, pos, box.Grow(-1e-7))
}
