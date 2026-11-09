//go:build crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

type pistonModel struct {
	Facing   cube.Face
	Extended bool
}

func (m pistonModel) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	if !m.Extended {
		return []cube.BBox{cube.Box(0, 0, 0, 1, 1, 1)}
	}
	return []cube.BBox{pistonBox(m.Facing, 0, .75, 0, 1)}
}
func (m pistonModel) FaceSolid(_ cube.Pos, f cube.Face, _ world.BlockSource) bool {
	return !m.Extended || f == m.Facing.Opposite()
}

type pistonHeadModel struct{ Facing cube.Face }

func (m pistonHeadModel) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	return []cube.BBox{pistonBox(m.Facing, .75, 1, 0, 1), pistonBox(m.Facing, -.25, .75, .375, .625)}
}
func (m pistonHeadModel) FaceSolid(_ cube.Pos, f cube.Face, _ world.BlockSource) bool {
	return f == m.Facing
}
func pistonBox(face cube.Face, lo, hi, edgeLo, edgeHi float64) cube.BBox {
	switch face {
	case cube.FaceEast:
		return cube.Box(lo, edgeLo, edgeLo, hi, edgeHi, edgeHi)
	case cube.FaceWest:
		return cube.Box(1-hi, edgeLo, edgeLo, 1-lo, edgeHi, edgeHi)
	case cube.FaceUp:
		return cube.Box(edgeLo, lo, edgeLo, edgeHi, hi, edgeHi)
	case cube.FaceDown:
		return cube.Box(edgeLo, 1-hi, edgeLo, edgeHi, 1-lo, edgeHi)
	case cube.FaceSouth:
		return cube.Box(edgeLo, edgeLo, lo, edgeHi, edgeHi, hi)
	default:
		return cube.Box(edgeLo, edgeLo, 1-hi, edgeHi, edgeHi, 1-lo)
	}
}

type retractingPistonModel struct {
	Facing   cube.Face
	Progress float64
}

func (m retractingPistonModel) BBox(pos cube.Pos, src world.BlockSource) []cube.BBox {
	boxes := (pistonModel{Facing: m.Facing, Extended: true}).BBox(pos, src)
	for _, box := range (pistonHeadModel{Facing: m.Facing}).BBox(pos, src) {
		boxes = append(boxes, box.TranslateTowards(m.Facing, m.Progress))
	}
	return boxes
}
func (m retractingPistonModel) FaceSolid(_ cube.Pos, f cube.Face, _ world.BlockSource) bool {
	return f == m.Facing.Opposite()
}
