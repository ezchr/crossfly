//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// throwEggs drops n thrown eggs of variant onto the stone floor around the origin and returns the chickens
// that hatched.
func throwEggs(t *testing.T, w *mobtest.World, v item.EggVariant, n int) []*mobtest.Ref {
	t.Helper()
	w.Do(func(tx *world.Tx) {
		for i := range n {
			pos := mgl64.Vec3{float64(i%20) - 9.5, 3, float64(i/20%20) - 9.5}
			tx.AddEntity(entity.NewEggOf(world.EntitySpawnOpts{Position: pos, Velocity: mgl64.Vec3{0, -0.6, 0}}, item.Egg{Variant: v}, nil))
		}
	})
	w.Tick(20)
	return w.Mobs("minecraft:chicken")
}

// TestThrownEggsHatch: thrown eggs hatch baby chickens one time in eight (egg.json spawn_chance), on the
// floor, persistent, and brown and blue eggs hatch warm and cold chicks.
func TestThrownEggsHatch(t *testing.T) {
	mobtest.RequireDefinitions(t)
	mobtest.RequireFilters(t)
	for _, c := range []struct {
		v       item.EggVariant
		climate string
	}{
		{item.EggTemperate, "temperate"},
		{item.EggWarm, "warm"},
		{item.EggCold, "cold"},
	} {
		t.Run(c.climate, func(t *testing.T) {
			w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
			w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 12, block.Stone{}) })
			// 400 eggs: about 50 hatch; none hatching has a chance of (7/8)^400, 1 in 10^23.
			chicks := throwEggs(t, w, c.v, 400)
			t.Logf("%d chickens hatched from 400 eggs", len(chicks))
			if len(chicks) == 0 || len(chicks) > 160 {
				t.Fatalf("%d chickens hatched from 400 eggs, want about 50", len(chicks))
			}
			for _, r := range chicks {
				r.With(func(m *mob.Mob) {
					if !m.Baby() {
						t.Errorf("hatched chicken is not a baby")
					}
					if got, _ := m.Property("minecraft:climate_variant"); got != c.climate {
						t.Errorf("chick from a %v egg is %v", c.climate, got)
					}
					if !m.Persistent() {
						t.Errorf("hatched chicken may despawn")
					}
					if y := m.Position()[1]; y < 0.99 || y > 1.5 {
						t.Errorf("chick hatched at y %v, want on the floor at 1", y)
					}
				})
			}
		})
	}
}

// TestPlayerThrowsColouredEgg: a player throwing a blue egg throws a blue thrown egg.
func TestPlayerThrowsColouredEgg(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 6, block.Stone{}) })
	p := w.AddPlayer("thrower", mgl64.Vec3{0.5, 1, 0.5})
	p.Hold(item.NewStack(item.Egg{Variant: item.EggCold}, 2))
	p.With(func(pl *player.Player) { pl.UseItem() })
	var eggs []item.Stack
	w.Do(func(tx *world.Tx) {
		for e := range tx.Entities() {
			if s, ok := entity.ThrownEggItem(e); ok {
				eggs = append(eggs, s)
			}
		}
	})
	if len(eggs) != 1 || eggs[0].Item() != (item.Egg{Variant: item.EggCold}) {
		t.Fatalf("thrown eggs %v, want one blue egg", eggs)
	}
}
