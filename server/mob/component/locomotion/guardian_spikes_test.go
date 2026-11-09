//go:build crossfly_full

package locomotion_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	_ "github.com/df-mc/dragonfly/server/mob/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestGuardianSpikesMobDamage: the spikes of a still guardian hurt the mob
// behind any damage but magic, projectiles and block explosions: an
// exploding mob takes 2, a mob whose projectile hit takes nothing.
func TestGuardianSpikesMobDamage(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 10, block.Stone{}) })
	g := w.Spawn("minecraft:guardian", mgl64.Vec3{0.5, 1, 0.5})
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{3.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:as_adult"})
	w.Tick(2)
	cases := []struct {
		name string
		src  func(a world.Entity) world.DamageSource
		want float64
	}{
		{"explosion", func(a world.Entity) world.DamageSource {
			return entity.ExplosionDamageSource{Source: world.EntityExplosionSource{Entity: a}}
		}, 2},
		{"projectile", func(a world.Entity) world.DamageSource {
			return entity.ProjectileDamageSource{Owner: a}
		}, 0},
	}
	for _, c := range cases {
		w.Tick(15) // past both hurt cooldowns
		w.Do(func(tx *world.Tx) {
			zm, _ := z.Mob(tx)
			gm, _ := g.Mob(tx)
			gm.Goals().ClearAll()
			zm.Goals().ClearAll()
			before := zm.Health()
			gm.Hurt(1, c.src(zm))
			if got := before - zm.Health(); got != c.want {
				t.Errorf("%v hit on a still guardian: attacker lost %.1f, want %.0f", c.name, got, c.want)
			}
		})
	}
}
