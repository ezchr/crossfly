//go:build crossfly_full

package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Entities pressing pressure plates, buttons and the blocks they step on (Crossfly Full, Java redstone).

// StepOnBlock notifies the block beneath an entity.
func StepOnBlock(tx *world.Tx, e world.Entity, pos mgl64.Vec3) {
	box := e.H().Type().BBox(e).Translate(pos)
	y := int(math.Floor(box.Min()[1] - 0.0001))
	horizontal := box.Grow(-0.0001)
	low, high := cube.PosFromVec3(horizontal.Min()), cube.PosFromVec3(horizontal.Max())

	for pos := range cube.Range3D(cube.Pos{low[0], y, low[2]}, cube.Pos{high[0], y, high[2]}) {
		if stepper, ok := tx.Block(pos).(block.EntityStepper); ok {
			stepper.EntityStepOn(pos, tx, e)
			return
		}
	}
}

// checkSteppers checks redstone components and the block beneath the entity.
func (m *Movement) checkSteppers(tx *world.Tx) {
	checkRedstoneInsiders(tx, m.e, m.pos)
	if m.onGround {
		StepOnBlock(tx, m.e, m.pos)
	}
}

// checkRedstoneInsiders notifies buttons and pressure plates touching an entity.
func checkRedstoneInsiders(tx *world.Tx, e world.Entity, pos mgl64.Vec3) {
	box := e.H().Type().BBox(e).Translate(pos).Grow(-0.0001)
	low, high := cube.PosFromVec3(box.Min()), cube.PosFromVec3(box.Max())
	for pos := range cube.Range3D(low, high) {
		switch b := tx.Block(pos).(type) {
		case block.PressurePlate:
			b.EntityInside(pos, tx, e)
		case block.Button:
			b.EntityInside(pos, tx, e)
		}
	}
}
