//go:build crossfly_full

package misc_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob/goal/misc"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/go-gl/mathgl/mgl64"
)

// TestTrailPositions: TrailSystem::_tickComponent's four corner blocks and
// its spawn_offset turned by the facing.
func TestTrailPositions(t *testing.T) {
	got := misc.TrailPositions(mgl64.Vec3{0.5, 1, 0.5}, 0, mgl64.Vec3{})
	for _, p := range got {
		if p != (cube.Pos{0, 1, 0}) {
			t.Errorf("centred mob: %v, want all at 0 1 0", got)
		}
	}
	// On a block edge the corners cover the four blocks around it.
	got = misc.TrailPositions(mgl64.Vec3{1, 1.4, 1}, 0, mgl64.Vec3{})
	want := [4]cube.Pos{{0, 1, 0}, {1, 1, 0}, {0, 1, 1}, {1, 1, 1}}
	if got != want {
		t.Errorf("edge: %v, want %v", got, want)
	}
	// Offset z = 2: two blocks ahead, whichever way the mob faces.
	for _, c := range []struct {
		yaw  float64
		want cube.Pos
	}{{0, cube.Pos{0, 1, 2}}, {180, cube.Pos{0, 1, -2}}, {90, cube.Pos{-2, 1, 0}}, {-90, cube.Pos{2, 1, 0}}} {
		if got := misc.TrailPositions(mgl64.Vec3{0.5, 1, 0.5}, c.yaw, mgl64.Vec3{0, 0, 2}); got[0] != c.want {
			t.Errorf("yaw %v: %v, want %v", c.yaw, got[0], c.want)
		}
	}
}

func snowCount(w *mobtest.World, r int) int {
	n := 0
	w.Do(func(tx *world.Tx) {
		for p := range cube.Range3D(cube.Pos{-r, 1, -r}, cube.Pos{r, 1, r}) {
			if _, ok := tx.Block(p).(block.SnowLayer); ok {
				n++
			}
		}
	})
	return n
}

// TestSnowGolemTrail: a snow golem walking about leaves snow layers in a
// plains biome (0.8 < 0.81) and none in a desert.
func TestSnowGolemTrail(t *testing.T) {
	for _, c := range []struct {
		biome world.Biome
		snow  bool
	}{{biome.Plains{}, true}, {biome.Desert{}, false}} {
		w := arena(t, 10)
		w.Do(func(tx *world.Tx) {
			for p := range cube.Range3D(cube.Pos{-11, 0, -11}, cube.Pos{11, 3, 11}) {
				tx.SetBiome(p, c.biome)
			}
		})
		w.Spawn("minecraft:snow_golem", mgl64.Vec3{0.5, 1, 0.5})
		w.Tick(400)
		if n := snowCount(w, 9); (n > 0) != c.snow {
			t.Errorf("%v: %d snow layers", c.biome.String(), n)
		}

	}
}

// TestTrailSkipsUnfitBlocks: no snow where a layer may not stand (on ice).
func TestTrailSkipsUnfitBlocks(t *testing.T) {
	w := arena(t, 6)
	w.Do(func(tx *world.Tx) {
		w.Fill(tx, cube.Pos{-6, 0, -6}, cube.Pos{6, 0, 6}, block.Ice{})
	})
	w.Spawn("minecraft:snow_golem", mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(200)
	if n := snowCount(w, 5); n != 0 {
		t.Errorf("%d snow layers on ice", n)
	}
}
