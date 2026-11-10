//go:build crossfly_full

package jnether

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// TestManhattanOrdered: Java's order for the first positions, every
// position of the box once.
func TestManhattanOrdered(t *testing.T) {
	var got []cube.Pos
	ManhattanOrdered(cube.Pos{}, 1, 1, 1, 3, func(p cube.Pos) bool { got = append(got, p); return true })
	want := []cube.Pos{{0, 0, 0}, {-1, 0, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}, {0, 1, 0}, {1, 0, 0}}
	for i, p := range want {
		if got[i] != p {
			t.Fatalf("position %d: %v, want %v (all %v)", i, got[i], p, got[:8])
		}
	}
	seen := map[cube.Pos]bool{}
	for _, p := range got {
		if seen[p] {
			t.Fatalf("%v twice", p)
		}
		seen[p] = true
	}
	if len(got) != 27 {
		t.Errorf("%d positions, want 27", len(got))
	}
}

// TestRepellents: a portal block repels hoglins within 8 x 4; a soul torch
// repels piglins, a normal torch does not.
func TestRepellents(t *testing.T) {
	w := newTestWorld(t, world.Overworld, world.DifficultyNormal)
	opts := &world.SetOpts{DisableBlockUpdates: true}
	w.do(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{5, 10, 2}, block.Portal{Axis: cube.X}, opts)
		tx.SetBlock(cube.Pos{-3, 11, 0}, block.Torch{Facing: cube.FaceDown, Type: block.NormalFire()}, opts)
		if p, ok := NearestHoglinRepellent(tx, cube.Pos{0, 10, 0}); !ok || p != (cube.Pos{5, 10, 2}) {
			t.Errorf("hoglin repellent %v %v", p, ok)
		}
		if _, ok := NearestHoglinRepellent(tx, cube.Pos{0, 15, 0}); ok {
			t.Error("a portal 5 below counts (vertical range 4)")
		}
		if _, ok := NearestPiglinRepellent(tx, cube.Pos{0, 10, 0}); ok {
			t.Error("a normal torch repels piglins")
		}
		tx.SetBlock(cube.Pos{-3, 11, 0}, block.Torch{Facing: cube.FaceDown, Type: block.SoulFire()}, opts)
		if p, ok := NearestPiglinRepellent(tx, cube.Pos{0, 10, 0}); !ok || p != (cube.Pos{-3, 11, 0}) {
			t.Errorf("piglin repellent %v %v", p, ok)
		}
	})
}
