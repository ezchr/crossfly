//go:build crossfly_full

package mob

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// BlockHost is the block.MobHost of the mobs: it lets blocks spawn mobs. Package mob installs it.
type BlockHost struct{}

// Compile time check that BlockHost implements block.MobHost.
var _ block.MobHost = BlockHost{}

func init() { block.SetMobHost(BlockHost{}) }

// SpawnMob ...
func (BlockHost) SpawnMob(tx *world.Tx, id string, pos mgl64.Vec3, opts block.MobSpawnOptions) (world.Entity, bool) {
	if _, ok := TypeFor(id); !ok {
		return nil, false
	}
	m, err := Spawn(tx, id, pos, SpawnOptions{Rotation: cube.Rotation{opts.Yaw, 0}, Event: opts.Event})
	if err != nil {
		return nil, false
	}
	if opts.Persistent {
		m.SetPersistent(true)
	}
	return m, true
}
