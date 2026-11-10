//go:build crossfly_full

package jvillage

import (
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// The block shape tests golem summoning needs. They follow Java's
// BlockStateBase on Dragonfly's collision models.

func boxesOf(tx *world.Tx, pos cube.Pos, b world.Block) []cube.BBox {
	return b.Model().BBox(pos, tx)
}

func blockName(b world.Block) string {
	n, _ := b.EncodeBlock()
	return strings.TrimPrefix(n, "minecraft:")
}

// bounds is VoxelShape.bounds of a block's collision shape.
func bounds(boxes []cube.BBox) (cube.BBox, bool) {
	if len(boxes) == 0 {
		return cube.BBox{}, false
	}
	b := boxes[0]
	for _, o := range boxes[1:] {
		mn, mx := b.Min(), b.Max()
		om, ox := o.Min(), o.Max()
		b = cube.Box(min(mn[0], om[0]), min(mn[1], om[1]), min(mn[2], om[2]), max(mx[0], ox[0]), max(mx[1], ox[1]), max(mx[2], ox[2]))
	}
	return b, true
}

// isLegacySolid is BlockStateBase.isSolid (calculateSolid): the collision
// bounds average at least 0.7291666 on the three axes or are a block high.
func isLegacySolid(tx *world.Tx, pos cube.Pos, b world.Block) bool {
	bb, ok := bounds(boxesOf(tx, pos, b))
	if !ok {
		return false
	}
	w, h, d := bb.Width(), bb.Height(), bb.Length()
	return (w+h+d)/3 >= 0.7291666666666666 || h >= 1
}

// fullFace reports whether the boxes cover the top face of the block.
func topFaceFull(boxes []cube.BBox) bool {
	for _, b := range boxes {
		mn, mx := b.Min(), b.Max()
		if mx[1] >= 1 && mn[0] <= 0 && mn[2] <= 0 && mx[0] >= 1 && mx[2] >= 1 {
			return true
		}
	}
	return false
}

// entityCanStandOn is BlockStateBase.entityCanStandOn: the collision shape
// has a full top face.
func entityCanStandOn(tx *world.Tx, pos cube.Pos) bool {
	return topFaceFull(boxesOf(tx, pos, tx.Block(pos)))
}

// collisionFullBlock is BlockStateBase.isCollisionShapeFullBlock.
func collisionFullBlock(tx *world.Tx, pos cube.Pos, b world.Block) bool {
	for _, bb := range boxesOf(tx, pos, b) {
		mn, mx := bb.Min(), bb.Max()
		if mn[0] <= 0 && mn[1] <= 0 && mn[2] <= 0 && mx[0] >= 1 && mx[1] >= 1 && mx[2] >= 1 {
			return true
		}
	}
	return false
}

// signalSource is BlockStateBase.isSignalSource by block name.
func signalSource(name string) bool {
	switch {
	case name == "lever", name == "redstone_block", name == "redstone_torch", name == "unlit_redstone_torch",
		name == "redstone_wire", name == "observer", name == "daylight_detector", name == "daylight_detector_inverted",
		name == "tripwire_hook", name == "target", name == "trapped_chest", name == "detector_rail",
		name == "powered_repeater", name == "unpowered_repeater", name == "powered_comparator", name == "unpowered_comparator",
		name == "sculk_sensor", name == "calibrated_sculk_sensor", name == "lightning_rod":
		return true
	}
	return strings.HasSuffix(name, "_button") || strings.HasSuffix(name, "pressure_plate")
}

// dangerousForGolem is EntityType.isBlockDangerous for the iron golem.
func dangerousForGolem(name string) bool {
	switch name {
	case "wither_rose", "sweet_berry_bush", "cactus", "powder_snow", "fire", "soul_fire", "lava", "flowing_lava", "magma", "campfire", "soul_campfire":
		return true
	}
	return false
}

// validEmptySpawnBlock is NaturalSpawner.isValidEmptySpawnBlock for an iron
// golem; withFluid=false is Java passing Fluids.EMPTY (the spawn block
// itself may hold water).
func validEmptySpawnBlock(tx *world.Tx, pos cube.Pos, withFluid bool) bool {
	b := tx.Block(pos)
	if _, liquid := b.(world.Liquid); liquid {
		return !withFluid
	}
	name := blockName(b)
	if collisionFullBlock(tx, pos, b) || signalSource(name) || strings.HasSuffix(name, "rail") || name == "rail" || dangerousForGolem(name) {
		return false
	}
	if withFluid {
		if _, ok := tx.Liquid(pos); ok {
			return false
		}
	}
	return true
}

// golemNeverOn lists the blocks LEGACY_IRON_GOLEM refuses to spawn on.
func golemNeverOn(name string) bool {
	switch name {
	case "web", "cobweb", "cactus", "glass_pane", "conduit", "ice", "tnt", "glowstone", "beacon", "sea_lantern",
		"frosted_ice", "tinted_glass", "glass":
		return true
	}
	return strings.HasSuffix(name, "stained_glass") || strings.HasSuffix(name, "stained_glass_pane") || strings.HasSuffix(name, "leaves")
}

// legacyGolemCanSpawnOn is SpawnUtil.Strategy.LEGACY_IRON_GOLEM.
func legacyGolemCanSpawnOn(tx *world.Tx, pos cube.Pos, b, above world.Block) bool {
	name := blockName(b)
	if golemNeverOn(name) {
		return false
	}
	aboveName := blockName(above)
	_, aboveLiquid := above.(world.Liquid)
	if !(aboveName == "air" || aboveLiquid) {
		return false
	}
	return isLegacySolid(tx, pos, b) || name == "powder_snow"
}

// isUnobstructed is LevelReader.isUnobstructed(entity): no entity that
// blocks building (living mobs, players, boats) overlaps box.
func isUnobstructed(tx *world.Tx, box cube.BBox) bool {
	for e := range tx.EntitiesWithin(box) {
		if _, ok := e.(interface{ Dead() bool }); ok {
			if e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) {
				return false
			}
		}
	}
	return true
}
