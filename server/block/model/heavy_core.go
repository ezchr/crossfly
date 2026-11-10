package model

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// HeavyCore is the model of a heavy core: a half-block-high cube half a block wide, centred.
type HeavyCore struct{}

// BBox ...
func (HeavyCore) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	return []cube.BBox{cube.Box(0.25, 0, 0.25, 0.75, 0.5, 0.75)}
}

// FaceSolid ...
func (HeavyCore) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool { return false }
