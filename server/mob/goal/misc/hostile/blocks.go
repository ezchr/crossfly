//go:build crossfly_full

package hostile

import (
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/kit"
	"github.com/df-mc/dragonfly/server/world"
)

// matchBlock matches a block against a block list, with the legacy group
// names entity JSON still uses (leaves, sapling, tallgrass, red_flower,
// double_plant, ...).
func matchBlock(l kit.BlockList, b world.Block) bool {
	name := kit.BlockName(b)
	if l.HasName(name) {
		return true
	}
	short := strings.TrimPrefix(name, "minecraft:")
	for _, n := range l {
		switch strings.TrimPrefix(n, "minecraft:") {
		case "leaves", "leaves2":
			if strings.HasSuffix(short, "_leaves") && !strings.Contains(short, "azalea") {
				return true
			}
		case "sapling":
			if strings.HasSuffix(short, "_sapling") {
				return true
			}
		case "tallgrass":
			if short == "short_grass" || short == "tall_grass" || short == "fern" || short == "large_fern" {
				return true
			}
		case "red_flower":
			if _, ok := b.(block.Flower); ok {
				return true
			}
		case "double_plant":
			if _, ok := b.(block.DoubleFlower); ok {
				return true
			}
			if _, ok := b.(block.DoubleTallGrass); ok {
				return true
			}
		case "deadbush":
			if short == "deadbush" || short == "dead_bush" {
				return true
			}
		case "reeds":
			if short == "sugar_cane" || short == "reeds" {
				return true
			}
		case "snow_layer":
			if short == "snow_layer" {
				return true
			}
		}
	}
	return false
}
