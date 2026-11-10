//go:build crossfly_full

package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Frogspawn only hatches with the mobs: it registers in Crossfly Full.
func init() {
	mobBlocks = append(mobBlocks, func() {
		world.RegisterBlock(FrogSpawn{})
		world.RegisterItem(FrogSpawn{})
	})
}

// RandomTick queues the hatching of frogspawn that has none queued, such as frogspawn set by a command or a
// structure, which never ran OnPlace.
func (f FrogSpawn) RandomTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if !tx.BlockUpdateScheduled(pos, f) {
		f.OnPlace(pos, tx)
	}
}
