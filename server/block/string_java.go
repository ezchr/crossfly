//go:build crossfly_full

package block

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// Tripwire with Java redstone (Crossfly Full): entities trip it, tripwire hooks (tripwire_hook.go) power it.

// tripwireHookFacing reports whether b is a tripwire hook facing the wire it is at face of.
func tripwireHookFacing(b world.Block, face cube.Face) bool {
	hook, ok := b.(TripwireHook)
	return ok && hook.Facing.Face() == face.Opposite()
}

// BlockPlaced lets the hooks of a circuit know a wire was added.
func (s String) BlockPlaced(pos cube.Pos, previous world.Block, tx *world.Tx) {
	if _, ok := previous.(String); !ok {
		s.updateSource(pos, tx)
	}
}

// BlockRemoved trips the circuit, as breaking a wire does in Java Edition, unless the wire was cut with shears.
func (s String) BlockRemoved(pos cube.Pos, replacement world.Block, tx *world.Tx) {
	if _, ok := replacement.(String); ok {
		return
	}
	s.Powered = true
	s.updateSource(pos, tx)
}

// PlayerWillBreak disarms the wire when a player cuts it with shears, so that the hooks detach without tripping.
func (s String) PlayerWillBreak(pos cube.Pos, held item.Stack, tx *world.Tx) {
	if !world.JavaRedstone() {
		return
	}
	if _, ok := held.Item().(item.Shears); ok {
		s.Disarmed = true
		tx.SetBlock(pos, s, &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true})
	}
}

// EntityInside trips the wire.
func (s String) EntityInside(pos cube.Pos, tx *world.Tx, e world.Entity) {
	if !world.JavaRedstone() {
		return
	}
	s.Connections = calculateConnections(s, tx, pos)
	if s.Powered || s.scheduled(pos, tx) || !tripwireDetects(e) || !entityIntersects(e, s.box(pos)) {
		return
	}
	s.checkPressed(pos, tx, true)
}

// ScheduledTick looks again whether something is still in a tripped wire.
func (s String) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if !world.JavaRedstone() {
		return
	}
	if s.Powered {
		s.checkPressed(pos, tx, s.entityInside(pos, tx))
	}
}

// box is the area in which an entity trips the wire.
func (s String) box(pos cube.Pos) cube.BBox {
	if s.Attached {
		return cube.Box(0, 1.0/16, 0, 1, 2.5/16, 1).Translate(pos.Vec3())
	}
	return cube.Box(0, 0, 0, 1, 0.5, 1).Translate(pos.Vec3())
}

func (s String) entityInside(pos cube.Pos, tx *world.Tx) bool {
	box := s.box(pos)
	for e := range tx.EntitiesWithin(box.Grow(1)) {
		if tripwireDetects(e) && entityIntersects(e, box) {
			return true
		}
	}
	return false
}

// tripwireDetects reports whether an entity trips a wire: any entity, except players that pass through blocks.
func tripwireDetects(e world.Entity) bool {
	if p, ok := e.(interface{ GameMode() world.GameMode }); ok && !p.GameMode().HasCollision() {
		return false
	}
	return true
}

// checkPressed is Java Edition's TripWireBlock.checkPressed.
func (s String) checkPressed(pos cube.Pos, tx *world.Tx, pressed bool) {
	// Scheduled ticks match the exact state, and the world derives the connections when it reads the wire.
	s.Connections = calculateConnections(s, tx, pos)
	was := s.Powered
	if pressed != was {
		s.Powered = pressed
		tx.SetBlock(pos, s, nil)
		s.updateSource(pos, tx)
	}
	if pressed {
		s.schedule(pos, tx, tripwireRecheck)
	} else if was {
		s.schedule(pos, tx, time.Second/20)
	}
}

// schedule schedules the wire's recheck for the states a hook may give it before then (attached or not): Java Edition
// keys the tick by block type, here it is kept per state.
func (s String) schedule(pos cube.Pos, tx *world.Tx, delay time.Duration) {
	if s.scheduled(pos, tx) {
		return
	}
	for _, a := range []bool{false, true} {
		v := s
		v.Attached = a
		tx.ScheduleBlockUpdate(pos, v, delay)
	}
}

// scheduled reports whether a recheck of the wire is on its way.
func (s String) scheduled(pos cube.Pos, tx *world.Tx) bool {
	for _, a := range []bool{false, true} {
		for _, p := range []bool{false, true} {
			v := s
			v.Attached, v.Powered = a, p
			if tx.BlockUpdateScheduled(pos, v) {
				return true
			}
		}
	}
	return false
}

// updateSource lets the hook to the south or west of the wire, if any, recalculate the circuit.
func (s String) updateSource(pos cube.Pos, tx *world.Tx) {
	for _, d := range []cube.Direction{cube.South, cube.West} {
		for i := 1; i < tripwireMaxLength; i++ {
			testPos := tripwireStep(pos, d, i)
			b := tx.Block(testPos)
			if hook, ok := b.(TripwireHook); ok {
				if hook.Facing == d.Opposite() {
					wire := s
					hook.calculateState(testPos, tx, false, true, i, &wire)
				}
				break
			}
			if _, ok := b.(String); !ok {
				break
			}
		}
	}
}
