//go:build crossfly_full

package block_test

import (
	"context"
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
)

// The tests of the mechanisms with redstone=dragonfly: Bedrock's rules on Dragonfly's redstone engine.

// dfWorld returns a synchronous world with redstone=dragonfly and minecarts. Java redstone is back on when the test
// ends.
func dfWorld(t *testing.T) *world.World {
	t.Helper()
	world.SetJavaRedstone(false)
	reg := entity.WithJavaRedstone(entity.DefaultRegistry.Config()).New(append(entity.DefaultRegistry.Types(), entity.JavaRedstoneTypes()...))
	w := world.Config{Synchronous: true, Entities: reg}.New()
	t.Cleanup(func() {
		_ = w.Close()
		world.SetJavaRedstone(true)
	})
	return w
}

func dfDo(w *world.World, f func(tx *world.Tx)) { w.Do(f).Wait(context.Background()) }

// dfFloor lays stone under the area the tests use.
func dfFloor(tx *world.Tx) {
	for x := -4; x <= 16; x++ {
		for z := -4; z <= 4; z++ {
			tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, &world.SetOpts{DisableBlockUpdates: true})
		}
	}
}

func dfCount(w *world.World, f func(tx *world.Tx) int) (n int) {
	dfDo(w, func(tx *world.Tx) { n = f(tx) })
	return n
}

func dfInvCount(pos cube.Pos) func(tx *world.Tx) int {
	return func(tx *world.Tx) int {
		c, ok := tx.Block(pos).(block.Container)
		if !ok {
			return -1
		}
		n := 0
		for _, s := range c.Inventory(tx, pos).Slots() {
			n += s.Count()
		}
		return n
	}
}

func TestDfDispenserFiresOnceOnRisingEdge(t *testing.T) {
	w := dfWorld(t)
	at := cube.Pos{0, 64, 0}
	dfDo(w, func(tx *world.Tx) {
		dfFloor(tx)
		d := block.NewDispenser()
		d.Facing = cube.FaceEast
		_ = d.Inventory(tx, at).SetItem(0, item.NewStack(block.Stone{}, 10))
		tx.SetBlock(at, d, nil)
	})
	w.AdvanceTick()
	dfDo(w, func(tx *world.Tx) { tx.SetBlock(at.Side(cube.FaceWest), block.RedstoneBlock{}, nil) })
	fired := -1
	for i := 1; i <= 30; i++ {
		w.AdvanceTick()
		if n := dfCount(w, dfInvCount(at)); n != 10 && fired < 0 {
			fired = i
		}
	}
	if fired < 0 {
		t.Fatalf("powered dispenser never fired")
	}
	if fired > 6 {
		t.Errorf("dispenser fired %d ticks after power, want about 4 after the engine saw it", fired)
	}
	if n := dfCount(w, dfInvCount(at)); n != 9 {
		t.Fatalf("dispenser fired %d times while powered, want once", 10-n)
	}
	if d := dfCount(w, func(tx *world.Tx) int {
		if tx.Block(at).(block.Dispenser).Triggered {
			return 1
		}
		return 0
	}); d != 1 {
		t.Errorf("powered dispenser not triggered")
	}
	// Off, then on again: it fires again.
	dfDo(w, func(tx *world.Tx) { tx.SetBlock(at.Side(cube.FaceWest), block.Air{}, nil) })
	for range 4 {
		w.AdvanceTick()
	}
	dfDo(w, func(tx *world.Tx) { tx.SetBlock(at.Side(cube.FaceWest), block.RedstoneBlock{}, nil) })
	for range 12 {
		w.AdvanceTick()
	}
	if n := dfCount(w, dfInvCount(at)); n != 8 {
		t.Fatalf("after a second rising edge %d left, want 8", n)
	}
}

func TestDfDispenserNoQuasiConnectivity(t *testing.T) {
	w := dfWorld(t)
	at := cube.Pos{0, 64, 0}
	dfDo(w, func(tx *world.Tx) {
		dfFloor(tx)
		d := block.NewDispenser()
		d.Facing = cube.FaceEast
		_ = d.Inventory(tx, at).SetItem(0, item.NewStack(block.Stone{}, 10))
		tx.SetBlock(at, d, nil)
	})
	w.AdvanceTick()
	// A redstone block next to the space above the dispenser powers that space only: Java's quasi-connectivity would
	// fire the dispenser, Bedrock's rules don't.
	dfDo(w, func(tx *world.Tx) { tx.SetBlock(at.Side(cube.FaceUp).Side(cube.FaceWest), block.RedstoneBlock{}, nil) })
	for range 20 {
		w.AdvanceTick()
	}
	// A block update next to it does nothing either.
	dfDo(w, func(tx *world.Tx) { tx.SetBlock(at.Side(cube.FaceNorth), block.Stone{}, nil) })
	for range 20 {
		w.AdvanceTick()
	}
	if n := dfCount(w, dfInvCount(at)); n != 10 {
		t.Fatalf("dispenser fired without power: %d left", n)
	}
}

func TestDfDropperIntoContainers(t *testing.T) {
	w := dfWorld(t)
	at := cube.Pos{0, 64, 0}
	r := rand.New(rand.NewPCG(1, 2))
	dfDo(w, func(tx *world.Tx) {
		dfFloor(tx)
		d := block.NewDropper()
		d.Facing = cube.FaceEast
		_ = d.Inventory(tx, at).SetItem(4, item.NewStack(block.Stone{}, 3))
		tx.SetBlock(at, d, nil)
		chest := block.NewChest()
		tx.SetBlock(at.Side(cube.FaceEast), chest, nil)
		chest = tx.Block(at.Side(cube.FaceEast)).(block.Chest)
		// The chest's first slot holds dirt: the stone goes into the first empty slot after it.
		_ = chest.Inventory(tx, at.Side(cube.FaceEast)).SetItem(0, item.NewStack(block.Dirt{}, 1))

		tx.Block(at).(block.Dispenser).ScheduledTick(at, tx, r)
		got, _ := chest.Inventory(tx, at.Side(cube.FaceEast)).Item(1)
		if got.Count() != 1 {
			t.Errorf("chest slot 1 = %v, want one stone", got)
		}
		if n := dfInvCount(at)(tx); n != 2 {
			t.Errorf("dropper holds %d, want 2", n)
		}
		// A full chest keeps the item in the dropper, and nothing is ejected.
		for i := 0; i < chest.Inventory(tx, at.Side(cube.FaceEast)).Size(); i++ {
			_ = chest.Inventory(tx, at.Side(cube.FaceEast)).SetItem(i, item.NewStack(block.Dirt{}, 64))
		}
		tx.Block(at).(block.Dispenser).ScheduledTick(at, tx, r)
		if n := dfInvCount(at)(tx); n != 2 {
			t.Errorf("dropper into a full chest holds %d, want 2", n)
		}
		for e := range tx.EntitiesWithin(cube.Box(-3, 60, -3, 4, 68, 4)) {
			t.Errorf("dropper ejected %s into a full chest", e.H().Type().EncodeEntity())
		}
		// No container: ejected.
		tx.SetBlock(at.Side(cube.FaceEast), block.Air{}, nil)
		tx.Block(at).(block.Dispenser).ScheduledTick(at, tx, r)
		n := 0
		for range tx.EntitiesWithin(cube.Box(-3, 60, -3, 4, 68, 4)) {
			n++
		}
		if n != 1 || dfInvCount(at)(tx) != 1 {
			t.Errorf("dropper without container: %d entities, %d left", n, dfInvCount(at)(tx))
		}
	})
}

func TestDfDispenserBedrockBehaviours(t *testing.T) {
	w := dfWorld(t)
	at := cube.Pos{0, 64, 0}
	r := rand.New(rand.NewPCG(1, 2))
	fire := func(tx *world.Tx, s item.Stack) block.Dispenser {
		d := block.NewDispenser()
		d.Facing = cube.FaceEast
		_ = d.Inventory(tx, at).SetItem(0, s)
		tx.SetBlock(at, d, nil)
		d = tx.Block(at).(block.Dispenser)
		d.ScheduledTick(at, tx, r)
		return d
	}
	front := at.Side(cube.FaceEast)
	dfDo(w, func(tx *world.Tx) {
		dfFloor(tx)

		// A minecart goes onto the rail in front.
		tx.SetBlock(front, block.Rail{Shape: block.RailEastWest}, nil)
		fire(tx, item.NewStack(item.Minecart{}, 1))
		carts := 0
		for e := range tx.EntitiesWithin(cube.Box(-3, 60, -3, 4, 68, 4)) {
			if e.H().Type().EncodeEntity() == "minecraft:minecart" {
				carts++
			}
		}
		if carts != 1 || dfInvCount(at)(tx) != 0 {
			t.Errorf("minecart dispensed onto rail: %d carts, %d left", carts, dfInvCount(at)(tx))
		}
		for e := range tx.EntitiesWithin(cube.Box(-3, 60, -3, 4, 68, 4)) {
			tx.RemoveEntity(e)
		}

		// A shulker box is placed in front, facing away from the dispenser.
		tx.SetBlock(front, block.Air{}, nil)
		fire(tx, item.NewStack(block.NewShulkerBox(), 1))
		if s, ok := tx.Block(front).(block.ShulkerBox); !ok || s.Facing != cube.FaceEast {
			t.Errorf("shulker box not placed: %#v", tx.Block(front))
		}

		// A water bottle turns dirt into mud and leaves a glass bottle.
		tx.SetBlock(front, block.Dirt{}, nil)
		d := fire(tx, item.NewStack(item.Potion{Type: potion.Water()}, 1))
		if _, ok := tx.Block(front).(block.Mud); !ok {
			t.Errorf("water bottle left %#v, want mud", tx.Block(front))
		}
		if s, _ := d.Inventory(tx, at).Item(0); s.Empty() {
			t.Errorf("no glass bottle back")
		} else if _, ok := s.Item().(item.GlassBottle); !ok {
			t.Errorf("got %v back, want a glass bottle", s)
		}

		// An armour stand is spawned, and a helmet then goes onto its head.
		tx.SetBlock(front, block.Air{}, nil)
		fire(tx, item.NewStack(item.ArmourStand{}, 1))
		var stand world.Entity
		for e := range tx.EntitiesWithin(cube.Box(-3, 60, -3, 4, 68, 4)) {
			if e.H().Type().EncodeEntity() == "minecraft:armor_stand" {
				stand = e
			}
		}
		if stand == nil {
			t.Fatalf("no armour stand dispensed")
		}
		fire(tx, item.NewStack(item.Helmet{Tier: item.ArmourTierIron{}}, 1))
		wearer, ok := stand.(interface{ Armour() *inventory.Armour })
		if !ok {
			t.Fatalf("armour stand has no armour")
		}
		if wearer.Armour().Helmet().Empty() || dfInvCount(at)(tx) != 0 {
			t.Errorf("helmet not put on the armour stand")
		}
	})
}
