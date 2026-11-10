//go:build crossfly_full

package mobtest_test

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/go-gl/mathgl/mgl64"
)

// wanderOnly spawns an adult happy ghast at pos over a wide stone floor with only its float_wander left
// and its home at home.
func wanderOnly(t *testing.T, pos mgl64.Vec3, home cube.Pos) (*mobtest.World, *mobtest.Ref) {
	t.Helper()
	mobtest.RequireDefinitions(t)
	mobtest.RequireFilters(t)
	w := mobtest.New(t, mobtest.Options{Time: 6000})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 100, block.Stone{}) })
	g := w.Spawn("minecraft:happy_ghast", pos, mob.SpawnOptions{Event: "minecraft:spawn_adult"})
	w.Tick(1)
	g.With(func(m *mob.Mob) {
		m.SetHome(home)
		for _, k := range m.Goals().Keys() {
			if k != "minecraft:behavior.float_wander" {
				m.Goals().Remove(k)
			}
		}
	})
	return w, g
}

// TestHappyGhastWandersWithinHome: float_wander's use_home_position_restriction keeps the targets of an
// adult happy ghast at the edge of its home restriction (64 blocks unharnessed) inside it.
func TestHappyGhastWandersWithinHome(t *testing.T) {
	home := cube.Pos{0, 3, 0}
	w, g := wanderOnly(t, mgl64.Vec3{60.5, 3, 0.5}, home)
	minD, maxD := 1e9, 0.0
	for i := 0; i < 80; i++ {
		w.Tick(50)
		d := g.Pos().Sub(home.Vec3Centre()).Len()
		minD, maxD = min(minD, d), max(maxD, d)
	}
	if maxD > 67 {
		t.Errorf("happy ghast wandered %.1f blocks from home (restriction 64)", maxD)
	}
	if maxD-minD < 2 {
		t.Errorf("happy ghast did not wander (%.1f to %.1f blocks from home):\n%v", minD, maxD, w.Dump(g))
	}
}

// TestHappyGhastWandersNearSurface: navigate_around_surface (surface_y_dist 16) draws an adult happy ghast
// high in the air down towards the ground.
func TestHappyGhastWandersNearSurface(t *testing.T) {
	w, g := wanderOnly(t, mgl64.Vec3{0.5, 40, 0.5}, cube.Pos{0, 40, 0})
	w.Tick(4000)
	if y := g.Pos()[1]; y > 30 {
		t.Errorf("happy ghast still at y %.1f, 40 blocks over the ground:\n%v", y, w.Dump(g))
	}
}

// TestHappyGhastHeals: a hurt happy ghast heals 1 HP every 600 ticks, every 20 in the rain.
func TestHappyGhastHeals(t *testing.T) {
	w, g, _ := adultGhast(t, mgl64.Vec3{0.5, 1, 0.5})
	g.With(func(m *mob.Mob) {
		m.Goals().ClearAll()
		m.Hurt(10, entity.VoidDamageSource{})
	})
	var start float64
	g.With(func(m *mob.Mob) { start = m.Health() })
	w.Tick(601)
	var after float64
	g.With(func(m *mob.Mob) { after = m.Health() })
	if after-start != 1 {
		t.Errorf("healed %.1f HP in 601 ticks without rain, want 1", after-start)
	}
	w.Do(func(tx *world.Tx) {
		for x := -3; x <= 3; x++ {
			for z := -3; z <= 3; z++ {
				for y := 0; y <= 8; y++ {
					tx.SetBiome(cube.Pos{x, y, z}, biome.Plains{})
				}
			}
		}
		tx.World().StartRaining(time.Minute)
	})
	w.Tick(200)
	g.With(func(m *mob.Mob) {
		if got := m.Health() - after; got < 9 {
			t.Errorf("healed %.1f HP in 200 ticks of rain, want 10", got)
		}
	})
}
