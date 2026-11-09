//go:build crossfly_full

package model

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Diode is the model of a repeater or comparator: a slab two pixels high.
type Diode struct{}

// BBox ...
func (Diode) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	return []cube.BBox{cube.Box(0, 0, 0, 1, 0.125, 1)}
}

// FaceSolid ...
func (Diode) FaceSolid(_ cube.Pos, face cube.Face, _ world.BlockSource) bool {
	return face == cube.FaceDown
}
