//go:build crossfly_full

package jnether

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestGhastShoots: a ghast that sees a player within 64 opens its mouth
// after 10 ticks and fires a large fireball at tick 20.
func TestGhastShoots(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 24, block.Stone{}) })
	w.AddPlayer("tester", mgl64.Vec3{0.5, 1, 0.5})
	g := spawnJ(t, w, "minecraft:ghast", mgl64.Vec3{14.5, 4, 0.5})
	charged, shot := false, 0
	for i := 0; i < 200 && shot == 0; i++ {
		w.Tick(1)
		withJ(t, w, g, func(m *javamob.Mob) {
			charged = charged || m.Meta().Charged
		})
		w.Do(func(tx *world.Tx) {
			for e := range tx.EntitiesWithin(cube.Box(-30, -10, -30, 40, 40, 30)) {
				if e.H().Type() == LargeFireballType {
					shot = i + 1
				}
			}
		})
	}
	if shot == 0 {
		t.Fatal("no fireball in 10 seconds")
	}
	if !charged {
		t.Error("the ghast never opened its mouth")
	}
}

// TestGhastReflectedFireball: a large fireball a player sent back deals
// 1000; any other fire does nothing to it.
func TestGhastReflectedFireball(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
	p := w.AddPlayer("tester", mgl64.Vec3{0.5, 1, 0.5})
	g := spawnJ(t, w, "minecraft:ghast", mgl64.Vec3{4.5, 4, 0.5})
	withJ(t, w, g, func(m *javamob.Mob) {
		b := m.Behaviour().(*Ghast)
		d := &mob.Damage{Amount: 6, Source: FireballDamageSource{}}
		b.FilterDamage(m, d)
		if !d.Cancel {
			t.Error("an unowned fireball hurt the ghast")
		}
		pl, _ := p.Entity(m.Tx())
		d = &mob.Damage{Amount: 6, Source: FireballDamageSource{ProjectileDamageSource: entity.ProjectileDamageSource{Owner: pl}, Large: true}}
		b.FilterDamage(m, d)
		if d.Cancel || d.Amount != 1000 {
			t.Errorf("reflected fireball: %v cancel %v, want 1000", d.Amount, d.Cancel)
		}
	})
}

// TestShulkerStaysPut: a shulker does not move when hit or pushed; it
// stays in the middle of its block.
func TestShulkerStaysPut(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
	s := spawnJ(t, w, ShulkerID, mgl64.Vec3{2.3, 1, 2.8})
	p := w.AddPlayer("tester", mgl64.Vec3{5.5, 1, 2.5})
	w.Tick(2)
	withJ(t, w, s, func(m *javamob.Mob) {
		pl, _ := p.Entity(m.Tx())
		m.Hurt(1, entity.AttackDamageSource{Attacker: pl.(*player.Player)})
	})
	w.Tick(20)
	withJ(t, w, s, func(m *javamob.Mob) {
		if pos := m.Position(); pos != (mgl64.Vec3{2.5, 1, 2.5}) {
			t.Errorf("shulker at %v, want 2.5 1 2.5", pos)
		}
	})
}

// TestShulkerDuplicates: an open shulker alone hit by a shulker bullet
// teleports (when one of its 5 tries finds a spot) and leaves a second
// shulker of its colour where it was.
func TestShulkerDuplicates(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 12, block.Stone{}) })
	s := spawnJ(t, w, ShulkerID, mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(2)
	count := func() (n, yellow int) {
		w.Do(func(tx *world.Tx) {
			for e := range tx.EntitiesWithin(cube.Box(-20, -10, -20, 20, 20, 20)) {
				if m, ok := javamob.Of(e); ok && m.JavaType().ID == ShulkerID {
					n++
					if m.Behaviour().(*Shulker).colour == 4 {
						yellow++
					}
				}
			}
		})
		return
	}
	for i := 0; i < 60; i++ {
		withJ(t, w, s, func(m *javamob.Mob) {
			b := m.Behaviour().(*Shulker)
			b.SetColour(m, 4)
			b.setRawPeek(m, 100, false)
			bullet := world.EntitySpawnOpts{Position: m.Position()}.New(ShulkerBulletType, projectileConfig{newProjectile(ShulkerBulletType, nil)})
			be := m.Tx().AddEntity(bullet)
			m.Hurt(0.5, entity.ProjectileDamageSource{Projectile: be})
			_ = be.Close()
		})
		w.Tick(11)
		if n, _ := count(); n > 1 {
			break
		}
	}
	if n, yellow := count(); n != 2 || yellow != 2 {
		t.Errorf("%d shulkers (%d yellow), want 2 yellow", n, yellow)
	}
}

// TestBlazeFireImmune: small fireballs (other blazes' volleys) do not hurt
// a blaze.
func TestBlazeFireImmune(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
	b := spawnJ(t, w, "minecraft:blaze", mgl64.Vec3{0.5, 1, 0.5})
	withJ(t, w, b, func(m *javamob.Mob) {
		before := m.Health()
		m.Hurt(5, FireballDamageSource{})
		if m.Health() != before {
			t.Errorf("health %v after a fireball, want %v", m.Health(), before)
		}
	})
}
