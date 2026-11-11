//go:build crossfly_full

package jnether

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestMagmaCubeSize: a size 4 magma cube has 16 health, 12 armour and hits
// for 4 + 2.
func TestMagmaCubeSize(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
	r := spawnJ(t, w, MagmaCubeID, mgl64.Vec3{0.5, 1, 0.5})
	withJ(t, w, r, func(m *javamob.Mob) {
		c := m.Behaviour().(*MagmaCube)
		c.SetSize(m, 4, true)
		if m.MaxHealth() != 16 || m.Attr(javamob.AttrArmor) != 12 || m.Attr(javamob.AttrAttackDamage) != 4 {
			t.Errorf("health %v armour %v damage %v", m.MaxHealth(), m.Attr(javamob.AttrArmor), m.Attr(javamob.AttrAttackDamage))
		}
	})
}

// TestMagmaCubeJumps: a jump from the ground gets +0.1 per size, a lava
// jump 0.22 + 0.05 per size, a water jump the base 0.04 nudge.
func TestMagmaCubeJumps(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
	r := spawnJ(t, w, MagmaCubeID, mgl64.Vec3{0.5, 1, 0.5})
	withJ(t, w, r, func(m *javamob.Mob) {
		c := m.Behaviour().(*MagmaCube)
		c.SetSize(m, 4, true)
		m.SetVelocity(mgl64.Vec3{})
		c.JumpFromGround(m)
		if want := m.JumpPower() + float64(float32(0.4)); math.Abs(m.Velocity()[1]-want) > 1e-6 {
			t.Errorf("ground jump %v, want %v", m.Velocity()[1], want)
		}
		m.SetVelocity(mgl64.Vec3{})
		c.JumpInLiquid(m, true)
		if want := float64(float32(0.22) + float32(0.2)); math.Abs(m.Velocity()[1]-want) > 1e-6 {
			t.Errorf("lava jump %v, want %v", m.Velocity()[1], want)
		}
		if c.JumpInLiquid(m, false) {
			t.Error("water jump overridden")
		}
	})
}

// TestMagmaCubeInLava: fire immune, so lava does not hurt it.
func TestMagmaCubeInLava(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 8, block.Stone{})
		w.Fill(tx, cube.Pos{-1, 1, -1}, cube.Pos{1, 1, 1}, block.Lava{Still: true, Depth: 8})
	})
	r := spawnJ(t, w, MagmaCubeID, mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(60)
	withJ(t, w, r, func(m *javamob.Mob) {
		if m.Health() < m.MaxHealth() {
			t.Errorf("hurt in lava: %v/%v", m.Health(), m.MaxHealth())
		}
	})
}
