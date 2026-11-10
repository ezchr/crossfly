//go:build crossfly_full

package jnether

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// The repellents piglins and hoglins keep away from (PiglinSpecificSensor
// and HoglinSpecificSensor findNearestRepellent): the first matching block
// of Level.findBlocksInBoxByManhattanDistance(pos, 8, 4), the box 8 x 4 x 8
// around the mob walked by increasing Manhattan distance.

// hoglinRepellents is BlockTags.HOGLIN_REPELLENTS (Bedrock names): warped
// fungus (and potted), nether portals, respawn anchors.
var hoglinRepellents = map[string]bool{
	"warped_fungus": true, "portal": true, "respawn_anchor": true,
}

// piglinRepellents is BlockTags.PIGLIN_REPELLENTS: soul fire, soul torches
// and lanterns, lit soul campfires.
var piglinRepellents = map[string]bool{
	"soul_fire": true, "soul_torch": true, "soul_lantern": true, "soul_campfire": true,
}

// NearestHoglinRepellent is HoglinSpecificSensor.findNearestRepellent.
func NearestHoglinRepellent(tx *world.Tx, pos cube.Pos) (cube.Pos, bool) {
	return findInManhattanBox(tx, pos, 8, 4, func(b world.Block) bool { return hoglinRepellents[blockID(b)] })
}

// NearestPiglinRepellent is PiglinSpecificSensor.findNearestRepellent (a
// soul campfire only when lit).
func NearestPiglinRepellent(tx *world.Tx, pos cube.Pos) (cube.Pos, bool) {
	return findInManhattanBox(tx, pos, 8, 4, func(b world.Block) bool {
		name, props := b.EncodeBlock()
		short := name
		if len(short) > 10 && short[:10] == "minecraft:" {
			short = short[10:]
		}
		if !piglinRepellents[short] {
			return false
		}
		if short == "soul_campfire" {
			out, _ := props["extinguished"].(bool)
			return !out
		}
		return true
	})
}

// findInManhattanBox is findBlocksInBoxByManhattanDistance(pos, xz, y)
// .filterState(match).findFirst().
func findInManhattanBox(tx *world.Tx, pos cube.Pos, xz, y int, match func(world.Block) bool) (cube.Pos, bool) {
	found, ok := cube.Pos{}, false
	ManhattanOrdered(pos, xz, y, xz, xz+y+xz, func(p cube.Pos) bool {
		if p.OutOfBounds(tx.Range()) {
			return true
		}
		if match(tx.Block(p)) {
			found, ok = p, true
			return false
		}
		return true
	})
	return found, ok
}

// ManhattanOrdered is BlockPos.manhattanOrdered: the positions around
// origin within reach on each axis, by increasing Manhattan distance up to
// maxDepth, in Java's order (x, then y ascending; z, then -z). f returning
// false stops.
func ManhattanOrdered(origin cube.Pos, rx, ry, rz, maxDepth int, f func(cube.Pos) bool) {
	for depth := 0; depth <= maxDepth; depth++ {
		mx := min(rx, depth)
		for x := -mx; x <= mx; x++ {
			my := min(ry, depth-abs(x))
			for y := -my; y <= my; y++ {
				z := depth - abs(x) - abs(y)
				if z > rz {
					continue
				}
				if !f(origin.Add(cube.Pos{x, y, z})) {
					return
				}
				if z != 0 && !f(origin.Add(cube.Pos{x, y, -z})) {
					return
				}
			}
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
