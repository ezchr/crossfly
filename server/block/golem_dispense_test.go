//go:build crossfly_full

package block

import (
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func TestDispenserPlacesCarvedPumpkinOnGolemPattern(t *testing.T) {
	h := withGolemHost(t)
	golemWorld(t, func(tx *world.Tx) {
		// A dispenser at (0, 3, 1) facing north, an iron T under (0, 3, 0).
		disp, head := cube.Pos{0, 3, 1}, cube.Pos{0, 3, 0}
		cells := buildGolemIronT(tx, head, true)
		ctx := &item.DispenseContext{Rand: rand.New(rand.NewPCG(1, 2))}
		if r := (Pumpkin{Carved: true}).Dispense(disp, cube.FaceNorth, tx, ctx); r != item.DispenseSuccess || ctx.CountSub != 1 {
			t.Fatalf("dispense = %v, count sub %v; want success using one pumpkin", r, ctx.CountSub)
		}
		golemOnlySpawn(t, h, "minecraft:iron_golem")
		golemRequireAir(t, tx, cells...)
	})
}

func TestDispenserPlacesJackOLanternOnSnowGolemPattern(t *testing.T) {
	h := withGolemHost(t)
	golemWorld(t, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 1, 0}, Snow{}, nil)
		tx.SetBlock(cube.Pos{0, 2, 0}, Snow{}, nil)
		ctx := &item.DispenseContext{}
		// A dispenser above the head spot, facing down.
		if r := (LitPumpkin{}).Dispense(cube.Pos{0, 4, 0}, cube.FaceDown, tx, ctx); r != item.DispenseSuccess {
			t.Fatalf("dispense = %v, want success", r)
		}
		golemOnlySpawn(t, h, "minecraft:snow_golem")
	})
}

func TestDispenserHeadPlacedFacingSouthWhenNoGolemSpawns(t *testing.T) {
	h := withGolemHost(t)
	h.fail = true
	golemWorld(t, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 1, 0}, Copper{Type: NormalCopper()}, nil)
		ctx := &item.DispenseContext{}
		if r := (Pumpkin{Carved: true}).Dispense(cube.Pos{1, 2, 0}, cube.FaceWest, tx, ctx); r != item.DispenseSuccess {
			t.Fatalf("dispense = %v, want success", r)
		}
		// The 26.60 dispenser places the head in its default state.
		if p, ok := tx.Block(cube.Pos{0, 2, 0}).(Pumpkin); !ok || !p.Carved || p.Facing != cube.South {
			t.Fatalf("head spot holds %#v, want a carved pumpkin facing south", tx.Block(cube.Pos{0, 2, 0}))
		}
	})
}

func TestDispenserDropsPumpkinWithoutGolemPattern(t *testing.T) {
	withGolemHost(t)
	golemWorld(t, func(tx *world.Tx) {
		ctx := &item.DispenseContext{}
		for _, b := range []world.Item{Pumpkin{Carved: true}, LitPumpkin{}, Pumpkin{}} {
			if r := b.(item.Dispensable).Dispense(cube.Pos{0, 2, 1}, cube.FaceNorth, tx, ctx); r != item.DispenseDefault {
				t.Fatalf("%T dispense = %v, want default (dropped)", b, r)
			}
		}
		// A plain pumpkin is no golem head, even on a pattern.
		tx.SetBlock(cube.Pos{0, 1, 0}, Snow{}, nil)
		tx.SetBlock(cube.Pos{0, 0, 0}, Snow{}, nil)
		if r := (Pumpkin{}).Dispense(cube.Pos{0, 2, 1}, cube.FaceNorth, tx, ctx); r != item.DispenseDefault {
			t.Fatalf("plain pumpkin dispense = %v, want default", r)
		}
		if !isGolemAir(tx.Block(cube.Pos{0, 2, 0})) {
			t.Fatal("plain pumpkin placed")
		}
	})
}

func TestDispenserBuildsGolemThroughScheduledTick(t *testing.T) {
	h := withGolemHost(t)
	golemWorld(t, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{5, 1, 5}, Snow{}, nil)
		tx.SetBlock(cube.Pos{5, 2, 5}, Snow{}, nil)
		d := NewDispenser()
		d.Facing = cube.FaceDown
		_ = d.Inventory(tx, cube.Pos{5, 4, 5}).SetItem(0, item.NewStack(Pumpkin{Carved: true}, 2))
		d.ScheduledTick(cube.Pos{5, 4, 5}, tx, rand.New(rand.NewPCG(3, 4)))
		golemOnlySpawn(t, h, "minecraft:snow_golem")
		if s, _ := d.Inventory(tx, cube.Pos{5, 4, 5}).Item(0); s.Count() != 1 {
			t.Fatalf("dispenser holds %v, want one pumpkin left", s)
		}
	})
}
