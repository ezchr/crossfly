//go:build crossfly_full

package jnether

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// buildWitherT puts a soul sand T at base (stem) with three wither skulls,
// lying along x (alongX) or z.
func buildWitherT(w *testWorld, base cube.Pos, alongX bool) {
	side := cube.Pos{1, 0, 0}
	if !alongX {
		side = cube.Pos{0, 0, 1}
	}
	opts := &world.SetOpts{DisableBlockUpdates: true}
	w.do(func(tx *world.Tx) {
		tx.SetBlock(base, block.SoulSand{}, opts)
		mid := base.Add(cube.Pos{0, 1, 0})
		for i := -1; i <= 1; i++ {
			p := mid.Add(cube.Pos{side[0] * i, 0, side[2] * i})
			tx.SetBlock(p, block.SoulSand{}, opts)
			tx.SetBlock(p.Add(cube.Pos{0, 1, 0}), block.Skull{Type: block.WitherSkeletonSkull()}, opts)
		}
	})
}

// TestWitherSummon: the wither appears in the stem's block (0.5, 0.55,
// 0.5), yaw 0 for a pattern facing along x, 90 along z; the nine blocks are
// gone.
func TestWitherSummon(t *testing.T) {
	for _, c := range []struct {
		alongX bool
		yaw    float64
	}{{true, 90}, {false, 0}} {
		w := newTestWorld(t, world.Overworld, world.DifficultyNormal)
		base := cube.Pos{10, 5, 10}
		buildWitherT(w, base, c.alongX)
		var at mgl64.Vec3
		var yaw float64
		n := 0
		hooks := SummonHooks{SpawnWither: func(_ *world.Tx, pos mgl64.Vec3, y float64) bool {
			n++
			at, yaw = pos, y
			return true
		}}
		w.do(func(tx *world.Tx) {
			// The last skull placed: the middle one.
			if !TrySummonWither(tx, base.Add(cube.Pos{0, 2, 0}), hooks) {
				t.Fatal("no wither")
			}
			for y := 5; y <= 7; y++ {
				if _, ok := tx.Block(cube.Pos{10, y, 10}).(block.Air); !ok {
					t.Errorf("block at y %d stays", y)
				}
			}
		})
		if want := (mgl64.Vec3{10.5, 5.55, 10.5}); at.Sub(want).Len() > 1e-9 {
			t.Errorf("along x %v: wither at %v, want %v", c.alongX, at, want)
		}
		// The forwards direction of the match is across the T: a T lying
		// along x is found facing north/south (yaw 90).
		if yaw != c.yaw {
			t.Errorf("along x %v: yaw %v, want %v", c.alongX, yaw, c.yaw)
		}
		if n != 1 {
			t.Errorf("%d withers", n)
		}
	}
}

// TestWitherSummonRules: nothing on peaceful, nothing without air in the
// lower corners.
func TestWitherSummonRules(t *testing.T) {
	hooks := SummonHooks{SpawnWither: func(*world.Tx, mgl64.Vec3, float64) bool { return true }}
	w := newTestWorld(t, world.Overworld, world.DifficultyPeaceful)
	buildWitherT(w, cube.Pos{0, 5, 0}, true)
	w.do(func(tx *world.Tx) {
		if TrySummonWither(tx, cube.Pos{0, 7, 0}, hooks) {
			t.Error("wither on peaceful")
		}
	})
	w = newTestWorld(t, world.Overworld, world.DifficultyHard)
	buildWitherT(w, cube.Pos{0, 5, 0}, true)
	w.fill(cube.Pos{1, 5, 0}, cube.Pos{1, 5, 0}, block.Dirt{})
	w.do(func(tx *world.Tx) {
		if TrySummonWither(tx, cube.Pos{0, 7, 0}, hooks) {
			t.Error("wither with a filled corner")
		}
	})
}
