package block

import (
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
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

