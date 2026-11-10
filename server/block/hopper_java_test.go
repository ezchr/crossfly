//go:build crossfly_full

package block_test

import (
	"context"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// hopperCounts returns the item count of the container at each position.
func hopperCounts(w *world.World, at ...cube.Pos) []int {
	out := make([]int, len(at))
	_ = w.Do(func(tx *world.Tx) {
		for i, pos := range at {
			c, ok := tx.Block(pos).(block.Container)
			if !ok {
				continue
			}
			for _, s := range c.Inventory(tx, pos).Slots() {
				out[i] += s.Count()
			}
		}
	}).Wait(context.Background())
	return out
}

// A hopper moves an item on its first tick, then every 8 ticks; a hopper that receives into its empty inventory from
// a hopper ticking after it waits 7, as Java's HopperBlockEntity does.
func TestJavaHopperChainTiming(t *testing.T) {
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry}.New()
	defer w.Close()
	a, b, c := cube.Pos{0, 64, 0}, cube.Pos{1, 64, 0}, cube.Pos{2, 64, 0}
	runDispenserWorld(t, w, func(tx *world.Tx) {
		for _, pos := range []cube.Pos{a, b} {
			h := block.NewHopper()
			h.Facing = cube.FaceEast
			tx.SetBlock(pos, h, nil)
		}
		tx.SetBlock(c, block.NewChest(), nil)
		ha := tx.Block(a).(block.Hopper)
		_ = ha.Inventory(tx, a).SetItem(0, item.NewStack(block.Cobblestone{}, 3))
	})
	var got [][]int
	for i := 0; i < 20; i++ {
		w.AdvanceTick()
		got = append(got, hopperCounts(w, a, b, c))
	}
	// Tick 1: A (created first, ticks first) pushes into B, which waits 8 from then. Tick 8: B pushes into the chest.
	// Tick 9: A pushes the next item.
	for tick, want := range map[int][]int{1: {2, 1, 0}, 7: {2, 1, 0}, 8: {2, 0, 1}, 9: {1, 1, 1}} {
		if g := got[tick-1]; !slices.Equal(g, want) {
			t.Errorf("after tick %d: counts %v, want %v (all: %v)", tick, g, want, got)
		}
	}
}

// Hoppers do not conduct redstone: a torch under one hopper locks it, not the hopper on top of it.
func TestJavaHopperDoesNotConduct(t *testing.T) {
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry, SettleSynchronousTransactions: true}.New()
	defer w.Close()
	lower, upper := cube.Pos{0, 65, 0}, cube.Pos{0, 66, 0}
	runDispenserWorld(t, w, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 63, 0}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{0, 64, 0}, block.RedstoneTorch{Facing: cube.FaceDown, Lit: true}, nil)
		tx.SetBlock(lower, block.NewHopper(), nil)
		tx.SetBlock(upper, block.NewHopper(), nil)
	})
	w.AdvanceTick()
	runDispenserWorld(t, w, func(tx *world.Tx) {
		if !tx.Block(lower).(block.Hopper).Powered {
			t.Errorf("the hopper on the torch is not locked")
		}
		if tx.Block(upper).(block.Hopper).Powered {
			t.Errorf("the hopper above it is locked")
		}
	})
}
