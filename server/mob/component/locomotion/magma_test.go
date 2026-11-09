//go:build crossfly_full

package locomotion_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	_ "github.com/df-mc/dragonfly/server/mob/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// maxRise is how far above its start r gets within n ticks.
func maxRise(w *mobtest.World, r *mobtest.Ref, n int) float64 {
	start := r.Pos()[1]
	best := 0.0
	w.Each(n, func(tx *world.Tx, _ int) bool {
		best = max(best, r.PosTx(tx)[1]-start)
		return true
	})
	return best
}

// TestMagmaCubeJumpHeight: a large magma cube jumps 0.42 + 0.1 x 4 (peak
// about 4.2 blocks up), a small one 0.52 (about 1.8), and lava does not slow it
// (ImmuneToLavaDragFlag): it jumps as high from a lava pool's floor.
func TestMagmaCubeJumpHeight(t *testing.T) {
	mobtest.RequireDefinitions(t)
	rise := func(event string, lava bool) float64 {
		w := mobtest.New(t, mobtest.Options{})
		w.Do(func(tx *world.Tx) {
			w.Arena(tx, 8, 6)
			if lava {
				w.Fill(tx, cube.Pos{-8, 1, -8}, cube.Pos{8, 3, 8}, block.Lava{Still: true, Depth: 8})
			}
		})
		c := w.Spawn("minecraft:magma_cube", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: event})
		return maxRise(w, c, 200)
	}
	if r := rise("spawn_large", false); r < 3.5 {
		t.Errorf("large magma cube rose %.2f blocks, want about 4.2", r)
	}
	if r := rise("spawn_small", false); r < 1.5 || r > 2.1 {
		t.Errorf("small magma cube rose %.2f blocks, want about 1.8", r)
	}
	if r := rise("spawn_large", true); r < 3.5 {
		t.Errorf("large magma cube in lava rose %.2f blocks, want about 4.2", r)
	}
}
