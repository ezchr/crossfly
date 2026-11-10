package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/world"
)

// HeavyCore is a block found in trial chamber ominous vaults, crafted with a breeze rod into a mace.
type HeavyCore struct {
	transparent
	sourceWaterDisplacer
}

// SideClosed ...
func (HeavyCore) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool { return false }

// Model ...
func (HeavyCore) Model() world.BlockModel { return model.HeavyCore{} }

// BreakInfo ...
func (h HeavyCore) BreakInfo() BreakInfo {
	return newBreakInfo(10, alwaysHarvestable, pickaxeEffective, oneOf(h)).withBlastResistance(1200)
}

// EncodeItem ...
func (HeavyCore) EncodeItem() (name string, meta int16) { return "minecraft:heavy_core", 0 }

// EncodeBlock ...
func (HeavyCore) EncodeBlock() (string, map[string]any) { return "minecraft:heavy_core", nil }
