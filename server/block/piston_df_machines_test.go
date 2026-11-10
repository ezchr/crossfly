//go:build crossfly_full

package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// TestDragonflyPistonDoor: a 2x2 piston door, a sticky piston on each side of each row, closes when powered and
// opens again when the power goes.
func TestDragonflyPistonDoor(t *testing.T) {
	w := dfWorld(t)
	runWorld(w, func(tx *world.Tx) {
		for _, y := range []int{64, 65} {
			tx.SetBlock(cube.Pos{-1, y, 0}, Piston{Facing: cube.FaceEast, Sticky: true}, nil)
			tx.SetBlock(cube.Pos{0, y, 0}, Stone{}, nil)
			tx.SetBlock(cube.Pos{3, y, 0}, Stone{}, nil)
			tx.SetBlock(cube.Pos{4, y, 0}, Piston{Facing: cube.FaceWest, Sticky: true}, nil)
		}
	})
	dfTicks(w, 2)
	power := func(on bool) {
		runWorld(w, func(tx *world.Tx) {
			var b world.Block
			if on {
				b = RedstoneBlock{}
			}
			for _, y := range []int{64, 65} {
				tx.SetBlock(cube.Pos{-2, y, 0}, b, nil)
				tx.SetBlock(cube.Pos{5, y, 0}, b, nil)
			}
		})
	}
	power(true)
	dfTicks(w, 6)
	for _, y := range []int{64, 65} {
		for _, x := range []int{1, 2} {
			if _, ok := dfBlockAt[Stone](t, w, cube.Pos{x, y, 0}); !ok {
				t.Errorf("door not closed at %v", cube.Pos{x, y, 0})
			}
		}
	}
	power(false)
	dfTicks(w, 6)
	for _, y := range []int{64, 65} {
		for _, x := range []int{1, 2} {
			if _, ok := dfBlockAt[Air](t, w, cube.Pos{x, y, 0}); !ok {
				t.Errorf("door not open at %v", cube.Pos{x, y, 0})
			}
		}
		for _, x := range []int{0, 3} {
			if _, ok := dfBlockAt[Stone](t, w, cube.Pos{x, y, 0}); !ok {
				t.Errorf("door block not back in the wall at %v", cube.Pos{x, y, 0})
			}
		}
	}
}

// dfObserversWork reports whether observers pulse in dragonfly mode (they belong to the diodes and sensors part).
func dfObserversWork(t *testing.T) bool {
	t.Helper()
	w := dfWorld(t)
	pos := cube.Pos{0, 64, 0}
	runWorld(w, func(tx *world.Tx) { tx.SetBlock(pos, Observer{Facing: cube.FaceEast}, nil) })
	dfTicks(w, 2)
	runWorld(w, func(tx *world.Tx) { tx.SetBlock(pos.Side(cube.FaceEast), Stone{}, nil) })
	for range 6 {
		w.AdvanceTick()
		if o, ok := dfBlockAt[Observer](t, w, pos); ok && o.Powered {
			return true
		}
	}
	return false
}

// dfFlyingMachine builds a one-way flying machine going east with its back at x: a piston pushes the front half (two
// slime blocks, a sticky piston facing back and an observer), whose observer fires the sticky piston once it has
// moved, which pulls the back half (the piston, two honey blocks and an observer) after it; that observer fires the
// piston again. The halves touch only where honey meets a piston or slime meets nothing, so they never stick
// together. cargo, if set, is placed on top of the front slime block.
func dfFlyingMachine(tx *world.Tx, x, y int, cargo world.Block) {
	tx.SetBlock(cube.Pos{x, y, 0}, Observer{Facing: cube.FaceWest}, nil)
	tx.SetBlock(cube.Pos{x, y + 1, 0}, dfHoney(), nil)
	tx.SetBlock(cube.Pos{x + 1, y, 0}, Piston{Facing: cube.FaceEast}, nil)
	tx.SetBlock(cube.Pos{x + 1, y + 1, 0}, dfHoney(), nil)
	tx.SetBlock(cube.Pos{x + 2, y, 0}, Slime{}, nil)
	tx.SetBlock(cube.Pos{x + 2, y + 1, 0}, Piston{Facing: cube.FaceWest, Sticky: true}, nil)
	tx.SetBlock(cube.Pos{x + 3, y, 0}, Slime{}, nil)
	tx.SetBlock(cube.Pos{x + 3, y + 1, 0}, Observer{Facing: cube.FaceEast}, nil)
	if cargo != nil {
		tx.SetBlock(cube.Pos{x + 2, y - 1, 0}, cargo, nil)
	}
}

func dfHoney() world.Block {
	b, _ := world.BlockByName("minecraft:honey_block", nil)
	return b
}

// dfFindPiston returns the x of the first sticky piston facing west on row y, z 0, between x0 and x1.
func dfFindPiston(t *testing.T, w *world.World, y, x0, x1 int) int {
	t.Helper()
	found := x0 - 1
	runWorld(w, func(tx *world.Tx) {
		for x := x0; x <= x1; x++ {
			if p, ok := tx.Block(cube.Pos{x, y, 0}).(Piston); ok && p.Sticky {
				found = x
				return
			}
		}
	})
	return found
}

// TestDragonflyFlyingMachine: the flying machine keeps moving once started, carrying a dispenser full of TNT
// (the payload of a Bedrock TNT bomber) with its contents.
func TestDragonflyFlyingMachine(t *testing.T) {
	if !dfObserversWork(t) {
		t.Skip("observers do not pulse with redstone = \"dragonfly\" yet (diodes and sensors part)")
	}
	w := dfWorld(t)
	d := NewDispenser()
	_ = d.inventory.SetItem(0, item.NewStack(TNT{}, 64))
	runWorld(w, func(tx *world.Tx) { dfFlyingMachine(tx, 0, 64, d) })
	dfTicks(w, 2)
	// Start it: something appears in front of the back observer.
	runWorld(w, func(tx *world.Tx) { tx.SetBlock(cube.Pos{-1, 64, 0}, Stone{}, nil) })
	dfTicks(w, 200)
	x := dfFindPiston(t, w, 65, 0, 400)
	if x < 12 {
		t.Fatalf("flying machine did not keep moving: front piston at x %d", x)
	}
	cargo, ok := dfBlockAt[Dispenser](t, w, cube.Pos{x, 63, 0})
	if !ok {
		t.Fatalf("the dispenser did not come along")
	}
	if s, _ := cargo.inventory.Item(0); s.Count() != 64 {
		t.Errorf("the dispenser lost its TNT: %v", s)
	}
}
