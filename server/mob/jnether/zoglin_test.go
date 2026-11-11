//go:build crossfly_full

package jnether

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/jbrain"
	"github.com/df-mc/dragonfly/server/mob/jbrain/jmob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestZoglinHuntsPlayer: a zoglin picks a visible player as its attack
// target, switches to the fight activity, walks over and hits (6 attack
// damage: 3 to 8 per adult hit, every 40 ticks).
func TestZoglinHuntsPlayer(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 24, block.Stone{}) })
	p := w.AddPlayer("tester", mgl64.Vec3{0.5, 1, 0.5})
	p.SetMaxHealth(1000)
	z := spawnJ(t, w, ZoglinID, mgl64.Vec3{8.5, 1, 0.5})
	withJ(t, w, z, func(m *javamob.Mob) {
		// Zoglin.setBaby(false) keeps a spawned baby's 0.5 damage (as in Java).
		m.SetBaby(false)
		m.Attributes().Instance(javamob.AttrAttackDamage).SetBase(6)
		m.SyncAttributes()
	})
	start := p.Health()
	fought, hitAt := false, 0
	for i := 0; i < 400 && hitAt == 0; i++ {
		w.Tick(1)
		withJ(t, w, z, func(m *javamob.Mob) {
			b, _ := jmob.Of(m)
			if b.IsActive(jbrain.ActFight) {
				fought = true
			}
		})
		if p.Health() < start {
			hitAt = i + 1
		}
	}
	if !fought {
		t.Fatal("the zoglin never switched to fighting")
	}
	if hitAt == 0 {
		t.Fatal("the zoglin never hit the player")
	}
	if d := start - p.Health(); d < 3 || d > 8 {
		t.Errorf("first hit dealt %v, want 3..8", d)
	}
}

// TestZoglinIgnoresCreepersAndZoglins: with only a creeper and another
// zoglin around, a zoglin has no attack target.
func TestZoglinIgnoresCreepersAndZoglins(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 24, block.Stone{}) })
	z := spawnJ(t, w, ZoglinID, mgl64.Vec3{0.5, 1, 0.5})
	spawnJ(t, w, ZoglinID, mgl64.Vec3{4.5, 1, 0.5})
	spawnJ(t, w, "minecraft:creeper", mgl64.Vec3{0.5, 1, 4.5})
	w.Tick(100)
	withJ(t, w, z, func(m *javamob.Mob) {
		if mem := jmob.Mem(m); mem.Has(jbrain.MemAttackTarget.ID()) {
			t.Error("the zoglin targets a creeper or a zoglin")
		}
	})
}

// TestZoglinBaby: babies are smaller and deal 0.5.
func TestZoglinBaby(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
	z := spawnJ(t, w, ZoglinID, mgl64.Vec3{0.5, 1, 0.5})
	withJ(t, w, z, func(m *javamob.Mob) {
		m.SetBaby(true)
		if m.Attr(javamob.AttrAttackDamage) != 0.5 {
			t.Errorf("baby attack damage %v", m.Attr(javamob.AttrAttackDamage))
		}
		if bb := m.BBox(); bb.Width() > 0.8 {
			t.Errorf("baby width %v", bb.Width())
		}
	})
}
