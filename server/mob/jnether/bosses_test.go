//go:build crossfly_full

package jnether

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob/component/boss"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestJavaWitherSummon: the skull pattern summons a Java wither with a
// third of its health that charges for 220 ticks to full health.
func TestJavaWitherSummon(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{Difficulty: world.DifficultyNormal})
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 12, block.Stone{})
		opts := &world.SetOpts{DisableBlockUpdates: true}
		tx.SetBlock(cube.Pos{0, 1, 0}, block.SoulSand{}, opts)
		for x := -1; x <= 1; x++ {
			tx.SetBlock(cube.Pos{x, 2, 0}, block.SoulSand{}, opts)
			tx.SetBlock(cube.Pos{x, 3, 0}, block.Skull{Type: block.WitherSkeletonSkull()}, opts)
		}
		if !TrySummonWither(tx, cube.Pos{0, 3, 0}, SummonHooks{SpawnWither: spawnWither}) {
			t.Fatal("no wither")
		}
	})
	var ref *mobtest.Ref
	w.Do(func(tx *world.Tx) {
		for e := range tx.EntitiesWithin(cube.Box(-3, 0, -3, 3, 6, 3)) {
			if m, ok := javamob.Of(e); ok && m.JavaType().ID == WitherID {
				ref = w.Track(m.H())
				if m.Health() != 100 {
					t.Errorf("health %v, want 100", m.Health())
				}
			}
		}
	})
	if ref == nil {
		t.Fatal("no Java wither in the world")
	}
	w.Tick(225)
	withJ(t, w, ref, func(m *javamob.Mob) {
		b := m.Behaviour().(*witherMob)
		if b.w.InvulnerableTicks() != 0 {
			t.Errorf("still charging: %d", b.w.InvulnerableTicks())
		}
		if m.Health() < 299 {
			t.Errorf("health %v after the charge, want 300", m.Health())
		}
	})
}

// TestJavaDragonFlies: a Java dragon in the holding pattern flies (no
// gravity, its own steering) and keeps its height.
func TestJavaDragonFlies(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 60, 4, block.EndStone{}) })
	var ref *mobtest.Ref
	w.Do(func(tx *world.Tx) {
		e := javaDragonHooks{}.Spawn(tx, mgl64.Vec3{0, 100, 0}, 0)
		if e == nil {
			t.Fatal("no dragon")
		}
		ref = w.Track(e.H())
	})
	var start mgl64.Vec3
	withJ(t, w, ref, func(m *javamob.Mob) { start = m.Position() })
	w.Tick(100)
	withJ(t, w, ref, func(m *javamob.Mob) {
		p := m.Position()
		if p.Sub(start).Len() < 5 {
			t.Errorf("the dragon barely moved: %v -> %v", start, p)
		}
		if p[1] < 70 {
			t.Errorf("the dragon fell to y %v", p[1])
		}
		if ph := m.Behaviour().(*dragonMob).d.Phase(); ph != PhaseHoldingPattern && ph != PhaseLandingApproach && ph != PhaseLanding {
			t.Logf("phase %d", ph)
		}
	})
}

// TestEndFightJavaDragon: with Java mob mode installed the End fight makes
// a Java dragon.
func TestEndFightJavaDragon(t *testing.T) {
	Install()
	t.Cleanup(func() { boss.JavaDragon = nil; portalHooks = PortalSpawnHooks{} })
	old := boss.EndFightWorld
	boss.EndFightWorld = func(*world.World) bool { return true }
	t.Cleanup(func() { boss.EndFightWorld = old })
	boss.EnableEndFight()
	w := mobtest.New(t, mobtest.Options{Dim: world.End})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 60, 6, block.EndStone{}) })
	w.AddPlayer("tester", mgl64.Vec3{0.5, 61, 3.5})
	// The chunks around the centre are loaded, as the player's view would.
	w.Do(func(tx *world.Tx) {
		for x := -1; x <= 1; x++ {
			for z := -1; z <= 1; z++ {
				tx.Block(cube.Pos{x * 16, 0, z * 16})
			}
		}
	})
	found := false
	for i := 0; i < 40 && !found; i++ {
		w.Tick(1)
		w.Do(func(tx *world.Tx) {
			for e := range tx.EntitiesWithin(cube.Box(-200, -64, -200, 200, 300, 200)) {
				if m, ok := javamob.Of(e); ok && m.JavaType().ID == DragonID {
					found = true
				}
			}
		})
	}
	if !found {
		t.Fatal("the End fight made no Java dragon")
	}
}
