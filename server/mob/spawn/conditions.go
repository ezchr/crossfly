//go:build crossfly_full

package spawn

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// conditions is BDS SpawnConditions: what the spawn position looks like.
type conditions struct {
	surface     bool // byte 0
	inWater     bool // byte 1
	inLava      bool // byte 3
	underground bool // byte 4
	delayEnd    uint64
	brightness  int
	pos         cube.Pos // the block the mob spawns in (floor + 1)
}

// env is the per-attempt view of the world the checks read.
type env struct {
	tx         *world.Tx
	st         *worldState
	cfg        *Config
	rnd        randSource
	difficulty int
	ageSeconds uint64
	skyDarken  int
	thunder    bool
	moon       float32
	rng        cube.Range
}

// canSpawnInConditions is MobSpawnRules::canSpawnInConditions.
func (r *Rule) canSpawnInConditions(c *conditions, e *env) bool {
	y := c.pos[1]
	if y < e.rng[0] || y > e.rng[1] || y < r.MinY || y > r.MaxY {
		return false
	}
	if (c.inWater && !r.Underwater) || (c.inLava && !r.Lava) ||
		(c.surface && !r.Surface) || (c.underground && !r.Underground) {
		return false
	}
	if r.hardcoded != nil && !r.hardcoded(c, e) {
		return false
	}
	if e.difficulty < r.MinDifficulty || e.difficulty > r.MaxDifficulty {
		return false
	}
	br := c.brightness
	if r.AdjustForWeather && e.thunder {
		// rain level * lightning level > 0.9: the brightness with a manual
		// sky darkening of 10.
		br = rawBrightness(e.tx, c.pos, 10)
	}
	if br < r.MinBrightness || br > r.MaxBrightness {
		return false
	}
	if e.moon < r.MinMoon || e.moon > r.MaxMoon {
		return false
	}
	if e.ageSeconds < r.MinWorldAge || e.ageSeconds > r.MaxWorldAge || e.ageSeconds <= c.delayEnd {
		return false
	}
	if r.MobEvent != "" && e.cfg.mobEventDisabled(r.MobEvent) {
		return false
	}
	// minecraft:player_in_village_filter fails when a player is in a village
	// within the distance. Dragonfly has no villages, so it always passes.
	return true
}

// spawnCount is MobSpawnRules::getSpawnCount.
func (r *Rule) spawnCount(c *conditions, e *env, h Herd) int {
	if !r.canSpawnInConditions(c, e) {
		return 0
	}
	if r.Rarity > 0 && e.rnd.IntN(r.Rarity) != 0 {
		return 0
	}
	f := e.rnd.Float32()
	return h.MinSize + int(math.Round(float64(float32(h.MaxSize-h.MinSize)*(f*f))))
}

// isSpawnPositionOk is Spawner::isSpawnPositionOk with the player check:
// pos is the block the mob spawns in.
func (r *Rule) isSpawnPositionOk(pos cube.Pos, e *env) bool {
	maxDist := r.MaxDistance
	sim := e.cfg.SimulationDistance
	if sim <= 4 || e.st.untickedNeighbour(chunkOf(pos)) {
		if sim != 4 {
			return false
		}
		maxDist = min(maxDist, lowEndMaxSpawnDistance)
	}
	p, ok := e.st.nearestPlayer(pos.Vec3(), float64(maxDist))
	if !ok {
		return false
	}
	dx, dy, dz := float64(pos[0])-p[0], float64(pos[1])-p[1], float64(pos[2])-p[2]
	if dx*dx+dy*dy+dz*dz < float64(r.MinDistance*r.MinDistance) {
		return false
	}
	tx := e.tx
	floorPos := pos.Side(cube.FaceDown)
	floor := tx.Block(floorPos)
	name := blockName(floor)
	if len(r.OnBlocks) > 0 && !contains(r.OnBlocks, name) {
		return false
	}
	if len(r.PreventedBlocks) > 0 && contains(r.PreventedBlocks, name) {
		return false
	}
	if len(r.AboveBlocks) > 0 {
		if r.AboveDistance <= 0 {
			return false
		}
		found := false
		for i, y := 0, pos[1]-1; ; i++ {
			below := tx.Block(cube.Pos{pos[0], y, pos[2]})
			if contains(r.AboveBlocks, blockName(below)) {
				found = true
				break
			}
			_, air := below.(block.Air)
			_, water := below.(block.Water)
			if !air && !water || i+1 >= r.AboveDistance {
				break
			}
			y--
		}
		if !found {
			return false
		}
	}
	water, lava := liquidAt(tx, pos)
	if solidBlocking(tx, pos.Side(cube.FaceUp)) || water != r.Underwater || lava != r.Lava {
		return false
	}
	switch floor.(type) {
	case block.Bedrock, block.InvisibleBedrock, block.Barrier:
		return false
	}
	return !solidBlocking(tx, pos)
}

// --- light ---

// skyDarken is Dimension's sky darkening (0 at noon .. 11 at night): the
// time-of-day factor scaled by rain and thunder, as Dimension::tick
// computes it (fog level 0).
func skyDarken(time int, rain, thunder bool) int {
	t := timeOfDay(time)
	v := 1 - min(max(0.5-math.Cos(t*2*math.Pi)*2, 0), 1)
	r, l := 0.0, 0.0
	if rain {
		r = 1
	}
	if thunder {
		l = 1
	}
	v *= (1 - r*5/16) * (1 - l*5/16)
	return int((1 - v) * 11)
}

// timeOfDay is Dimension::getTimeOfDay(1.0): 0 at noon, 0.5 at midnight.
func timeOfDay(time int) float64 {
	t := (float64(((time%24000)+24000)%24000)+1)/24000 - 0.25
	if t < 0 {
		t++
	}
	if t > 1 {
		t--
	}
	return t + (1-(math.Cos(t*math.Pi)+1)/2-t)/3
}

// moonBrightness is Dimension::getMoonBrightness.
func moonBrightness(time int) float32 {
	phases := [8]float32{1, 0.75, 0.5, 0.25, 0, 0.25, 0.5, 0.75}
	day := time / 24000
	if time < 0 {
		day = (time - 23999) / 24000
	}
	return phases[((day%8)+8)%8]
}

// rawBrightness is BlockSource::getRawBrightness: the sky light minus the
// sky darkening, or the block light if higher.
func rawBrightness(tx *world.Tx, pos cube.Pos, darken int) int {
	if pos[1] > tx.Range()[1] {
		return 15 - darken
	}
	if pos[1] < tx.Range()[0] {
		return 0
	}
	sky := int(tx.SkyLight(pos)) - darken
	return max(sky, int(tx.BlockLight(pos)), 0)
}

// monsterSpawnTypes are the types whose BDS class takes
// Monster::checkSpawnRules (Monster::isDarkEnoughToSpawn) or
// Zombie::checkSpawnRules for natural spawns: the classes Zombie (zombie,
// husk, drowned, zombie villager), Skeleton (skeleton, stray, wither
// skeleton), Monster (bogged, zoglin) and the other Monster subclasses
// (VanillaBuiltInEntities::registerMappings, registerVanillaActorData).
// Slime, LavaSlime, Guardian, PigZombie, Shulker and Phantom override
// checkSpawnRules without the darkness check, and animals are Mob.
// Parched is a skeleton variant added after 1.21.3, taken as Skeleton.
var monsterSpawnTypes = map[string]bool{
	"minecraft:zombie": true, "minecraft:husk": true, "minecraft:drowned": true,
	"minecraft:zombie_villager": true, "minecraft:zombie_villager_v2": true,
	"minecraft:skeleton": true, "minecraft:stray": true, "minecraft:wither_skeleton": true,
	"minecraft:bogged": true, "minecraft:parched": true, "minecraft:zoglin": true,
	"minecraft:creeper": true, "minecraft:spider": true, "minecraft:cave_spider": true,
	"minecraft:enderman": true, "minecraft:silverfish": true, "minecraft:endermite": true,
	"minecraft:witch": true, "minecraft:blaze": true, "minecraft:ghast": true,
	"minecraft:vindicator": true, "minecraft:evocation_illager": true, "minecraft:pillager": true,
	"minecraft:ravager": true, "minecraft:vex": true, "minecraft:warden": true,
	"minecraft:breeze": true, "minecraft:piglin": true, "minecraft:piglin_brute": true,
	"minecraft:wither": true, "minecraft:ender_dragon": true,
}

// monsterDarkEnough is Monster::isDarkEnoughToSpawn at the block a mob
// spawns in: in an ultra warm dimension (the Nether) the raw brightness must
// be at most 7; elsewhere the block light must be 0 and the raw brightness,
// with a manual sky darkening of 10 while it thunders, at most 7.
func monsterDarkEnough(e *env, pos cube.Pos) bool {
	tx := e.tx
	return darkEnough(rawBrightness(tx, pos, e.skyDarken), blockLight(tx, pos),
		tx.World().Dimension().WaterEvaporates(), e.thunder, func() int { return rawBrightness(tx, pos, 10) })
}

// darkEnough is the decision of Monster::isDarkEnoughToSpawn: raw is the raw
// brightness, block the block light, thunderRaw the raw brightness with a
// sky darkening of 10.
func darkEnough(raw, block int, ultraWarm, thunder bool, thunderRaw func() int) bool {
	if ultraWarm {
		return raw <= 7
	}
	if block > 0 {
		return false
	}
	if thunder {
		raw = thunderRaw()
	}
	return raw <= 7
}

// blockLight is the block light at pos, 0 outside the world.
func blockLight(tx *world.Tx, pos cube.Pos) int {
	if r := tx.Range(); pos[1] < r[0] || pos[1] > r[1] {
		return 0
	}
	return int(tx.BlockLight(pos))
}

// --- blocks ---

func blockName(b world.Block) string {
	n, _ := b.EncodeBlock()
	return n
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

func liquidAt(tx *world.Tx, pos cube.Pos) (water, lava bool) {
	l, ok := tx.Liquid(pos)
	if !ok {
		return false, false
	}
	switch l.(type) {
	case block.Water:
		return true, false
	case block.Lava:
		return false, true
	}
	return false, false
}

// blocksMotion is Material::getBlocksMotion: the block has a collision box.
func blocksMotion(tx *world.Tx, pos cube.Pos, b world.Block) bool {
	if _, ok := b.(world.Liquid); ok {
		return false
	}
	return len(b.Model().BBox(pos, tx)) > 0
}

// isSolid is Block::isSolid: a block mobs can stand and spawn on. Glass and
// leaves are full blocks in Dragonfly's model but not solid in BDS.
func isSolid(tx *world.Tx, pos cube.Pos, b world.Block) bool {
	switch b.(type) {
	case block.Glass, block.StainedGlass, block.TintedGlass, block.Leaves, world.Liquid:
		return false
	}
	return b.Model().FaceSolid(pos, cube.FaceUp, tx)
}

// solidBlocking is BlockSource::isSolidBlockingBlock: a full solid cube.
func solidBlocking(tx *world.Tx, pos cube.Pos) bool {
	if pos[1] < tx.Range()[0] || pos[1] > tx.Range()[1] {
		return false
	}
	b := tx.Block(pos)
	if !isSolid(tx, pos, b) {
		return false
	}
	boxes := b.Model().BBox(pos, tx)
	if len(boxes) != 1 {
		return false
	}
	bb := boxes[0]
	return bb.Min() == (mgl64.Vec3{}) && bb.Max() == (mgl64.Vec3{1, 1, 1})
}

func isAirOrLiquid(b world.Block) bool {
	switch b.(type) {
	case block.Air, world.Liquid:
		return true
	}
	return false
}

func isCarpet(b world.Block) bool {
	switch b.(type) {
	case block.Carpet, block.MossCarpet:
		return true
	}
	return false
}

func isIce(b world.Block) bool {
	switch b.(type) {
	case block.Ice, block.PackedIce, block.BlueIce:
		return true
	}
	return false
}

// findNextSpawnBlockUnder is Spawner::findNextSpawnBlockUnder: it moves pos
// down to the next block a mob can spawn on (pos becomes that floor block).
// surface (SpawnBlockRequirements 1) fails instead of passing through solid
// blocks, carpets and bottom slabs, so it only finds the first floor under
// the sky.
func findNextSpawnBlockUnder(tx *world.Tx, pos *cube.Pos, surface bool) bool {
	minY := tx.Range()[0]
	for {
		// Phase 1: leave the blocks we are in (down to air or liquid).
		for pos[1] >= minY {
			b := tx.Block(*pos)
			if isAirOrLiquid(b) {
				break
			}
			if surface && isSolid(tx, *pos, b) {
				return false
			}
			pos[1]--
		}
		// Phase 2: fall through air, liquid and anything without collision.
		var b world.Block
		passable := false
		for {
			if pos[1] < minY {
				return false
			}
			b = tx.Block(*pos)
			if isCarpet(b) {
				passable = true
				break
			}
			if blocksMotion(tx, *pos, b) {
				break
			}
			pos[1]--
		}
		if !passable {
			if s, ok := b.(block.Slab); ok && !s.Top && !s.Double {
				passable = true // a bottom slab: no spawning on it
			} else if isIce(b) || isSolid(tx, *pos, b) {
				return true
			}
		}
		if passable && surface {
			return false
		}
		if pos[1] < minY {
			return false
		}
		// A non-solid block (leaves, glass, fence...): continue below it.
	}
}

// chunkOf is the chunk of a block position.
func chunkOf(pos cube.Pos) world.ChunkPos {
	return world.ChunkPos{int32(pos[0] >> 4), int32(pos[2] >> 4)}
}

// --- slime ---

// slimeSpawnRules is SlimeSpawnRules::checkSpawnRulesCallback: below y 40
// slimes spawn only in slime chunks; between 51 and 69 only in swamps, in
// the dark, more often on bright-moon nights.
func slimeSpawnRules(c *conditions, e *env) bool {
	y := c.pos[1] + 1
	if y < 40 {
		return isSlimeChunk(chunkOf(c.pos))
	}
	if y < 51 || y > 69 || c.brightness >= 8 {
		return false
	}
	if c.brightness > e.rnd.IntN(8) {
		return false
	}
	if e.rnd.Float32() >= e.moon {
		return false
	}
	switch e.tx.Biome(cube.Pos{c.pos[0], y, c.pos[2]}).String() {
	case "swampland", "swampland_mutated", "mangrove_swamp":
		return true
	}
	return false
}

// isSlimeChunk is SlimeSpawnRules::isSlimeChunk: the first MT19937 output
// seeded with z ^ x*0x1f1f1f1f is a multiple of 10.
func isSlimeChunk(c world.ChunkPos) bool {
	seed := uint32(c[1]) ^ (uint32(c[0]) * 522133279)
	return mt19937First(seed)%10 == 0
}

// mt19937First returns the first output of a 32-bit Mersenne Twister seeded
// with seed (init_genrand).
func mt19937First(seed uint32) uint32 {
	const n, m = 624, 397
	var mt [n]uint32
	mt[0] = seed
	for i := 1; i < n; i++ {
		mt[i] = 1812433253*(mt[i-1]^(mt[i-1]>>30)) + uint32(i)
	}
	// Generate only element 0 of the first twist.
	y := (mt[0] & 0x80000000) | (mt[1] & 0x7fffffff)
	v := mt[m] ^ (y >> 1)
	if y&1 != 0 {
		v ^= 0x9908b0df
	}
	v ^= v >> 11
	v ^= (v << 7) & 0x9d2c5680
	v ^= (v << 15) & 0xefc60000
	v ^= v >> 18
	return v
}
