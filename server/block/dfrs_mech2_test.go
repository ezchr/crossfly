//go:build crossfly_full

package block_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestDfHopperTransfersEveryEightTicksOnRedstoneTicks(t *testing.T) {
	w := dfWorld(t)
	at := cube.Pos{0, 64, 0}
	chestPos := at.Side(cube.FaceEast)
	dfDo(w, func(tx *world.Tx) {
		dfFloor(tx)
		tx.SetBlock(chestPos, block.NewChest(), nil)
		h := block.NewHopper()
		h.Facing = cube.FaceEast
		_ = h.Inventory(tx, at).SetItem(0, item.NewStack(block.Stone{}, 4))
		tx.SetBlock(at, h, nil)
	})
	var moves []int64
	last := 0
	for range 40 {
		w.AdvanceTick()
		var tick int64
		n := dfCount(w, func(tx *world.Tx) int { tick = tx.CurrentTick(); return dfInvCount(chestPos)(tx) })
		if n != last {
			moves = append(moves, tick)
			last = n
		}
	}
	if len(moves) != 4 {
		t.Fatalf("moves at ticks %v, want 4", moves)
	}
	for i, m := range moves {
		if m%2 != 0 {
			t.Errorf("move at odd tick %d: hoppers work on redstone ticks", m)
		}
		if i > 0 && m-moves[i-1] != 8 {
			t.Errorf("moves at ticks %v, want 8 apart", moves)
		}
	}
}

func TestDfHopperChainWaitsAndLock(t *testing.T) {
	w := dfWorld(t)
	a, b := cube.Pos{0, 64, 0}, cube.Pos{1, 64, 0}
	chestPos := cube.Pos{2, 64, 0}
	dfDo(w, func(tx *world.Tx) {
		dfFloor(tx)
		tx.SetBlock(chestPos, block.NewChest(), nil)
		hb := block.NewHopper()
		hb.Facing = cube.FaceEast
		tx.SetBlock(b, hb, nil)
		ha := block.NewHopper()
		ha.Facing = cube.FaceEast
		_ = ha.Inventory(tx, a).SetItem(0, item.NewStack(block.Stone{}, 1))
		tx.SetBlock(a, ha, nil)
	})
	var inB, inChest int64 = -1, -1
	for range 40 {
		w.AdvanceTick()
		dfDo(w, func(tx *world.Tx) {
			if inB < 0 && dfInvCount(b)(tx) == 1 {
				inB = tx.CurrentTick()
			}
			if inChest < 0 && dfInvCount(chestPos)(tx) == 1 {
				inChest = tx.CurrentTick()
			}
		})
	}
	if inB < 0 || inChest < 0 || inChest-inB != 8 {
		t.Fatalf("item reached the second hopper at %d and the chest at %d, want 8 ticks later", inB, inChest)
	}

	// A locked hopper moves nothing.
	dfDo(w, func(tx *world.Tx) {
		h := tx.Block(a).(block.Hopper)
		h.Powered = true
		_ = h.Inventory(tx, a).SetItem(0, item.NewStack(block.Stone{}, 5))
		tx.SetBlock(a, h, &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true})
	})
	for range 20 {
		w.AdvanceTick()
	}
	if n := dfCount(w, dfInvCount(a)); n != 5 {
		t.Fatalf("locked hopper holds %d, want 5", n)
	}
}

func TestDfHopperPicksUpItemEntityStack(t *testing.T) {
	w := dfWorld(t)
	at := cube.Pos{0, 64, 0}
	dfDo(w, func(tx *world.Tx) {
		dfFloor(tx)
		h := block.NewHopper()
		h.Facing = cube.FaceDown
		tx.SetBlock(at, h, nil)
		tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 65.2, 0.5}}, item.NewStack(block.Dirt{}, 7)))
	})
	for range 6 {
		w.AdvanceTick()
	}
	if n := dfCount(w, dfInvCount(at)); n != 7 {
		t.Fatalf("hopper picked up %d, want the whole stack of 7", n)
	}
}

func TestDfCopperBulbTogglesOnRisingEdge(t *testing.T) {
	w := dfWorld(t)
	at := cube.Pos{0, 64, 0}
	src := at.Side(cube.FaceWest)
	bulb := func() block.CopperBulb {
		var b block.CopperBulb
		dfDo(w, func(tx *world.Tx) { b = tx.Block(at).(block.CopperBulb) })
		return b
	}
	dfDo(w, func(tx *world.Tx) {
		dfFloor(tx)
		tx.SetBlock(at, block.CopperBulb{}, nil)
	})
	step := func(b world.Block) {
		dfDo(w, func(tx *world.Tx) { tx.SetBlock(src, b, nil) })
		for range 4 {
			w.AdvanceTick()
		}
	}
	step(block.RedstoneBlock{})
	if b := bulb(); !b.Lit || !b.Powered {
		t.Fatalf("after power on: %+v, want lit and powered", b)
	}
	step(block.Air{})
	if b := bulb(); !b.Lit || b.Powered {
		t.Fatalf("after power off: %+v, want still lit, unpowered", b)
	}
	step(block.RedstoneBlock{})
	if b := bulb(); b.Lit || !b.Powered {
		t.Fatalf("after power on again: %+v, want dark and powered", b)
	}
	if l := (block.CopperBulb{Lit: true, Oxidation: block.WeatheredOxidation()}).LightEmissionLevel(); l != 8 {
		t.Errorf("weathered bulb light %d, want 8", l)
	}
}

func TestDfPoweredRailLineReach(t *testing.T) {
	for _, activator := range []bool{false, true} {
		w := dfWorld(t)
		rail := func(on bool) world.Block {
			if activator {
				return block.ActivatorRail{Shape: block.RailEastWest, Powered: on}
			}
			return block.PoweredRail{Shape: block.RailEastWest, Powered: on}
		}
		powered := func(tx *world.Tx, x int) bool {
			switch r := tx.Block(cube.Pos{x, 64, 0}).(type) {
			case block.PoweredRail:
				return r.Powered
			case block.ActivatorRail:
				return r.Powered
			}
			t.Fatalf("no rail at %d: %#v", x, tx.Block(cube.Pos{x, 64, 0}))
			return false
		}
		dfDo(w, func(tx *world.Tx) {
			dfFloor(tx)
			for x := 0; x < 12; x++ {
				tx.SetBlock(cube.Pos{x, 64, 0}, rail(false), nil)
			}
		})
		w.AdvanceTick()
		dfDo(w, func(tx *world.Tx) { tx.SetBlock(cube.Pos{-1, 64, 0}, block.RedstoneBlock{}, nil) })
		for range 4 {
			w.AdvanceTick()
		}
		dfDo(w, func(tx *world.Tx) {
			for x := 0; x < 12; x++ {
				if want := x <= 8; powered(tx, x) != want {
					t.Errorf("activator=%t: rail %d powered=%t, want %t", activator, x, powered(tx, x), want)
				}
			}
		})
		dfDo(w, func(tx *world.Tx) { tx.SetBlock(cube.Pos{-1, 64, 0}, block.Air{}, nil) })
		for range 4 {
			w.AdvanceTick()
		}
		dfDo(w, func(tx *world.Tx) {
			for x := 0; x < 12; x++ {
				if powered(tx, x) {
					t.Errorf("activator=%t: rail %d still powered after the power went", activator, x)
				}
			}
		})
	}
}

func TestDfDetectorRailMinecart(t *testing.T) {
	w := dfWorld(t)
	at := cube.Pos{0, 64, 0}
	dfDo(w, func(tx *world.Tx) {
		dfFloor(tx)
		tx.SetBlock(at, block.DetectorRail{Shape: block.RailEastWest}, nil)
		tx.AddEntity(entity.NewMinecart(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64.0625, 0.5}}, item.MinecartChest))
	})
	for range 4 {
		w.AdvanceTick()
	}
	dfDo(w, func(tx *world.Tx) {
		r := tx.Block(at).(block.DetectorRail)
		if !r.Powered {
			t.Fatalf("detector rail with a minecart on it is off")
		}
		if got := block.DetectorRailComparatorOutput(at, tx); got != 0 {
			t.Errorf("empty chest minecart reads %d, want 0", got)
		}
		for e := range tx.EntitiesWithin(cube.Box(-2, 62, -2, 3, 67, 3)) {
			if c, ok := e.(block.RailMinecart); ok && c.MinecartInventory() != nil {
				_ = c.MinecartInventory().SetItem(0, item.NewStack(block.Stone{}, 64))
			}
		}
		if got := block.DetectorRailComparatorOutput(at, tx); got != 1 {
			t.Errorf("chest minecart with one stack reads %d, want 1", got)
		}
	})
	// The minecart leaves: the rail turns off at its next check.
	dfDo(w, func(tx *world.Tx) {
		for e := range tx.EntitiesWithin(cube.Box(-2, 62, -2, 3, 67, 3)) {
			if _, ok := e.(block.RailMinecart); ok {
				tx.RemoveEntity(e)
			}
		}
	})
	for range 25 {
		w.AdvanceTick()
	}
	dfDo(w, func(tx *world.Tx) {
		if tx.Block(at).(block.DetectorRail).Powered {
			t.Errorf("detector rail still on after the minecart left")
		}
	})
}

func TestDfLecternPagePulse(t *testing.T) {
	w := dfWorld(t)
	at := cube.Pos{0, 64, 0}
	dfDo(w, func(tx *world.Tx) {
		dfFloor(tx)
		tx.SetBlock(at, block.Lectern{Book: item.NewStack(item.BookAndQuill{Pages: []string{"a", "b", "c"}}, 1)}, nil)
	})
	dfDo(w, func(tx *world.Tx) {
		if err := tx.Block(at).(block.Lectern).TurnPage(at, tx, 1); err != nil {
			t.Fatal(err)
		}
		if !tx.Block(at).(block.Lectern).Powered {
			t.Errorf("lectern not powered after a page turn")
		}
	})
	for range 3 {
		w.AdvanceTick()
	}
	dfDo(w, func(tx *world.Tx) {
		if tx.Block(at).(block.Lectern).Powered {
			t.Errorf("lectern still powered 3 ticks after the page turn")
		}
	})
}
