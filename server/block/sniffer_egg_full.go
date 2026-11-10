//go:build crossfly_full

package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Sniffer eggs only hatch with the mobs: they register in Crossfly Full.
func init() {
	mobBlocks = append(mobBlocks, func() {
		registerAll(allSnifferEggs())
		world.RegisterItem(SnifferEgg{})
	})
}

// RandomTick queues the steps of a sniffer egg that has none queued, such as one set by a command or a
// structure, which never ran OnPlace.
func (s SnifferEgg) RandomTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if !tx.BlockUpdateScheduled(pos, s) {
		s.OnPlace(pos, tx)
	}
}
