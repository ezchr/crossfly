package block

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Java Edition's FallingBlock (sand, gravel, concrete powder, anvils, the dragon egg) does not fall the moment
// something next to it changes: onPlace and updateShape schedule a tick two ticks later, and the block falls then if
// the space below is still free. With Java redstone these blocks do the same; otherwise they fall at once, as on
// Bedrock Edition.

// gravityDelay is FallingBlock.getDelayAfterPlace: two ticks, five for the dragon egg.
const gravityDelay = time.Second / 10

func gravityDelayOf(b world.Block) time.Duration {
	if _, ok := b.(DragonEgg); ok {
		return time.Second / 4
	}
	return gravityDelay
}

// gravityUpdate makes b at pos fall now (Bedrock) or schedules the fall (Java): a neighbour changed.
func (g gravityAffected) gravityUpdate(b world.Block, pos cube.Pos, tx *world.Tx) {
	if world.JavaRedstone() {
		tx.ScheduleBlockUpdate(pos, b, gravityDelayOf(b))
		return
	}
	g.fall(b, pos, tx)
}

// gravityPlaced is FallingBlock.onPlace: a fall is scheduled whenever the block is placed (or moved by a piston).
func gravityPlaced(b world.Block, pos cube.Pos, tx *world.Tx) {
	if world.JavaRedstone() {
		tx.ScheduleBlockUpdate(pos, b, gravityDelayOf(b))
	}
}

// gravityShapeUpdate is FallingBlock.updateShape: a neighbour's state changed.
func gravityShapeUpdate(b world.Block, pos cube.Pos, tx *world.Tx) {
	if world.JavaRedstone() {
		tx.ScheduleBlockUpdate(pos, b, gravityDelayOf(b))
	}
}

// BlockPlaced ...
func (s Sand) BlockPlaced(pos cube.Pos, _ world.Block, tx *world.Tx) { gravityPlaced(s, pos, tx) }

// BlockStateChanged ...
func (s Sand) BlockStateChanged(pos, _ cube.Pos, tx *world.Tx) { gravityShapeUpdate(s, pos, tx) }

// ScheduledTick ...
func (s Sand) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) { s.fall(s, pos, tx) }

// BlockPlaced ...
func (g Gravel) BlockPlaced(pos cube.Pos, _ world.Block, tx *world.Tx) { gravityPlaced(g, pos, tx) }

// BlockStateChanged ...
func (g Gravel) BlockStateChanged(pos, _ cube.Pos, tx *world.Tx) { gravityShapeUpdate(g, pos, tx) }

// ScheduledTick ...
func (g Gravel) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) { g.fall(g, pos, tx) }

// BlockPlaced ...
func (a Anvil) BlockPlaced(pos cube.Pos, _ world.Block, tx *world.Tx) { gravityPlaced(a, pos, tx) }

// BlockStateChanged ...
func (a Anvil) BlockStateChanged(pos, _ cube.Pos, tx *world.Tx) { gravityShapeUpdate(a, pos, tx) }

// ScheduledTick ...
func (a Anvil) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) { a.fall(a, pos, tx) }

// BlockPlaced ...
func (d DragonEgg) BlockPlaced(pos cube.Pos, _ world.Block, tx *world.Tx) { gravityPlaced(d, pos, tx) }

// BlockStateChanged ...
func (d DragonEgg) BlockStateChanged(pos, _ cube.Pos, tx *world.Tx) { gravityShapeUpdate(d, pos, tx) }

// ScheduledTick ...
func (d DragonEgg) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) { d.fall(d, pos, tx) }

// BlockPlaced ...
func (c ConcretePowder) BlockPlaced(pos cube.Pos, _ world.Block, tx *world.Tx) {
	gravityPlaced(c, pos, tx)
}

// BlockStateChanged is ConcretePowderBlock.updateShape: water beside it hardens it now, else it may fall later.
func (c ConcretePowder) BlockStateChanged(pos, _ cube.Pos, tx *world.Tx) {
	if c.harden(pos, tx) {
		return
	}
	gravityShapeUpdate(c, pos, tx)
}

// ScheduledTick ...
func (c ConcretePowder) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) { c.fall(c, pos, tx) }
