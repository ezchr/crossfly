//go:build crossfly_full

package path

import (
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/world"
)

// Type is a PathType: what a block position is to a walking mob.
type Type uint8

// The path types, in PathType order.
const (
	Blocked Type = iota
	Open
	Walkable
	WalkableDoor
	Trapdoor
	PowderSnow
	OnTopOfPowderSnow
	Fence
	Lava
	Water
	WaterBorder
	Rail
	UnpassableRail
	FireInNeighbour
	Fire
	DamagingInNeighbour
	Damaging
	DoorOpen
	DoorWoodClosed
	DoorIronClosed
	Breach
	Leaves
	StickyHoney
	Cocoa
	DamageCautious
	OnTopOfTrapdoor
	BigMobsCloseToDanger
	// TypeCount is the number of path types.
	TypeCount
)

var typeNames = [...]string{"BLOCKED", "OPEN", "WALKABLE", "WALKABLE_DOOR", "TRAPDOOR", "POWDER_SNOW",
	"ON_TOP_OF_POWDER_SNOW", "FENCE", "LAVA", "WATER", "WATER_BORDER", "RAIL", "UNPASSABLE_RAIL", "FIRE_IN_NEIGHBOR",
	"FIRE", "DAMAGING_IN_NEIGHBOR", "DAMAGING", "DOOR_OPEN", "DOOR_WOOD_CLOSED", "DOOR_IRON_CLOSED", "BREACH", "LEAVES",
	"STICKY_HONEY", "COCOA", "DAMAGE_CAUTIOUS", "ON_TOP_OF_TRAPDOOR", "BIG_MOBS_CLOSE_TO_DANGER"}

// String ...
func (t Type) String() string {
	if t < TypeCount {
		return typeNames[t]
	}
	return "INVALID"
}

var defaultMalus = [TypeCount]float32{
	Blocked: -1, Open: 0, Walkable: 0, WalkableDoor: 0, Trapdoor: 0, PowderSnow: -1, OnTopOfPowderSnow: 0, Fence: -1,
	Lava: -1, Water: 8, WaterBorder: 8, Rail: 0, UnpassableRail: -1, FireInNeighbour: 8, Fire: 16,
	DamagingInNeighbour: 8, Damaging: -1, DoorOpen: 0, DoorWoodClosed: -1, DoorIronClosed: -1, Breach: 4, Leaves: -1,
	StickyHoney: 8, Cocoa: 0, DamageCautious: 0, OnTopOfTrapdoor: 0, BigMobsCloseToDanger: 4,
}

// Malus is the default malus of the type (PathType.getMalus): below 0 the mob never walks there.
func (t Type) Malus() float32 { return defaultMalus[t] }

// blockKind is what getPathTypeFromState decides from a block state alone.
type blockKind uint8

const (
	kindAir blockKind = iota
	kindTrapdoor
	kindPowderSnow
	kindDamaging
	kindHoney
	kindCocoa
	kindCautious
	kindLava
	kindFire
	kindDoorOpen
	kindDoorWood
	kindDoorIron
	kindRail
	kindLeaves
	kindFence
	kindBlocked
	// kindOpen is a pathfindable block: water if water is in it (waterlogged), open otherwise.
	kindOpen
	// kindWater is a water block.
	kindWater
)

// blockInfo is what the pathfinder needs of one block state, worked out once per runtime ID.
type blockInfo struct {
	kind blockKind
	// cauldron: BlockTags.CAULDRONS (PathNavigation.trimPath).
	cauldron bool
	// solidish: BlockStateBase.isSolid (the legacy solid flag GroundPathNavigation.findSurfacePosition reads).
	solid bool
	// solidRender: BlockStateBase.isSolidRender (PathNavigation.isStableDestination).
	solidRender bool
	// fenceLike: BlockTags.FENCES, BlockTags.WALLS or a fence gate (Entity.getOnPos), doorOrFence: DOORS or
	// FENCES (MoveControl's jump check).
	fenceLike, doorOrFence bool
}

var (
	infoOnce sync.Once
	infos    []blockInfo
)

// infoOf returns the static info of the block with the runtime ID.
func infoOf(rid uint32) blockInfo {
	infoOnce.Do(buildInfos)
	if int(rid) < len(infos) {
		return infos[rid]
	}
	b, ok := world.BlockByRuntimeID(rid)
	if !ok {
		return blockInfo{kind: kindBlocked}
	}
	return classify(b)
}

// InfoOf is infoOf for a block.
func blockInfoOf(b world.Block) blockInfo { return infoOf(world.BlockRuntimeID(b)) }

func buildInfos() {
	n := len(world.Blocks())
	infos = make([]blockInfo, n)
	for rid := range infos {
		if b, ok := world.BlockByRuntimeID(uint32(rid)); ok {
			infos[rid] = classify(b)
		} else {
			infos[rid] = blockInfo{kind: kindBlocked}
		}
	}
}

// notPathfindable are the blocks whose isPathfindable(LAND) is false although their collision box is not a full
// block (the isPathfindable overrides of 26.3), by Bedrock name without the namespace.
var notPathfindable = map[string]bool{
	"chorus_plant": true, "pointed_dripstone": true, "cauldron": true, "lava_cauldron": true, "mud": true,
	"respawn_anchor": true, "azalea": true, "flowering_azalea": true, "chain": true, "iron_chain": true,
	"brewing_stand": true, "bamboo": true, "soul_sand": true, "enchanting_table": true, "cocoa": true,
	"chest": true, "trapped_chest": true, "ender_chest": true, "heavy_core": true, "sniffer_egg": true,
	"iron_bars": true, "lantern": true, "soul_lantern": true, "decorated_pot": true, "end_portal_frame": true,
	"piston": true, "sticky_piston": true, "piston_arm_collision": true, "sticky_piston_arm_collision": true,
	"moving_block": true, "sea_pickle": true, "grass_path": true, "dirt_path": true, "cake": true,
	"flower_pot": true, "conduit": true, "hopper": true, "cactus": true, "sculk_sensor": true,
	"calibrated_sculk_sensor": true, "campfire": true, "soul_campfire": true, "dragon_egg": true, "composter": true,
	"dried_ghast": true, "farmland": true, "lectern": true, "bell": true, "stonecutter_block": true, "end_rod": true,
	"lightning_rod": true, "bed": true, "anvil": true, "chipped_anvil": true, "damaged_anvil": true,
	"grindstone": true, "copper_golem_statue": true, "red_mushroom_block": false,
}

func classify(b world.Block) blockInfo {
	name, props := b.EncodeBlock()
	name = strings.TrimPrefix(name, "minecraft:")
	info := blockInfo{}
	mdl := safeModel(b)
	_, isFence := mdl.(model.Fence)
	_, isWall := mdl.(model.Wall)
	_, isGate := mdl.(model.FenceGate)
	_, isDoor := mdl.(model.Door)
	info.fenceLike = isFence || isWall || isGate
	info.doorOrFence = isFence || isDoor
	info.cauldron = name == "cauldron" || name == "lava_cauldron"
	full := isFullModel(mdl)
	info.solidRender = full && !strings.Contains(name, "leaves") && !strings.Contains(name, "glass") && name != "barrier"
	info.solid = full || isWall || isFence || strings.HasSuffix(name, "_slab") || strings.HasSuffix(name, "_stairs")
	open := func() bool { v, _ := props["open_bit"].(bool); return v }
	switch {
	case name == "air" || strings.HasSuffix(name, "_air"):
		info.kind = kindAir
		info.solid = false
	case strings.HasSuffix(name, "trapdoor") || name == "waterlily" || name == "big_dripleaf":
		info.kind = kindTrapdoor
	case name == "powder_snow":
		info.kind = kindPowderSnow
	case name == "cactus" || name == "sweet_berry_bush":
		info.kind = kindDamaging
	case name == "honey_block":
		info.kind = kindHoney
	case name == "cocoa":
		info.kind = kindCocoa
	case name == "wither_rose" || name == "pointed_dripstone":
		info.kind = kindCautious
	case name == "lava" || name == "flowing_lava":
		info.kind = kindLava
	case burning(b, name, props):
		info.kind = kindFire
	case isDoor:
		switch {
		case open():
			info.kind = kindDoorOpen
		case name == "iron_door":
			info.kind = kindDoorIron
		default:
			info.kind = kindDoorWood
		}
	case name == "rail" || strings.HasSuffix(name, "_rail"):
		info.kind = kindRail
	case strings.Contains(name, "leaves"):
		info.kind = kindLeaves
	case isFence || isWall || isGate && !open():
		info.kind = kindFence
	case name == "water" || name == "flowing_water":
		info.kind = kindWater
	case !pathfindable(b, name, props, full, isGate, open):
		info.kind = kindBlocked
	default:
		info.kind = kindOpen
	}
	return info
}

// burning is NodeEvaluator.isBurningBlock without lava (checked before): fire, magma, a lit campfire, a lava
// cauldron.
func burning(b world.Block, name string, props map[string]any) bool {
	switch name {
	case "fire", "soul_fire", "magma", "lava_cauldron":
		return true
	case "campfire", "soul_campfire":
		ext, _ := props["extinguished"].(bool)
		return !ext
	case "cauldron":
		l, _ := props["cauldron_liquid"].(string)
		fill, _ := props["fill_level"].(int32)
		return l == "lava" && fill > 0
	}
	return false
}

// pathfindable is BlockBehaviour.isPathfindable(LAND) and its overrides.
func pathfindable(b world.Block, name string, props map[string]any, full, gate bool, open func() bool) bool {
	switch {
	case notPathfindable[name], strings.HasSuffix(name, "_slab"), strings.HasSuffix(name, "_stairs"),
		strings.HasSuffix(name, "_pane"), strings.HasSuffix(name, "_wall"), strings.HasSuffix(name, "_skull"),
		strings.HasSuffix(name, "_head") && name != "piston_head", strings.HasSuffix(name, "candle_cake"),
		strings.HasSuffix(name, "_bed"), strings.HasSuffix(name, "_shelf"), strings.HasSuffix(name, "_wall_hanging_sign"),
		strings.HasSuffix(name, "_chain"), strings.HasSuffix(name, "lightning_rod"), strings.HasSuffix(name, "_lantern"):
		return false
	case gate:
		return open()
	case name == "snow_layer":
		h, _ := props["height"].(int32)
		return h < 4
	case name == "powder_snow":
		return true
	}
	return !full
}

// isFullModel reports whether the block's collision shape is a full block (isCollisionShapeFullBlock).
func isFullModel(mdl world.BlockModel) bool {
	switch m := mdl.(type) {
	case model.Solid, model.Leaves:
		return true
	case model.Slab:
		return m.Double
	}
	return false
}

// collisionTop is the top of the block's collision boxes at pos above pos's y (VoxelShape.max(Y)), and whether it
// has any.
func collisionTop(tx *world.Tx, pos cube.Pos) (float64, bool) {
	boxes := tx.Block(pos).Model().BBox(pos, tx)
	if len(boxes) == 0 {
		return 0, false
	}
	top := boxes[0].Max()[1]
	for _, bb := range boxes[1:] {
		top = max(top, bb.Max()[1])
	}
	return top, true
}

// FloorLevel is WalkNodeEvaluator.getFloorLevel(level, pos): the top of the collision box of the block under pos.
func FloorLevel(tx *world.Tx, pos cube.Pos) float64 {
	below := pos.Side(cube.FaceDown)
	top, _ := collisionTop(tx, below)
	return float64(below[1]) + top
}

// IsDoorOrFence reports whether the block is a door or a fence (MoveControl: BlockTags.DOORS, BlockTags.FENCES).
func IsDoorOrFence(b world.Block) bool { return blockInfoOf(b).doorOrFence }

// IsSolid is BlockStateBase.isSolid of the block at pos (the legacy solid flag).
func IsSolid(tx *world.Tx, pos cube.Pos) bool {
	return !pos.OutOfBounds(tx.Range()) && infoOf(tx.BlockRuntimeIDAt(pos)).solid
}

// IsFence reports whether the block is a fence (BlockTags.FENCES).
func IsFence(b world.Block) bool { _, ok := b.Model().(model.Fence); return ok }

// IsWallOrGate reports whether the block is a wall or a fence gate (BlockTags.WALLS, FenceGateBlock).
func IsWallOrGate(b world.Block) bool {
	switch b.Model().(type) {
	case model.Wall, model.FenceGate:
		return true
	}
	return false
}

// IsSuffocating is BlockStateBase.isSuffocating: a full solid block that is not see-through.
func IsSuffocating(b world.Block) bool { return blockInfoOf(b).solidRender }

// safeModel is b.Model(), or nil for a registry block whose model needs state it does not have (a zero shulker
// box).
func safeModel(b world.Block) (m world.BlockModel) {
	defer func() {
		if recover() != nil {
			m = nil
		}
	}()
	return b.Model()
}
