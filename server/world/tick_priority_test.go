//go:build crossfly_full

package world

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"math/rand/v2"
	"slices"
	"testing"
	"time"
)

type priorityTickTestBlock struct{}

var priorityTickTestRun func(cube.Pos, *Tx)

func (priorityTickTestBlock) ScheduledTick(pos cube.Pos, tx *Tx, _ *rand.Rand) {
	priorityTickTestRun(pos, tx)
}
func (priorityTickTestBlock) EncodeBlock() (string, map[string]any) { return "test:tick_priority", nil }
func (priorityTickTestBlock) Hash() (uint64, uint64)                { return 1 << 45, 0 }
func (priorityTickTestBlock) Model() BlockModel                     { return nil }

func TestScheduledTickPriorityTimeAndInsertionOrder(t *testing.T) {
	registry := NewBlockRegistry()
	registry.RegisterBlockState(BlockState{Name: "test:tick_priority", Properties: map[string]any{}})
	registry.RegisterBlock(priorityTickTestBlock{})
	w := Config{Synchronous: true, Blocks: registry}.New()
	defer w.Close()
	queue := newScheduledTickQueue(100)
	b := priorityTickTestBlock{}
	var got []int
	priorityTickTestRun = func(pos cube.Pos, tx *Tx) {
		got = append(got, pos[0])
		if pos[0] == 1 {
			queue.schedulePriority(registry, pos, b, 3*time.Second/20, TickPriorityVeryHigh)
		}
	}
	t.Cleanup(func() { priorityTickTestRun = nil })
	runWorld(w, func(tx *Tx) {
		for x := 0; x < 5; x++ {
			tx.SetBlock(cube.Pos{x, 64, 0}, b, nil)
		}
		// Time wins over priority. Equal priorities retain original insertion order.
		queue.schedulePriority(registry, cube.Pos{0, 64, 0}, b, 2*time.Second/20, TickPriorityExtremelyHigh)
		queue.schedulePriority(registry, cube.Pos{1, 64, 0}, b, time.Second/20, TickPriorityHigh)
		queue.schedulePriority(registry, cube.Pos{2, 64, 0}, b, time.Second/20, TickPriorityNormal)
		queue.schedulePriority(registry, cube.Pos{3, 64, 0}, b, time.Second/20, TickPriorityVeryHigh)
		queue.schedulePriority(registry, cube.Pos{4, 64, 0}, b, time.Second/20, TickPriorityHigh)
		queue.tick(tx, 102)
		if !slices.Equal(got, []int{3, 1, 4, 2, 0}) {
			t.Fatalf("run order %v", got)
		}
		if len(queue.ticks) != 1 || queue.ticks[0].t != 105 {
			t.Fatalf("callback-scheduled tick lost: %v", queue.ticks)
		}
		if !queue.scheduled(registry, cube.Pos{1, 64, 0}, b) {
			t.Fatal("callback tick not indexed")
		}
	})
}
func TestScheduledTickReloadPreservesPriorityAndOrder(t *testing.T) {
	queue := newScheduledTickQueue(100)
	queue.add([]scheduledTick{{pos: cube.Pos{0, 64, 0}, t: 110, priority: TickPriorityHigh, order: 20}, {pos: cube.Pos{1, 64, 0}, t: 110, priority: TickPriorityVeryHigh, order: 10}})
	if queue.nextOrder != 20 {
		t.Fatal("reloaded sequence not restored")
	}
	queue.schedule(DefaultBlockRegistry, cube.Pos{2, 64, 0}, scheduledTickTestBlock{}, time.Second)
	if queue.ticks[2].order != 21 {
		t.Fatal("new tick order predates restored ticks")
	}
	if queue.ticks[0].priority != TickPriorityHigh || queue.ticks[1].priority != TickPriorityVeryHigh {
		t.Fatal("loaded priorities lost")
	}
}
