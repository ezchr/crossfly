package model

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// SnowLayer is the model of a snow layer, one to eight layers of 2/16 of a block each.
type SnowLayer struct {
	// Height is the number of layers above the first, 0-7.
	Height int
}

// BBox returns the collision box of the layers: one layer less than drawn, like Java Edition (and Bedrock Edition
// with its newer snow collision). A single layer has no collision box at all.
func (s SnowLayer) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	if s.Height <= 0 {
		return nil
	}
	return []cube.BBox{cube.Box(0, 0, 0, 1, float64(s.Height)*0.125, 1)}
}

// FaceSolid returns true for the bottom face, and for every face once all eight layers are there.
func (s SnowLayer) FaceSolid(_ cube.Pos, face cube.Face, _ world.BlockSource) bool {
	return face == cube.FaceDown || s.Height >= 7
}
