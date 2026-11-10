//go:build crossfly_full

package block

import (
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Observers with redstone=dragonfly: Bedrock Edition's rules (BDS 1.21.3 ObserverBlock, PulseCapacitor).
//
//   - An observer reacts to a neighbour update from the block it faces (any change there, a state change too), and only
//     while it is off: changes during its pulse are passed by.
//   - It fires a redstone tick (two game ticks) after the change, and gives a pulse of one redstone tick: strong power
//     out of its back.
//   - Placing it gives no pulse. Moved by a piston, it fires at once (Bedrock's movedByPiston).

// dfObserverDelay is the time from a change to the pulse, and the length of the pulse, in game ticks.
const dfObserverDelay = 2

// dfNeighbourUpdate schedules the pulse for a change in front of the observer.
func (o Observer) dfNeighbourUpdate(pos, changed cube.Pos, tx *world.Tx) {
	if o.Powered || changed != pos.Side(o.Facing) {
		return
	}
	tx.ScheduleBlockUpdate(pos, o, dfObserverDelay*time.Second/20)
}

// dfTick turns the pulse on, or off once it has lasted.
func (o Observer) dfTick(pos cube.Pos, tx *world.Tx) {
	o.Powered = !o.Powered
	dfSetOutput(pos, o, tx)
	if o.Powered {
		tx.ScheduleBlockUpdate(pos, o, dfObserverDelay*time.Second/20)
	}
}

// MovedByPiston fires the observer at once when a piston moved it, as Bedrock Edition's observer does (flying
// machines).
func (o Observer) MovedByPiston(pos cube.Pos, tx *world.Tx) {
	if world.JavaRedstone() {
		return
	}
	if o, ok := tx.Block(pos).(Observer); ok && !o.Powered {
		o.dfTick(pos, tx)
	}
}

// NeighbourUpdateTick: with redstone=dragonfly a neighbour update from the block in front fires the observer. A Java
// Edition observer reacts to shape updates only (BlockStateChanged).
func (o Observer) NeighbourUpdateTick(pos, changed cube.Pos, tx *world.Tx) {
	if world.JavaRedstone() {
		return
	}
	o.dfNeighbourUpdate(pos, changed, tx)
}
