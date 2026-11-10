//go:build crossfly_full

package jnether

import (
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// fakeWither is a WitherHost without a mob.
type fakeWither struct {
	fakeDragon
	target world.Entity
}

func (f *fakeWither) BodyYaw() float64             { return f.yaw }
func (f *fakeWither) MaxHealth() float64           { return 300 }
func (f *fakeWither) Target() (world.Entity, bool) { return f.target, f.target != nil }

func newFakeWither(pos mgl64.Vec3) *fakeWither {
	return &fakeWither{fakeDragon: fakeDragon{
		h:   world.EntitySpawnOpts{Position: pos}.New(SmallFireballType, projectileConfig{newProjectile(SmallFireballType, nil)}),
		pos: pos, health: 300, r: rand.New(rand.NewPCG(9, 10)),
	}}
}

// TestWitherSpawnCharge: 220 ticks from a third of its health, +10 every
// 10 ticks, immune meanwhile, then an explosion that breaks blocks around.
func TestWitherSpawnCharge(t *testing.T) {
	w := newTestWorld(t, world.Overworld, world.DifficultyNormal)
	w.fill(cube.Pos{-3, 8, -3}, cube.Pos{3, 8, 3}, block.Dirt{})
	h := newFakeWither(mgl64.Vec3{0.5, 9, 0.5})
	wi := NewWither()
	w.do(func(tx *world.Tx) {
		h.tx = tx
		wi.MakeInvulnerable(h)
		if h.health != 100 {
			t.Fatalf("health %v after makeInvulnerable, want 100", h.health)
		}
		if wi.FilterDamage(h, entity.AttackDamageSource{}) {
			t.Error("hurt while charging")
		}
		for i := 1; i <= 220; i++ {
			h.ticks = i
			if wi.CustomServerAiStep(h) {
				t.Fatalf("tick %d: the base ran while charging", i)
			}
		}
		if wi.InvulnerableTicks() != 0 {
			t.Errorf("still %d ticks", wi.InvulnerableTicks())
		}
		if h.health != 300 {
			t.Errorf("health %v after charging, want 300 (22 x 10 from 100)", h.health)
		}
		if _, ok := tx.Block(cube.Pos{0, 8, 0}).(block.Dirt); ok {
			t.Error("the explosion broke nothing under it")
		}
	})
}

// TestWitherDamageRules: no drowning (wither_immune_to); a hit arms the
// block breaking (20 ticks); powered at half health.
func TestWitherDamageRules(t *testing.T) {
	w := newTestWorld(t, world.Overworld, world.DifficultyNormal)
	h := newFakeWither(mgl64.Vec3{0, 50, 0})
	wi := NewWither()
	w.do(func(tx *world.Tx) {
		h.tx = tx
		if wi.FilterDamage(h, entity.DrowningDamageSource{}) {
			t.Error("drowning hurt the wither")
		}
		if !wi.FilterDamage(h, entity.AttackDamageSource{}) {
			t.Error("a plain hit was refused")
		}
		if wi.destroyBlocksTick != 20 {
			t.Errorf("destroyBlocksTick %d, want 20", wi.destroyBlocksTick)
		}
		h.health = 150
		if !wi.Powered(h) {
			t.Error("not powered at half health")
		}
	})
}
