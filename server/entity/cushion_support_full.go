//go:build crossfly_full

package entity

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// cushionFullOutline returns the vanilla outline of blocks only Crossfly Full has, none of which have a collision
// box of their own (see cushionOutline).
func cushionFullOutline(b world.Block) ([]cube.BBox, bool) {
	switch b := b.(type) {
	case block.CoralFan:
		if b.Wall {
			return cushionShape(0.25, 0.75), true
		}
		return cushionShape(0, 0.25), true
	case block.Button:
		depth := 0.125
		if b.Pressed {
			depth = 0.0625
		}
		switch b.Facing {
		case cube.FaceUp:
			return cushionShape(0, depth), true
		case cube.FaceDown:
			return cushionShape(1-depth, 1), true
		}
		return cushionShape(0.375, 0.625), true
	case block.PressurePlate:
		if b.Power > 0 {
			return cushionShape(0, 0.03125), true
		}
		return cushionShape(0, 0.0625), true
	case block.Rail:
		return cushionRailShape(b.Shape), true
	case block.PoweredRail:
		return cushionRailShape(b.Shape), true
	case block.ActivatorRail:
		return cushionRailShape(b.Shape), true
	case block.DetectorRail:
		return cushionRailShape(b.Shape), true
	case block.TripwireHook:
		return cushionShape(0, 0.625), true
	}
	return nil, false
}

// cushionRailShape returns the outline of a rail: Flat, or a slope.
func cushionRailShape(shape block.RailShape) []cube.BBox {
	if shape.Slope() {
		return cushionShape(0, 0.5)
	}
	return cushionShape(0, 0.125)
}
