//go:build crossfly_full

package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// tripwireCircuit builds stone, a hook facing east, three wires, a hook facing west and stone along x at y 64.
func tripwireCircuit(tx *world.Tx) {
	for x := 0; x <= 8; x++ {
		tx.SetBlock(cube.Pos{x, 63, 0}, Stone{}, nil)
	}
	tx.SetBlock(cube.Pos{1, 64, 0}, Stone{}, nil)
	tx.SetBlock(cube.Pos{7, 64, 0}, Stone{}, nil)
	tx.SetBlock(cube.Pos{2, 64, 0}, TripwireHook{Facing: cube.East}, nil)
	tx.SetBlock(cube.Pos{6, 64, 0}, TripwireHook{Facing: cube.West}, nil)
	for x := 3; x <= 5; x++ {
		tx.SetBlock(cube.Pos{x, 64, 0}, String{}, nil)
	}
}

func tripwireHooks(t *testing.T, tx *world.Tx, attached, powered bool, when string) {
	t.Helper()
	for _, x := range []int{2, 6} {
		h, ok := tx.Block(cube.Pos{x, 64, 0}).(TripwireHook)
		if !ok {
			t.Fatalf("%s: no hook at x %d", when, x)
		}
		if h.Attached != attached || h.Powered != powered {
			t.Errorf("%s: hook at x %d attached %v powered %v, want %v %v", when, x, h.Attached, h.Powered, attached, powered)
		}
	}
}

func TestTripwireCircuitAttachesAndTrips(t *testing.T) {
	w := world.Config{Dim: world.End, Synchronous: true}.New()
	var e *world.EntityHandle
	runWorld(w, func(tx *world.Tx) {
		tripwireCircuit(tx)
		tripwireHooks(t, tx, true, false, "built")
		if s := tx.Block(cube.Pos{4, 64, 0}).(String); !s.Attached {
			t.Error("wire not attached")
		}
		if !(String{}).Connects(cube.Pos{3, 64, 0}, cube.FaceWest, tx) {
			t.Error("wire does not connect to the hook facing it")
		}
		ent := tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{4.5, 64, 0.5}}.New(redstoneTNTTestEntityType{}, redstoneTNTTestEntityConfig{}))
		e = ent.H()
		at := cube.Pos{4, 64, 0}
		tx.Block(at).(String).EntityInside(at, tx, ent)
		tripwireHooks(t, tx, true, true, "entity in the wire")
		if p := tx.Block(cube.Pos{2, 64, 0}).(TripwireHook).RedstoneStrongPower(cube.Pos{2, 64, 0}, tx, cube.FaceWest); p != 15 {
			t.Errorf("hook strong power into its wall %d, want 15", p)
		}
	})
	// Still inside at the recheck: stays tripped.
	for range 12 {
		w.AdvanceTick()
	}
	runWorld(w, func(tx *world.Tx) {
		tripwireHooks(t, tx, true, true, "entity still in the wire")
		if ent, ok := e.Entity(tx); ok {
			_ = tx.RemoveEntity(ent)
		}
	})
	// Gone: the wire releases at its next recheck (at most 10 ticks), then the hooks follow.
	for range 12 {
		w.AdvanceTick()
	}
	runWorld(w, func(tx *world.Tx) {
		tripwireHooks(t, tx, true, false, "entity gone")
	})
}

func TestTripwireBreakingTripsAndShearsDisarm(t *testing.T) {
	for _, shears := range []bool{false, true} {
		w := world.Config{Dim: world.End, Synchronous: true}.New()
		runWorld(w, func(tx *world.Tx) {
			tripwireCircuit(tx)
			at := cube.Pos{4, 64, 0}
			if shears {
				tx.Block(at).(String).PlayerWillBreak(at, item.NewStack(item.Shears{}, 1), tx)
				if !tx.Block(at).(String).Disarmed {
					t.Fatal("shears did not disarm the wire")
				}
			}
			tx.SetBlock(at, nil, nil)
			if shears {
				tripwireHooks(t, tx, false, false, "cut with shears")
			} else {
				tripwireHooks(t, tx, true, true, "broken by hand")
			}
		})
		if shears {
			continue
		}
		for range 12 {
			w.AdvanceTick()
		}
		runWorld(w, func(tx *world.Tx) {
			tripwireHooks(t, tx, false, false, "after the recheck")
		})
	}
}

func TestTripwireHookRemovalDetachesOtherHook(t *testing.T) {
	w := world.Config{Dim: world.End, Synchronous: true}.New()
	runWorld(w, func(tx *world.Tx) {
		tripwireCircuit(tx)
		tx.SetBlock(cube.Pos{6, 64, 0}, nil, nil)
		h := tx.Block(cube.Pos{2, 64, 0}).(TripwireHook)
		if h.Attached || h.Powered {
			t.Errorf("remaining hook attached %v powered %v", h.Attached, h.Powered)
		}
		// As in Java Edition, the wires keep their attached state: the hook that is gone is not there to clear them.
		for x := 3; x <= 5; x++ {
			if !tx.Block(cube.Pos{x, 64, 0}).(String).Attached {
				t.Errorf("wire at x %d detached", x)
			}
		}
	})
}
