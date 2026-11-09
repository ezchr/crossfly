//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/tame"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestSneakUseTamedMountDoesNotRide: a sneaking owner using a tamed horse or
// donkey opens its inventory (crouching_skip_interact); it must not get on.
func TestSneakUseTamedMountDoesNotRide(t *testing.T) {
	for _, id := range []string{"minecraft:horse", "minecraft:donkey", "minecraft:llama"} {
		t.Run(id, func(t *testing.T) {
			w := mobtest.New(t, mobtest.Options{})
			w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 6, block.Stone{}) })
			r := w.Spawn(id, mgl64.Vec3{0.5, 1, 2.5}, mob.SpawnOptions{Event: "minecraft:spawn_adult"})
			p := w.AddPlayer("owner", mgl64.Vec3{0.5, 1, 0.5})
			w.Tick(2)
			p.With(func(pl *player.Player) {
				m, _ := r.Mob(pl.Tx())
				if !tame.Tame(m, pl) {
					t.Fatalf("%v: not tameable", id)
				}
			})
			w.Tick(2)
			p.With(func(pl *player.Player) { pl.StartSneaking() })
			p.Interact(r, item.Stack{})
			p.With(func(pl *player.Player) {
				if _, riding := pl.RidingEntity(pl.Tx()); riding {
					m, _ := r.Mob(pl.Tx())
					t.Fatalf("%v: sneaking owner got on the tamed mob; components %v", id, m.Components())
				}
			})
		})
	}
}
