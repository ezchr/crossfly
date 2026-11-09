//go:build crossfly_full

package locomotion_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/locomotion"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	_ "github.com/df-mc/dragonfly/server/mob/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func health(w *mobtest.World, r *mobtest.Ref) float64 {
	var h float64
	w.Do(func(tx *world.Tx) {
		if m, ok := r.Mob(tx); ok {
			h = m.Health()
		}
	})
	return h
}

// TestWaterSplashHurtsWaterSensitive: a splash water bottle makes
// in_contact_with_water true for one tick, so the hurt_on_condition drowning
// damage (1) hits blazes, endermen and snow golems within 4 blocks, not
// zombies, and an axolotl's drying_out_timer refills.
func TestWaterSplashHurtsWaterSensitive(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 10, block.Stone{}) })
	ids := []string{"minecraft:blaze", "minecraft:enderman", "minecraft:snow_golem", "minecraft:zombie"}
	offs := []mgl64.Vec3{{0.5, 1, 0.5}, {2.5, 1, 0.5}, {0.5, 1, 2.5}, {-1.5, 1, 0.5}}
	refs := make([]*mobtest.Ref, len(ids))
	for i, id := range ids {
		refs[i] = w.Spawn(id, offs[i])
	}
	w.Tick(2)
	before := make([]float64, len(ids))
	for i, r := range refs {
		r.With(func(m *mob.Mob) {
			m.Goals().ClearAll()
		})
		before[i] = health(w, r)
	}
	splashWater(w, mgl64.Vec3{0.5, 4, 0.5})
	w.Tick(20)
	want := []float64{1, 1, 1, 0}
	for i, r := range refs {
		if got := before[i] - health(w, r); got != want[i] {
			t.Errorf("%v lost %.1f health to a water bottle, want %.0f", ids[i], got, want[i])
		}
	}

	// An axolotl out of water for 10 s gets water_bottle_refill_time (90 s)
	// back, up to total_time (300 s).
	w = mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 10, block.Stone{}) })
	a := w.Spawn("minecraft:axolotl", mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(200)
	var dryBefore, dryAfter int
	a.With(func(m *mob.Mob) { dryBefore, _ = locomotion.DryingRemaining(m) })
	splashWater(w, a.Pos().Add(mgl64.Vec3{0, 2, 0}))
	w.Tick(20)
	a.With(func(m *mob.Mob) { dryAfter, _ = locomotion.DryingRemaining(m) })
	if dryAfter < 5960 {
		t.Errorf("axolotl drying timer %d -> %d after a water bottle, want close to 6000", dryBefore, dryAfter)
	}
}

func splashWater(w *mobtest.World, pos mgl64.Vec3) {
	w.Do(func(tx *world.Tx) {
		tx.AddEntity(entity.NewSplashPotion(world.EntitySpawnOpts{Position: pos, Velocity: mgl64.Vec3{0, -0.5, 0}}, potion.Water(), nil))
	})
}

// TestSnowballHurtsBlazes: a snowball (impact_damage 3, filter blaze) hurts
// a blaze by 3 and nothing else.
func TestSnowballHurtsBlazes(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 10, block.Stone{}) })
	b := w.Spawn("minecraft:blaze", mgl64.Vec3{0.5, 1, 0.5})
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{0.5, 1, 6.5})
	w.Tick(2)
	for _, r := range []*mobtest.Ref{b, z} {
		r.With(func(m *mob.Mob) { m.Goals().ClearAll() })
	}
	w.Do(func(tx *world.Tx) {
		tx.AddEntity(entity.NewSnowball(world.EntitySpawnOpts{Position: mgl64.Vec3{3.5, 2, 0.5}, Velocity: mgl64.Vec3{-1, 0, 0}}, nil))
		tx.AddEntity(entity.NewSnowball(world.EntitySpawnOpts{Position: mgl64.Vec3{3.5, 2, 6.5}, Velocity: mgl64.Vec3{-1, 0, 0}}, nil))
	})
	w.Tick(10)
	if h := health(w, b); h != 17 {
		t.Errorf("blaze hit by a snowball has %.1f health, want 17", h)
	}
	if h := health(w, z); h != 20 {
		t.Errorf("zombie hit by a snowball has %.1f health, want 20", h)
	}
}
