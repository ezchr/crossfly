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
