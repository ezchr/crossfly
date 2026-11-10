//go:build crossfly_full

package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Turtle eggs only hatch with the mobs: they register in Crossfly Full.
func init() {
	mobBlocks = append(mobBlocks, func() {
		registerAll(allTurtleEggs())
		world.RegisterItem(TurtleEgg{})
	})
}

// RandomTick queues the steps of turtle eggs that have none queued, such as eggs set by a command or a
// structure, which never ran OnPlace.
func (t TurtleEgg) RandomTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if !tx.BlockUpdateScheduled(pos, t) {
		tx.ScheduleBlockUpdate(pos, t, turtleEggTickDelay())
	}
}
