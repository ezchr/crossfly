//go:build crossfly_full

package block

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func liquidTestWorld(t *testing.T) *world.World {
	t.Helper()
	w := world.Config{Synchronous: true, SettleSynchronousTransactions: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	runWorld(w, func(tx *world.Tx) {
		for x := -10; x <= 10; x++ {
			for z := -10; z <= 10; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, Stone{}, &world.SetOpts{DisableBlockUpdates: true})
			}
		}
	})
	return w
}

func waterAround(w *world.World) (n int) {
	runWorld(w, func(tx *world.Tx) {
		for x := -10; x <= 10; x++ {
			for z := -10; z <= 10; z++ {
				if _, ok := tx.Liquid(cube.Pos{x, 64, z}); ok {
					n++
				}
			}
		}
	})
	return n
}

// Java's FlowingFluid.getNewLiquid: a flowing water block is what its neighbours give it, so one that nothing
// feeds is gone on its next tick. (Before, it shrank two levels a tick and lingered.)
func TestLoneFlowingWaterVanishesOnItsTick(t *testing.T) {
	w := liquidTestWorld(t)
	pos := cube.Pos{0, 64, 0}
	runWorld(w, func(tx *world.Tx) {
		tx.SetLiquid(pos, Water{Depth: 7})
		tx.ScheduleBlockUpdate(pos, Water{Depth: 7}, time.Second/4)
	})
	for i := 0; i < 5; i++ {
		w.AdvanceTick()
	}
	if n := waterAround(w); n != 0 {
		t.Fatalf("water blocks 5 ticks after a lone flowing water block = %d, want 0", n)
	}
}

// A source feeds the block beside it one level lower; once the source is gone, the whole spread recedes.
func TestWaterSpreadRecedesWithoutSource(t *testing.T) {
	w := liquidTestWorld(t)
	src, flow := cube.Pos{0, 64, 0}, cube.Pos{1, 64, 0}
	runWorld(w, func(tx *world.Tx) {
		tx.SetLiquid(src, Water{Depth: 8, Still: true})
		tx.ScheduleBlockUpdate(src, Water{Depth: 8, Still: true}, time.Second/4)
	})
	for i := 0; i < 60; i++ {
		w.AdvanceTick()
	}
	var fed world.Liquid
	runWorld(w, func(tx *world.Tx) { fed, _ = tx.Liquid(flow) })
	if fed == nil || fed.LiquidDepth() != 7 {
		t.Fatalf("water beside a source = %v, want depth 7", fed)
	}
	runWorld(w, func(tx *world.Tx) { tx.SetLiquid(src, nil) })
	for i := 0; i < 100; i++ {
		w.AdvanceTick()
	}
	if n := waterAround(w); n != 0 {
		t.Fatalf("water blocks 100 ticks after the source was removed = %d, want 0", n)
	}
}
