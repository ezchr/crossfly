//go:build crossfly_full

package entity

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Primed TNT inside a nether portal goes to the nether as in Java; with Config.EntitiesCreatePortals it makes the
// destination portal itself.
func TestTNTTravelsThroughPortalCreatingOne(t *testing.T) {
	var overworld, nether *world.World
	overworld = world.Config{Entities: DefaultRegistry, EntitiesCreatePortals: true, PortalDestination: func(dim world.Dimension) *world.World {
		if dim == world.Nether {
			return nether
		}
		return nil
	}}.New()
	nether = world.Config{Dim: world.Nether, Entities: DefaultRegistry, EntitiesCreatePortals: true, PortalDestination: func(dim world.Dimension) *world.World {
		if dim == world.Nether {
			return overworld
		}
		return nil
	}}.New()
	t.Cleanup(func() {
		_ = overworld.Close()
		_ = nether.Close()
	})
	sourcePortal := cube.Pos{80, 64, 80}
	mustDo(t, overworld, func(tx *world.Tx) {
		buildActivePortal(tx, sourcePortal)
	})
	handle := NewTNT(world.EntitySpawnOpts{Position: sourcePortal.Vec3Middle(), Velocity: mgl64.Vec3{0, 0.001, 0}}, time.Second*4)
	mustDo(t, overworld, func(tx *world.Tx) {
		e := tx.AddEntity(handle)
		e.(world.TickerEntity).Tick(tx, 1)
	})
	waitForEntityWorld(t, handle, nether)
}
