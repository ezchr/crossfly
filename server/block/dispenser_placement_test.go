//go:build crossfly_full

package block_test

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"testing"
)

func TestDispenserAndDropperPlacementWaitsForNeighbour(t *testing.T) {
	for _, dropper := range []bool{false, true} {
		w := world.Config{Synchronous: true, SettleSynchronousTransactions: true, Entities: entity.DefaultRegistry}.New()
		pos := cube.Pos{0, 64, 0}
		d := block.NewDispenser()
		d.Dropper = dropper
		d.Facing = cube.FaceEast
		runDispenserWorld(t, w, func(tx *world.Tx) { tx.SetBlock(pos.Side(cube.FaceWest), block.RedstoneBlock{}, nil) })
		runDispenserWorld(t, w, func(tx *world.Tx) {
			_ = d.Inventory(tx, pos).SetItem(0, item.NewStack(block.Stone{}, 2))
			tx.SetBlock(pos, d, nil)
		})
		for range 6 {
			w.AdvanceTick()
		}
		runDispenserWorld(t, w, func(tx *world.Tx) {
			got := tx.Block(pos).(block.Dispenser)
			stack, _ := got.Inventory(tx, pos).Item(0)
			if got.Triggered || stack.Count() != 2 {
				t.Errorf("dropper=%t: placed into power fired", dropper)
			}
			tx.SetBlock(pos.Side(cube.FaceNorth), block.Stone{}, nil)
		})
		for range 5 {
			w.AdvanceTick()
		}
		runDispenserWorld(t, w, func(tx *world.Tx) {
			stack, _ := d.Inventory(tx, pos).Item(0)
			if stack.Count() != 1 {
				t.Errorf("dropper=%t: neighbour did not activate dispenser", dropper)
			}
		})
		w.Close()
	}
}
