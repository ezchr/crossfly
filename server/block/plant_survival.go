package block

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// breakOrScheduleBreak breaks a plant that can no longer stay where it is. With Java redstone it breaks a tick
// later instead, as Java's sugar cane, cactus and bamboo do (updateShape schedules a tick, the tick destroys the
// block): a cane on top of one a piston breaks is still there for that tick.
func breakOrScheduleBreak(b world.Block, pos cube.Pos, tx *world.Tx) {
	if world.JavaRedstone() {
		tx.ScheduleBlockUpdate(pos, b, time.Second/20)
		return
	}
	breakBlock(b, pos, tx)
}

// popResourcePos is where a broken block drops an item: its centre, or with Java redstone where Java's
// Block.popResource puts it, up to a quarter block off the centre on each axis and half an item lower.
func popResourcePos(pos cube.Pos) mgl64.Vec3 {
	if !world.JavaRedstone() {
		return pos.Vec3Centre()
	}
	off := func() float64 { return rand.Float64()*0.5 - 0.25 }
	return pos.Vec3Centre().Add(mgl64.Vec3{off(), off() - 0.125, off()})
}
