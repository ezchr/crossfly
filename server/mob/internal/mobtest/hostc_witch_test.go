//go:build crossfly_full

package mobtest_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/hostile"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestWitchMagicDamage: Witch::getDamageAfterEnchantReduction keeps 15% of
// magic damage, leaves melee alone and drops damage the witch did itself.
func TestWitchMagicDamage(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
	wi := w.Spawn("minecraft:witch", mgl64.Vec3{0.5, 1, 0.5})
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{3.5, 1, 0.5})
	w.Do(func(tx *world.Tx) {
		m, _ := wi.Mob(tx)
		zm, _ := z.Mob(tx)
		for _, c := range []struct {
			name   string
			d      mob.Damage
			want   float64
			cancel bool
		}{
			{"instant damage", mob.Damage{Amount: 6, Source: effect.InstantDamageSource{}}, 0.9, false},
			{"poison", mob.Damage{Amount: 1, Source: effect.PoisonDamageSource{}}, 0.15, false},
			{"melee", mob.Damage{Amount: 4, Source: entity.AttackDamageSource{Attacker: zm}, Attacker: zm}, 4, false},
			{"own hit", mob.Damage{Amount: 4, Source: entity.AttackDamageSource{Attacker: m}, Attacker: m}, 4, true},
		} {
			d := c.d
			hostile.WitchDamage(m, &d)
			if d.Cancel != c.cancel || (!c.cancel && math.Abs(d.Amount-c.want) > 1e-9) {
				t.Errorf("%v: got %v (cancel %v), want %v (cancel %v)", c.name, d.Amount, d.Cancel, c.want, c.cancel)
			}
		}
		// Other mobs are left alone.
		d := mob.Damage{Amount: 6, Source: effect.InstantDamageSource{}}
		hostile.WitchDamage(zm, &d)
		if d.Amount != 6 {
			t.Errorf("zombie magic damage changed to %v", d.Amount)
		}
		// Mob.Hurt runs it through the damage hook.
		before := m.Health()
		m.Hurt(6, effect.InstantDamageSource{})
		if got := before - m.Health(); math.Abs(got-0.9) > 1e-9 {
			t.Errorf("witch hurt by 6 instant damage lost %v, want 0.9", got)
		}
	})
}

// TestWitchAvoidsWaterWithoutTarget: Witch::preAiStep sets avoid_water
// while the witch has no target and clears it while it has one.
func TestWitchAvoidsWaterWithoutTarget(t *testing.T) {
	needs(t, true, false, "minecraft:navigation.walk")
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
	wi := w.Spawn("minecraft:witch", mgl64.Vec3{0.5, 1, 0.5})
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{3.5, 1, 0.5})
	type avoider interface{ AvoidWater() bool }
	w.Tick(2)
	wi.With(func(m *mob.Mob) {
		n, ok := m.Navigation().(avoider)
		if !ok {
			t.Skip("navigation has no AvoidWater")
		}
		if !n.AvoidWater() {
			t.Error("witch without a target does not avoid water")
		}
	})
	w.Do(func(tx *world.Tx) {
		m, _ := wi.Mob(tx)
		ze, _ := z.Entity(tx)
		m.SetTarget(ze)
	})
	w.Tick(1)
	wi.With(func(m *mob.Mob) {
		if _, ok := m.Target(); !ok {
			t.Skip("witch dropped the target")
		}
		if m.Navigation().(avoider).AvoidWater() {
			t.Error("witch with a target still avoids water")
		}
	})
}
