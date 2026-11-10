//go:build crossfly_full

package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// dfpWorld makes a world with redstone = "dragonfly" for one test; the test binary runs Java redstone otherwise.
func dfpWorld(t *testing.T) *world.World {
	t.Helper()
	world.SetJavaRedstone(false)
	w := world.Config{Dim: world.End, Synchronous: true}.New()
	t.Cleanup(func() {
		_ = w.Close()
		world.SetJavaRedstone(true)
	})
	return w
}

func dfpTicks(w *world.World, n int) {
	for range n {
		w.AdvanceTick()
	}
}

func dfpBlockAt[T world.Block](t *testing.T, w *world.World, pos cube.Pos) (b T, ok bool) {
	t.Helper()
	runWorld(w, func(tx *world.Tx) { b, ok = tx.Block(pos).(T) })
	return
}

func dfpPiston(t *testing.T, w *world.World, pos cube.Pos) Piston {
	t.Helper()
	p, ok := dfpBlockAt[Piston](t, w, pos)
	if !ok {
		t.Fatalf("no piston at %v", pos)
	}
	return p
}

func TestDragonflyPistonPushAndPull(t *testing.T) {
	for _, sticky := range []bool{false, true} {
		w := dfpWorld(t)
		pos := cube.Pos{0, 64, 0}
		runWorld(w, func(tx *world.Tx) {
			tx.SetBlock(cube.Pos{1, 64, 0}, Stone{}, nil)
			tx.SetBlock(pos, Piston{Facing: cube.FaceEast, Sticky: sticky}, nil)
		})
		dfpTicks(w, 2)
		runWorld(w, func(tx *world.Tx) { tx.SetBlock(pos.Side(cube.FaceWest), RedstoneBlock{}, nil) })
		// The redstone engine sees the power at the end of this tick, the piston starts on the next.
		dfpTicks(w, 2)
		if p := dfpPiston(t, w, pos); p.State != 1 || p.Progress != .5 {
			t.Fatalf("sticky=%t: piston should be half out one tick after power: state %d progress %v", sticky, p.State, p.Progress)
		}
		if _, ok := dfpBlockAt[MovingPiston](t, w, cube.Pos{2, 64, 0}); !ok {
			t.Fatalf("sticky=%t: stone is not moving", sticky)
		}
		dfpTicks(w, 1)
		if p := dfpPiston(t, w, pos); p.State != 1 || p.Progress != 1 {
			t.Fatalf("sticky=%t: piston should be all out: state %d progress %v", sticky, p.State, p.Progress)
		}
		dfpTicks(w, 1)
		if p := dfpPiston(t, w, pos); p.State != 2 || !p.Extended {
			t.Fatalf("sticky=%t: piston should have landed its blocks: state %d", sticky, p.State)
		}
		if _, ok := dfpBlockAt[Stone](t, w, cube.Pos{2, 64, 0}); !ok {
			t.Fatalf("sticky=%t: stone not pushed", sticky)
		}
		if _, ok := dfpBlockAt[PistonHead](t, w, cube.Pos{1, 64, 0}); !ok {
			t.Fatalf("sticky=%t: no arm", sticky)
		}
		runWorld(w, func(tx *world.Tx) { tx.SetBlock(pos.Side(cube.FaceWest), nil, nil) })
		dfpTicks(w, 5)
		if p := dfpPiston(t, w, pos); p.State != 0 || p.Extended {
			t.Fatalf("sticky=%t: piston did not retract: state %d", sticky, p.State)
		}
		want := cube.Pos{2, 64, 0}
		if sticky {
			want = cube.Pos{1, 64, 0}
		}
		if _, ok := dfpBlockAt[Stone](t, w, want); !ok {
			t.Errorf("sticky=%t: stone should be at %v", sticky, want)
		}
		if _, ok := dfpBlockAt[Air](t, w, cube.Pos{1, 64, 0}); !ok && !sticky {
			t.Errorf("arm left behind")
		}
	}
}

func TestDragonflyPistonPushLimit(t *testing.T) {
	for _, count := range []int{12, 13} {
		w := dfpWorld(t)
		pos := cube.Pos{0, 64, 0}
		runWorld(w, func(tx *world.Tx) {
			for i := 1; i <= count; i++ {
				tx.SetBlock(cube.Pos{i, 64, 0}, Stone{}, nil)
			}
			_, ok := dfResolvePiston(tx, pos, cube.FaceEast, true)
			if ok != (count <= 12) {
				t.Errorf("%d blocks: movable %t", count, ok)
			}
		})
	}
}

// TestDragonflyPistonNoQuasiConnectivity: power next to the block above a piston does nothing, nor does power in
// front of it.
func TestDragonflyPistonNoQuasiConnectivity(t *testing.T) {
	w := dfpWorld(t)
	pos := cube.Pos{0, 64, 0}
	other := cube.Pos{0, 64, 5}
	runWorld(w, func(tx *world.Tx) {
		tx.SetBlock(pos, Piston{Facing: cube.FaceEast}, nil)
		tx.SetBlock(cube.Pos{0, 66, 0}, RedstoneBlock{}, nil)
		tx.SetBlock(other, Piston{Facing: cube.FaceEast}, nil)
		// In front: the piston would push it if it powered it.
		tx.SetBlock(other.Side(cube.FaceEast), RedstoneBlock{}, nil)
	})
	dfpTicks(w, 6)
	if p := dfpPiston(t, w, pos); p.State != 0 {
		t.Error("piston powered through the block above it (quasi-connectivity)")
	}
	if p := dfpPiston(t, w, other); p.State != 0 {
		t.Error("piston powered from its front")
	}
	runWorld(w, func(tx *world.Tx) { tx.SetBlock(cube.Pos{0, 65, 0}, RedstoneBlock{}, nil) })
	dfpTicks(w, 6)
	if p := dfpPiston(t, w, pos); p.State != 2 {
		t.Errorf("piston with a redstone block on top did not extend: state %d", p.State)
	}
}

// TestDragonflyPistonMovesBlockEntities: Bedrock moves containers with their contents.
func TestDragonflyPistonMovesBlockEntities(t *testing.T) {
	w := dfpWorld(t)
	pos := cube.Pos{0, 64, 0}
	runWorld(w, func(tx *world.Tx) {
		c := NewChest()
		_ = c.inventory.SetItem(3, item.NewStack(Stone{}, 17))
		tx.SetBlock(cube.Pos{1, 64, 0}, c, nil)
		d := NewDispenser()
		_ = d.inventory.SetItem(0, item.NewStack(TNT{}, 9))
		tx.SetBlock(cube.Pos{2, 64, 0}, d, nil)
		tx.SetBlock(pos, Piston{Facing: cube.FaceEast, Sticky: true}, nil)
		tx.SetBlock(pos.Side(cube.FaceDown), RedstoneBlock{}, nil)
	})
	dfpTicks(w, 6)
	c, ok := dfpBlockAt[Chest](t, w, cube.Pos{2, 64, 0})
	if !ok {
		t.Fatal("chest not pushed")
	}
	if s, _ := c.inventory.Item(3); s.Count() != 17 {
		t.Errorf("chest lost its contents: %v", s)
	}
	d, ok := dfpBlockAt[Dispenser](t, w, cube.Pos{3, 64, 0})
	if !ok {
		t.Fatal("dispenser not pushed")
	}
	if s, _ := d.inventory.Item(0); s.Count() != 9 {
		t.Errorf("dispenser lost its contents: %v", s)
	}
	runWorld(w, func(tx *world.Tx) { tx.SetBlock(pos.Side(cube.FaceDown), nil, nil) })
	dfpTicks(w, 6)
	c, ok = dfpBlockAt[Chest](t, w, cube.Pos{1, 64, 0})
	if !ok {
		t.Fatal("chest not pulled back")
	}
	if s, _ := c.inventory.Item(3); s.Count() != 17 {
		t.Errorf("pulled chest lost its contents: %v", s)
	}
}

func TestDragonflyPistonRules(t *testing.T) {
	w := dfpWorld(t)
	pos := cube.Pos{0, 64, 0}
	runWorld(w, func(tx *world.Tx) {
		// Obsidian stops the push.
		tx.SetBlock(cube.Pos{1, 64, 0}, Obsidian{}, nil)
		if _, ok := dfResolvePiston(tx, pos, cube.FaceEast, true); ok {
			t.Error("obsidian moved")
		}
		// A pumpkin breaks.
		tx.SetBlock(cube.Pos{1, 64, 0}, Stone{}, nil)
		tx.SetBlock(cube.Pos{2, 64, 0}, Pumpkin{}, nil)
		r, ok := dfResolvePiston(tx, pos, cube.FaceEast, true)
		if !ok || len(r.attached) != 1 || len(r.breaks) != 1 || r.breaks[0] != (cube.Pos{2, 64, 0}) {
			t.Errorf("pumpkin should break: %v %v %v", ok, r.attached, r.breaks)
		}
		tx.SetBlock(cube.Pos{2, 64, 0}, nil, nil)

		// A slime block drags the stone on it, but not the honey beside it, and leaves the obsidian.
		honey, ok := world.BlockByName("minecraft:honey_block", nil)
		if !ok {
			t.Fatal("no honey block")
		}
		tx.SetBlock(cube.Pos{1, 64, 0}, Slime{}, nil)
		tx.SetBlock(cube.Pos{1, 65, 0}, Stone{}, nil)
		tx.SetBlock(cube.Pos{1, 64, 1}, honey, nil)
		tx.SetBlock(cube.Pos{1, 64, -1}, Obsidian{}, nil)
		r, ok = dfResolvePiston(tx, pos, cube.FaceEast, true)
		if !ok || len(r.attached) != 2 {
			t.Errorf("slime should carry only the stone: %v %v", ok, r.attached)
		}
		// Something immovable in front of a dragged block stops it all.
		tx.SetBlock(cube.Pos{2, 65, 0}, Obsidian{}, nil)
		if _, ok := dfResolvePiston(tx, pos, cube.FaceEast, true); ok {
			t.Error("a blocked dragged block did not stop the move")
		}
		tx.SetBlock(cube.Pos{2, 65, 0}, nil, nil)
		// Honey in the direction of the move is pushed by the slime.
		tx.SetBlock(cube.Pos{2, 64, 0}, honey, nil)
		r, ok = dfResolvePiston(tx, pos, cube.FaceEast, true)
		if !ok || len(r.attached) != 3 {
			t.Errorf("slime should push the honey in front: %v %v", ok, r.attached)
		}

		// Glazed terracotta is pushed but a sticky piston does not pull it.
		gt, ok := world.BlockByName("minecraft:red_glazed_terracotta", map[string]any{"facing_direction": int32(2)})
		if ok {
			q := cube.Pos{0, 70, 0}
			tx.SetBlock(cube.Pos{1, 70, 0}, gt, nil)
			if r, ok := dfResolvePiston(tx, q, cube.FaceEast, true); !ok || len(r.attached) != 1 {
				t.Error("glazed terracotta not pushed")
			}
			tx.SetBlock(cube.Pos{1, 70, 0}, nil, nil)
			tx.SetBlock(cube.Pos{2, 70, 0}, gt, nil)
			if r, ok := dfResolvePiston(tx, q, cube.FaceEast, false); !ok || len(r.attached) != 0 {
				t.Error("glazed terracotta pulled")
			}
		}
	})
}

// TestDragonflyStickyPistonShortPulse: a one-tick pulse extends a sticky piston all the way, and it then pulls its
// block back instead of dropping it.
func TestDragonflyStickyPistonShortPulse(t *testing.T) {
	w := dfpWorld(t)
	pos := cube.Pos{0, 64, 0}
	runWorld(w, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{1, 64, 0}, Stone{}, nil)
		tx.SetBlock(pos, Piston{Facing: cube.FaceEast, Sticky: true}, nil)
	})
	dfpTicks(w, 1)
	runWorld(w, func(tx *world.Tx) { tx.SetBlock(pos.Side(cube.FaceWest), RedstoneBlock{}, nil) })
	dfpTicks(w, 1)
	runWorld(w, func(tx *world.Tx) { tx.SetBlock(pos.Side(cube.FaceWest), nil, nil) })
	dfpTicks(w, 12)
	if p := dfpPiston(t, w, pos); p.State != 0 {
		t.Fatalf("piston should be back: state %d", p.State)
	}
	if _, ok := dfpBlockAt[Stone](t, w, cube.Pos{1, 64, 0}); !ok {
		t.Error("stone was not pulled back")
	}
	if _, ok := dfpBlockAt[Air](t, w, cube.Pos{2, 64, 0}); !ok {
		t.Error("stone was dropped")
	}
}

// TestDragonflyPistonRetriesWhenBlocked: a powered piston that cannot push extends once the way is free.
func TestDragonflyPistonRetriesWhenBlocked(t *testing.T) {
	w := dfpWorld(t)
	pos := cube.Pos{0, 64, 0}
	runWorld(w, func(tx *world.Tx) {
		for i := 1; i <= 13; i++ {
			tx.SetBlock(cube.Pos{i, 64, 0}, Stone{}, nil)
		}
		tx.SetBlock(pos, Piston{Facing: cube.FaceEast}, nil)
		tx.SetBlock(pos.Side(cube.FaceWest), RedstoneBlock{}, nil)
	})
	dfpTicks(w, 6)
	if p := dfpPiston(t, w, pos); p.State != 0 {
		t.Fatal("piston pushed 13 blocks")
	}
	runWorld(w, func(tx *world.Tx) { tx.SetBlock(cube.Pos{13, 64, 0}, nil, nil) })
	dfpTicks(w, 6)
	if p := dfpPiston(t, w, pos); p.State != 2 {
		t.Error("piston did not extend once the 13th block was gone")
	}
}
