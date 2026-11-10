//go:build crossfly_full

package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// dfPistonWorld makes a world with redstone = "dragonfly" for one test.
func dfPistonWorld(t *testing.T) *world.World {
	t.Helper()
	world.SetJavaRedstone(false)
	w := world.Config{Dim: world.End, Synchronous: true, Entities: DefaultRegistry}.New()
	t.Cleanup(func() {
		_ = w.Close()
		world.SetJavaRedstone(true)
	})
	return w
}

func dfItemAt(t *testing.T, w *world.World, h *world.EntityHandle) (pos, vel mgl64.Vec3) {
	t.Helper()
	doWorld(w, func(tx *world.Tx) {
		e, ok := h.Entity(tx)
		if !ok {
			t.Fatal("item gone")
		}
		pos, vel = e.Position(), e.(*Ent).Velocity()
	})
	return
}

// TestDragonflyPistonSlimeLaunches: a slime block pushed up launches what stands on it at a block a tick, as on
// Bedrock.
func TestDragonflyPistonSlimeLaunches(t *testing.T) {
	w := dfPistonWorld(t)
	base := cube.Pos{0, 64, 0}
	h := NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 66, .5}}, item.NewStack(block.Stone{}, 1))
	doWorld(w, func(tx *world.Tx) {
		tx.SetBlock(base, block.Piston{Facing: cube.FaceUp, Sticky: true}, nil)
		tx.SetBlock(base.Side(cube.FaceUp), block.Slime{}, nil)
		tx.AddEntity(h)
	})
	for range 20 {
		w.AdvanceTick()
	}
	start, _ := dfItemAt(t, w, h)
	if start[1] > 66.01 || start[1] < 65.99 {
		t.Fatalf("item should rest on the slime block at y 66: %v", start)
	}
	doWorld(w, func(tx *world.Tx) { tx.SetBlock(base.Side(cube.FaceDown), block.RedstoneBlock{}, nil) })
	w.AdvanceTick()
	w.AdvanceTick()
	pos, vel := dfItemAt(t, w, h)
	if pos[1] < 66.4 {
		t.Errorf("item not pushed up by the slime: %v", pos)
	}
	if vel[1] < .5 {
		t.Errorf("item not launched: velocity %v", vel)
	}
	for range 4 {
		w.AdvanceTick()
	}
	if pos, _ := dfItemAt(t, w, h); pos[1] < 69 {
		t.Errorf("launched item should be well above the slime block: %v", pos)
	}
}

// TestDragonflyPistonCarriesOnTop: what stands on a block a piston moves sideways goes along with it.
func TestDragonflyPistonCarriesOnTop(t *testing.T) {
	w := dfPistonWorld(t)
	base := cube.Pos{0, 64, 0}
	h := NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{1.5, 65, .5}}, item.NewStack(block.Stone{}, 1))
	doWorld(w, func(tx *world.Tx) {
		for x := -1; x <= 4; x++ {
			tx.SetBlock(cube.Pos{x, 63, 1}, block.Stone{}, nil)
		}
		tx.SetBlock(base, block.Piston{Facing: cube.FaceEast}, nil)
		tx.SetBlock(cube.Pos{1, 64, 0}, block.Stone{}, nil)
		tx.AddEntity(h)
	})
	for range 10 {
		w.AdvanceTick()
	}
	start, _ := dfItemAt(t, w, h)
	doWorld(w, func(tx *world.Tx) { tx.SetBlock(base.Side(cube.FaceWest), block.RedstoneBlock{}, nil) })
	for range 5 {
		w.AdvanceTick()
	}
	pos, _ := dfItemAt(t, w, h)
	if pos[0]-start[0] < .9 {
		t.Errorf("item on the pushed block did not move along: %v -> %v", start, pos)
	}
}

// TestDragonflyPistonTNTBomber: a Bedrock-style TNT bomber, a flying machine carrying a dispenser full of TNT that
// an observer on the front half fires each time the machine moves (the observer fires when a piston moves it).
func TestDragonflyPistonTNTBomber(t *testing.T) {
	if _, ok := any(block.Observer{}).(interface{ MovedByPiston(cube.Pos, *world.Tx) }); !ok {
		t.Skip("observers do not fire when moved with redstone = \"dragonfly\" yet (diodes and sensors part)")
	}
	w := dfPistonWorld(t)
	honey, _ := world.BlockByName("minecraft:honey_block", nil)
	y := 64
	disp := cube.Pos{2, y - 1, 0}
	doWorld(w, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, y, 0}, block.Observer{Facing: cube.FaceWest}, nil)
		tx.SetBlock(cube.Pos{0, y + 1, 0}, honey, nil)
		tx.SetBlock(cube.Pos{1, y, 0}, block.Piston{Facing: cube.FaceEast}, nil)
		tx.SetBlock(cube.Pos{1, y + 1, 0}, honey, nil)
		tx.SetBlock(cube.Pos{2, y, 0}, block.Slime{}, nil)
		tx.SetBlock(cube.Pos{2, y + 1, 0}, block.Piston{Facing: cube.FaceWest, Sticky: true}, nil)
		tx.SetBlock(cube.Pos{3, y, 0}, block.Slime{}, nil)
		tx.SetBlock(cube.Pos{3, y + 1, 0}, block.Observer{Facing: cube.FaceEast}, nil)
		d := block.NewDispenser()
		d.Facing = cube.FaceDown
		_ = d.Inventory(tx, disp).SetItem(0, item.NewStack(block.TNT{}, 64))
		tx.SetBlock(disp, d, nil)
		tx.SetBlock(cube.Pos{3, y - 1, 0}, block.Observer{Facing: cube.FaceEast}, nil)
	})
	for range 2 {
		w.AdvanceTick()
	}
	doWorld(w, func(tx *world.Tx) { tx.SetBlock(cube.Pos{-1, y, 0}, block.Stone{}, nil) })
	for range 60 {
		w.AdvanceTick()
	}
	var xs []float64
	left := -1
	doWorld(w, func(tx *world.Tx) {
		for e := range tx.Entities() {
			if e.H().Type().EncodeEntity() == "minecraft:tnt" {
				xs = append(xs, e.Position()[0])
			}
		}
		for x := 2; x < 60; x++ {
			if d, ok := tx.Block(cube.Pos{x, y - 1, 0}).(block.Dispenser); ok {
				s, _ := d.Inventory(tx, cube.Pos{x, y - 1, 0}).Item(0)
				left = s.Count()
			}
		}
	})
	if left < 0 {
		t.Fatal("the dispenser is gone")
	}
	if len(xs) < 3 || 64-left < 3 {
		t.Fatalf("bomber dropped %d TNT (%d left in the dispenser)", len(xs), left)
	}
	lo, hi := xs[0], xs[0]
	for _, x := range xs {
		lo, hi = min(lo, x), max(hi, x)
	}
	if hi-lo < 2 {
		t.Errorf("the TNT was not dropped along the way: x from %v to %v", lo, hi)
	}
}
