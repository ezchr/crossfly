//go:build crossfly_full

package javaspawn

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// dimensionType is what spawning reads of the 26.3 dimension_type json.
type dimensionType struct {
	ambientLight float32
	hasSkyLight  bool
	// monsterBlockLightLimit is monster_spawn_block_light_limit.
	monsterBlockLightLimit int
	// monsterLightMin/Max is monster_spawn_light_level (uniform or
	// constant).
	monsterLightMin, monsterLightMax int
}

var (
	overworldType = dimensionType{ambientLight: 0, hasSkyLight: true, monsterBlockLightLimit: 0, monsterLightMin: 0, monsterLightMax: 7}
	netherType    = dimensionType{ambientLight: 0.1, hasSkyLight: false, monsterBlockLightLimit: 15, monsterLightMin: 7, monsterLightMax: 7}
	endType       = dimensionType{ambientLight: 0.25, hasSkyLight: true, monsterBlockLightLimit: 0, monsterLightMin: 15, monsterLightMax: 15}
)

func dimensionTypeOf(d world.Dimension) *dimensionType {
	switch d {
	case world.Nether:
		return &netherType
	case world.End:
		return &endType
	}
	return &overworldType
}

// skyLightLevel is the gameplay/sky_light_level environment attribute
// (EnvironmentAttributes.SKY_LIGHT_LEVEL): the overworld follows
// timeline/day.json and the rain and thunder modifiers (as the daylight
// detector in package block); the nether fixes it at 4
// (dimension_type/the_nether.json); the end has the default 15.
func skyLightLevel(d world.Dimension, timeOfDay int, rain, thunder bool) float32 {
	switch d {
	case world.Nether:
		return 4
	case world.End:
		return 15
	}
	timeOfDay = ((timeOfDay % 24000) + 24000) % 24000
	t := float32(timeOfDay)
	level := float32(1)
	const night = float32(.26666668)
	switch {
	case timeOfDay >= 11867 && timeOfDay < 13670:
		level = 1 + (night-1)*(t-11867)/(13670-11867)
	case timeOfDay >= 13670 && timeOfDay < 22330:
		level = night
	case timeOfDay >= 22330 || timeOfDay < 133:
		if timeOfDay < 133 {
			t += 24000
		}
		level = night + (1-night)*(t-22330)/(24133-22330)
	}
	level *= 15
	var r, th float32
	if rain {
		r = 1
		if thunder {
			th = 1
		}
	}
	rainOnly := max(0, r-th)
	rainValue := level + (4-level)*float32(.3125)
	level += (rainValue - level) * rainOnly
	thunderValue := level + (4-level)*float32(.52734375)
	level += (thunderValue - level) * th
	return level
}

// skyDarken is Level.updateSkyBrightness: 15 - SKY_LIGHT_LEVEL.
func skyDarken(d world.Dimension, timeOfDay int, rain, thunder bool) int {
	return int(15 - skyLightLevel(d, timeOfDay, rain, thunder))
}

// skyLight is getBrightness(LightLayer.SKY, pos).
func (e *env) skyLight(pos cube.Pos) int {
	if !e.dim.hasSkyLight {
		return 0
	}
	if pos[1] > e.rng[1] {
		return 15
	}
	if pos[1] < e.rng[0] {
		return 0
	}
	return int(e.tx.SkyLight(pos))
}

// blockLight is getBrightness(LightLayer.BLOCK, pos).
func (e *env) blockLight(pos cube.Pos) int {
	if pos[1] > e.rng[1] || pos[1] < e.rng[0] {
		return 0
	}
	return int(e.tx.BlockLight(pos))
}

// rawBrightness is getRawBrightness(pos, darkening): max(sky - darkening,
// block).
func (e *env) rawBrightness(pos cube.Pos, darkening int) int {
	return max(e.skyLight(pos)-darkening, e.blockLight(pos), 0)
}

// maxLocalRawBrightness is getMaxLocalRawBrightness(pos): the raw
// brightness with the level's sky darkening.
func (e *env) maxLocalRawBrightness(pos cube.Pos) int { return e.rawBrightness(pos, e.skyDarken) }

// canSeeSky is BlockAndLightGetter.canSeeSky: full sky light.
func (e *env) canSeeSky(pos cube.Pos) bool { return e.skyLight(pos) >= 15 }

// canSeeSkyFromBelowWater is LevelReader.canSeeSkyFromBelowWater.
func (e *env) canSeeSkyFromBelowWater(pos cube.Pos) bool {
	if pos[1] >= e.seaLevel {
		return e.canSeeSky(pos)
	}
	check := cube.Pos{pos[0], e.seaLevel, pos[2]}
	if !e.canSeeSky(check) {
		return false
	}
	for y := check[1] - 1; y > pos[1]; y-- {
		p := cube.Pos{pos[0], y, pos[2]}
		b := e.tx.Block(p)
		if _, liquid := b.(world.Liquid); liquid {
			continue
		}
		if lightBlock(e.tx, p, b) > 0 {
			return false
		}
	}
	return true
}

// lightBlock is BlockState.getLightBlock (> 0: the block dims light
// passing through it).
func lightBlock(tx *world.Tx, pos cube.Pos, b world.Block) int {
	if d, ok := b.(block.LightDiffuser); ok {
		return int(d.LightDiffusionLevel())
	}
	if isCollisionShapeFullBlock(tx, pos, b) {
		return 15
	}
	return 0
}

// pathfindingCostFromLightLevels is LevelReader.getPathfindingCostFromLightLevels:
// getLightLevelDependentMagicValue(pos) - 0.5.
func (e *env) pathfindingCostFromLightLevels(pos cube.Pos) float32 {
	v := float32(e.maxLocalRawBrightness(pos)) / 15
	curved := v / (4 - 3*v)
	return curved + e.dim.ambientLight*(1-curved) - 0.5
}

// isDarkEnoughToSpawn is Monster.isDarkEnoughToSpawn (Paper keeps the
// dimension's block light limit with monster-spawn-max-light-level at its
// default).
func (e *env) isDarkEnoughToSpawn(pos cube.Pos) bool {
	brightness := func() int {
		if e.thundering {
			return e.rawBrightness(pos, 10)
		}
		return e.maxLocalRawBrightness(pos)
	}
	return darkEnoughToSpawn(e.dim, e.skyLight(pos), e.blockLight(pos), brightness, e.rnd)
}

// darkEnoughToSpawn is the body of Monster.isDarkEnoughToSpawn: sky light
// above rand(32) fails, block light above the dimension's limit fails, and
// the raw brightness (thunder darkens the sky by 10) must not exceed the
// dimension's monster_spawn_light_level sample.
func darkEnoughToSpawn(d *dimensionType, sky, blockLight int, brightness func() int, rnd randSource) bool {
	if sky > rnd.IntN(32) {
		return false
	}
	if lim := d.monsterBlockLightLimit; lim < 15 && blockLight > lim {
		return false
	}
	b := brightness()
	test := d.monsterLightMin
	if d.monsterLightMax > d.monsterLightMin {
		test += rnd.IntN(d.monsterLightMax - d.monsterLightMin + 1)
	}
	return b <= test
}
