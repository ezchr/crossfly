//go:build crossfly_full

package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// A lectern gives a two tick redstone pulse when its page turns, with both redstone engines (lecternPulses).

// ScheduledTick ends the page turn pulse.
func (l Lectern) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if l.Powered {
		l.Powered = false
		tx.SetBlock(pos, l, nil)
	}
}

// RedstonePower gives power to every side during the page turn pulse.
func (l Lectern) RedstonePower(cube.Pos, *world.Tx, cube.Face) int {
	if l.Powered {
		return 15
	}
	return 0
}

// RedstoneStrongPower strongly powers the block below during the page turn pulse.
func (l Lectern) RedstoneStrongPower(_ cube.Pos, _ *world.Tx, face cube.Face) int {
	if l.Powered && face == cube.FaceDown {
		return 15
	}
	return 0
}
