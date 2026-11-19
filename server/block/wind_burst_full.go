//go:build crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// windBurstToggle presses the button, whatever it is made of (ButtonBlock::buttonPressed).
func (b Button) windBurstToggle(pos cube.Pos, tx *world.Tx, src world.Entity) {
	b.pressBy(pos, tx, src)
}

// windBurstToggle rings the bell the way it faces (BellBlockActor::ring with the block's direction).
func (b Bell) windBurstToggle(pos cube.Pos, tx *world.Tx, _ world.Entity) {
	b.ring(pos, tx, b.Facing.Face())
}
