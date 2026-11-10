//go:build crossfly_full

package javaspawn

import (
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/java/javamap"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// blockInfo is the Java block of a Dragonfly block state, with what spawning
// reads of its BlockBehaviour.Properties (Blocks.java) precomputed.
type blockInfo struct {
	name  string // Java block name without namespace
	props map[string]string
	// validSpawn is the isValidSpawn override: validDefault uses the
	// default predicate (isFaceSturdy(UP) && getLightEmission() < 14).
	validSpawn validSpawnRule
	// conductor is the isRedstoneConductor override (conductorDefault:
	// isCollisionShapeFullBlock).
	conductor     conductorRule
	signal        bool // isSignalSource
	preventInside bool // #prevent_mob_spawning_inside
	burning       bool // NodeEvaluator.isBurningBlock
	alwaysHurts   bool // wither rose, sweet berry bush, cactus, powder snow (EntityType.isBlockDangerous)
	leaves        bool
	tags          map[string]bool
}

type validSpawnRule uint8

const (
	validDefault validSpawnRule = iota
	validNever
	validAlways
	validOcelotOrParrot
	validPolarBear
	validFireImmune
)

type conductorRule uint8

const (
	conductorDefault conductorRule = iota
	conductorNever
	conductorAlways
)

var (
	blockInfoOnce sync.Once
	blockInfos    []*blockInfo // by Bedrock runtime id
	unknownBlock  = &blockInfo{}
)

// tagSets are blockTags as sets.
var tagSets = sync.OnceValue(func() map[string]map[string]bool {
	m := map[string]map[string]bool{}
	for t, names := range blockTags {
		s := map[string]bool{}
		for _, n := range names {
			s[n] = true
		}
		m[t] = s
	}
	return m
})

// Blocks.java isValidSpawn overrides (26.3).
var (
	validNeverBlocks = map[string]bool{"bedrock": true, "glass": true, "tinted_glass": true, "moving_piston": true,
		"barrier": true, "chorus_flower": true, "scaffolding": true, "iron_trapdoor": true}
	validAlwaysBlocks = map[string]bool{"soul_sand": true, "carved_pumpkin": true, "jack_o_lantern": true,
		"redstone_lamp": true, "mud": true}
	// isRedstoneConductor(Blocks::never) besides leaves, glass, copper
	// grates and bulbs.
	conductorNeverBlocks = map[string]bool{"glass": true, "tinted_glass": true, "moving_piston": true, "tnt": true,
		"ice": true, "frosted_ice": true, "glowstone": true, "beacon": true, "redstone_block": true,
		"sea_lantern": true, "chorus_flower": true, "observer": true, "bamboo": true, "scaffolding": true,
		"powder_snow": true, "pointed_dripstone": true, "sulfur_spike": true, "piston": true, "sticky_piston": true}
	// The blocks whose class overrides isSignalSource (26.3): Lever,
	// Button, DaylightDetector, DetectorRail, Diode (repeater,
	// comparator), Jukebox, Lectern, LightningRod, Observer, Powered
	// (redstone block), RedstoneTorch, RedStoneWire, SculkSensor, Target,
	// TrappedChest, TripWireHook and the pressure plates.
	signalBlocks = map[string]bool{"lever": true, "daylight_detector": true, "detector_rail": true, "repeater": true,
		"comparator": true, "jukebox": true, "lectern": true, "observer": true, "redstone_block": true,
		"redstone_torch": true, "redstone_wall_torch": true, "redstone_wire": true, "sculk_sensor": true,
		"calibrated_sculk_sensor": true, "target": true, "trapped_chest": true, "tripwire_hook": true}
)

func newBlockInfo(name string, props map[string]string) *blockInfo {
	tags := tagSets()
	in := &blockInfo{name: name, props: props, tags: map[string]bool{}}
	for t, s := range tags {
		if s[name] {
			in.tags[t] = true
		}
	}
	in.leaves = in.tags["leaves"]
	in.preventInside = in.tags["prevent_mob_spawning_inside"]
	switch {
	case validNeverBlocks[name], strings.HasSuffix(name, "_stained_glass"), strings.HasSuffix(name, "_trapdoor"),
		strings.HasSuffix(name, "copper_grate"):
		in.validSpawn = validNever
	case validAlwaysBlocks[name]:
		in.validSpawn = validAlways
	case in.leaves:
		in.validSpawn = validOcelotOrParrot
	case name == "ice" || name == "frosted_ice":
		in.validSpawn = validPolarBear
	case name == "magma_block":
		in.validSpawn = validFireImmune
	}
	switch {
	case conductorNeverBlocks[name], in.leaves, strings.HasSuffix(name, "_stained_glass"),
		strings.HasSuffix(name, "copper_grate"), strings.HasSuffix(name, "copper_bulb"):
		in.conductor = conductorNever
	case name == "soul_sand" || name == "mud":
		in.conductor = conductorAlways
	}
	in.signal = signalBlocks[name] || strings.HasSuffix(name, "_button") || strings.HasSuffix(name, "_pressure_plate") ||
		strings.HasSuffix(name, "lightning_rod")
	// NodeEvaluator.isBurningBlock: #fire, lava, magma block, lit
	// campfires, lava cauldron.
	in.burning = in.tags["fire"] || name == "lava" || name == "magma_block" || name == "lava_cauldron" ||
		((name == "campfire" || name == "soul_campfire") && props["lit"] == "true")
	in.alwaysHurts = name == "wither_rose" || name == "sweet_berry_bush" || name == "cactus" || name == "powder_snow"
	return in
}

// infoOf returns the Java block info of a Dragonfly block.
func infoOf(b world.Block) *blockInfo {
	blockInfoOnce.Do(func() {
		states := javamap.DefaultBlockStates()
		blockInfos = make([]*blockInfo, len(states))
		byJava := map[int32]*blockInfo{}
		for rid, java := range states {
			if in, ok := byJava[java]; ok {
				blockInfos[rid] = in
				continue
			}
			name, props, ok := javamap.JavaStateProperties(java)
			if !ok {
				blockInfos[rid] = unknownBlock
				continue
			}
			in := newBlockInfo(strings.TrimPrefix(name, "minecraft:"), props)
			byJava[java], blockInfos[rid] = in, in
		}
	})
	rid := world.BlockRuntimeID(b)
	if int(rid) < len(blockInfos) {
		return blockInfos[rid]
	}
	return unknownBlock
}

// javaName returns the Java block name of b ("grass_block").
func javaName(b world.Block) string { return infoOf(b).name }

// hasTag reports whether the block at pos is in a 26.3 block tag.
func (e *env) hasTag(pos cube.Pos, tag string) bool {
	return infoOf(e.tx.Block(pos)).tags[tag]
}

// isCollisionShapeFullBlock: the collision shape is exactly the unit cube.
func isCollisionShapeFullBlock(tx *world.Tx, pos cube.Pos, b world.Block) bool {
	if _, ok := b.(world.Liquid); ok {
		return false
	}
	boxes := b.Model().BBox(pos, tx)
	if len(boxes) != 1 {
		return false
	}
	bb := boxes[0]
	return bb.Min() == (mgl64.Vec3{}) && bb.Max() == (mgl64.Vec3{1, 1, 1})
}

// isRedstoneConductor is BlockState.isRedstoneConductor.
func isRedstoneConductor(tx *world.Tx, pos cube.Pos, b world.Block) bool {
	switch infoOf(b).conductor {
	case conductorNever:
		return false
	case conductorAlways:
		return true
	}
	return isCollisionShapeFullBlock(tx, pos, b)
}

// isValidSpawn is BlockState.isValidSpawn(level, pos, type) of the block a
// mob stands on.
func isValidSpawn(tx *world.Tx, pos cube.Pos, t *EntityType) bool {
	b := tx.Block(pos)
	switch infoOf(b).validSpawn {
	case validNever:
		return false
	case validAlways:
		return true
	case validOcelotOrParrot:
		return t.ID == "minecraft:ocelot" || t.ID == "minecraft:parrot"
	case validPolarBear:
		return t.ID == "minecraft:polar_bear"
	case validFireImmune:
		return t.FireImmune
	}
	// The default: isFaceSturdy(UP) && getLightEmission() < 14.
	if _, ok := b.(world.Liquid); ok {
		return false
	}
	if l, ok := b.(block.LightEmitter); ok && l.LightEmissionLevel() >= 14 {
		return false
	}
	return b.Model().FaceSolid(pos, cube.FaceUp, tx)
}

// fluidAt returns the fluid at pos: the block's own or a waterlogged one
// (FluidState).
func fluidAt(tx *world.Tx, pos cube.Pos) (water, lava bool) {
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

// isWaterBlock is BlockState.is(Blocks.WATER): a water block, not a
// waterlogged one.
func isWaterBlock(tx *world.Tx, pos cube.Pos) bool {
	_, ok := tx.Block(pos).(block.Water)
	return ok
}

func isAir(tx *world.Tx, pos cube.Pos) bool {
	_, ok := tx.Block(pos).(block.Air)
	return ok
}

// isBlockDangerous is EntityType.isBlockDangerous.
func isBlockDangerous(in *blockInfo, t *EntityType) bool {
	if in.tags[t.ImmuneTo] {
		return false
	}
	if !t.FireImmune && in.burning {
		return true
	}
	return in.alwaysHurts
}

// isValidEmptySpawnBlock is NaturalSpawner.isValidEmptySpawnBlock.
func isValidEmptySpawnBlock(tx *world.Tx, pos cube.Pos, t *EntityType) bool {
	b := tx.Block(pos)
	in := infoOf(b)
	if isCollisionShapeFullBlock(tx, pos, b) || in.signal || in.preventInside || isBlockDangerous(in, t) {
		return false
	}
	w, l := fluidAt(tx, pos)
	return !w && !l
}
