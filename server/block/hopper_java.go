//go:build crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

var _ world.RedstonePowerConsumer = Hopper{}

// JavaRedstoneExtension: power locks a hopper only with Java redstone.
func (Hopper) JavaRedstoneExtension() {}

// RedstonePowerUpdate updates the hopper's locked state to match the power it receives.
func (h Hopper) RedstonePowerUpdate(_ cube.Pos, _ *world.Tx, power int) (world.Block, bool) {
	if !world.JavaRedstone() {
		return h, false
	}
	powered := power > 0
	if h.Powered == powered {
		return h, false
	}
	h.Powered = powered
	return h, true
}
