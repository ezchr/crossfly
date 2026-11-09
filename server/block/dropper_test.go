//go:build crossfly_full

package block_test

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"math/rand/v2"
	"testing"
)

func TestDropperTransfersOneItemAndRetainsWhenFull(t *testing.T) {
	for _, full := range []bool{false, true} {
		w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry}.New()
		runDispenserWorld(t, w, func(tx *world.Tx) {
			d := block.NewDropper()
			d.Facing = cube.FaceEast
			chest := block.NewChest()
			if full {
				for i := 0; i < chest.Inventory(tx, cube.Pos{}).Size(); i++ {
					_ = chest.Inventory(tx, cube.Pos{}).SetItem(i, item.NewStack(block.Stone{}, 64))
				}
			}
			_ = d.Inventory(tx, cube.Pos{}).SetItem(0, item.NewStack(block.Stone{}, 64))
			tx.SetBlock(cube.Pos{0, 64, 0}, d, nil)
			tx.SetBlock(cube.Pos{1, 64, 0}, chest, nil)
			d.ScheduledTick(cube.Pos{0, 64, 0}, tx, rand.New(rand.NewPCG(1, 2)))
			src, _ := d.Inventory(tx, cube.Pos{}).Item(0)
			dest, _ := chest.Inventory(tx, cube.Pos{}).Item(0)
			wantSrc, wantDest := 63, 1
			if full {
				wantSrc, wantDest = 64, 64
			}
			if src.Count() != wantSrc || dest.Count() != wantDest {
				t.Errorf("full=%t source=%d destination=%d", full, src.Count(), dest.Count())
			}
			for e := range tx.EntitiesWithin(cube.Box(-2, 60, -2, 3, 68, 3)) {
				t.Errorf("container transfer ejected %s", e.H().Type().EncodeEntity())
			}
		})
		w.Close()
	}
}

func TestDropperEjectsArrowAsItem(t *testing.T) {
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry}.New()
	defer w.Close()
	runDispenserWorld(t, w, func(tx *world.Tx) {
		d := block.NewDropper()
		d.Facing = cube.FaceEast
		_ = d.Inventory(tx, cube.Pos{}).SetItem(0, item.NewStack(item.Arrow{}, 2))
		tx.SetBlock(cube.Pos{0, 64, 0}, d, nil)
		d.ScheduledTick(cube.Pos{0, 64, 0}, tx, rand.New(rand.NewPCG(1, 2)))
		count := 0
		for e := range tx.EntitiesWithin(cube.Box(-2, 60, -2, 3, 68, 3)) {
			count++
			if name := e.H().Type().EncodeEntity(); name != "minecraft:item" {
				t.Errorf("dropper fired %s", name)
			}
		}
		remaining, _ := d.Inventory(tx, cube.Pos{}).Item(0)
		if count != 1 || remaining.Count() != 1 {
			t.Errorf("ejected %d entities, remaining=%d", count, remaining.Count())
		}
	})
}

func TestDropperUsesFurnaceSidedInsertion(t *testing.T) {
	for _, face := range []cube.Face{cube.FaceDown, cube.FaceEast} {
		w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry}.New()
		runDispenserWorld(t, w, func(tx *world.Tx) {
			at := cube.Pos{0, 64, 0}
			dest := at.Side(face)
			d := block.NewDropper()
			d.Facing = face
			_ = d.Inventory(tx, at).SetItem(0, item.NewStack(block.Stone{}, 2))
			furnace := block.NewFurnace(cube.North)
			tx.SetBlock(at, d, nil)
			tx.SetBlock(dest, furnace, nil)
			d.ScheduledTick(at, tx, rand.New(rand.NewPCG(1, 2)))
			remaining, _ := d.Inventory(tx, at).Item(0)
			inserted, _ := furnace.Inventory(tx, dest).Item(0)
			if face == cube.FaceDown && (remaining.Count() != 1 || inserted.Count() != 1) {
				t.Error("downward dropper did not use input slot")
			}
			if face == cube.FaceEast && (remaining.Count() != 2 || !inserted.Empty()) {
				t.Error("side dropper inserted non-fuel into furnace")
			}
		})
		w.Close()
	}
}

func TestDropperNBTKeepsVariantAndInventory(t *testing.T) {
	d := block.NewDropper()
	d.Facing = cube.FaceWest
	d.Triggered = true
	_ = d.Inventory(nil, cube.Pos{}).SetItem(2, item.NewStack(block.Stone{}, 17))
	data := d.EncodeNBT()
	if data["id"] != "Dropper" {
		t.Fatalf("wrong block entity: %v", data["id"])
	}
	restored := d.DecodeNBT(data).(block.Dispenser)
	stack, _ := restored.Inventory(nil, cube.Pos{}).Item(2)
	if !restored.Dropper || restored.Facing != cube.FaceWest || !restored.Triggered || stack.Count() != 17 {
		t.Fatal("dropper save lost its state")
	}
}
