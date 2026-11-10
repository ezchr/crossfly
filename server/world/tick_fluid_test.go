//go:build crossfly_full

package world

import (
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// fluidTickTestLiquid is a liquid that records its scheduled ticks.
type fluidTickTestLiquid struct{}

func (fluidTickTestLiquid) ScheduledTick(pos cube.Pos, tx *Tx, _ *rand.Rand) {
	priorityTickTestRun(pos, tx)
}
func (fluidTickTestLiquid) EncodeBlock() (string, map[string]any)  { return "test:tick_fluid", nil }
func (fluidTickTestLiquid) Hash() (uint64, uint64)                 { return 1<<45 + 1, 0 }
func (fluidTickTestLiquid) Model() BlockModel                      { return nil }
func (fluidTickTestLiquid) LiquidDepth() int                       { return 8 }
func (fluidTickTestLiquid) SpreadDecay() int                       { return 1 }
func (l fluidTickTestLiquid) WithDepth(int, bool) Liquid           { return l }
func (fluidTickTestLiquid) LiquidFalling() bool                    { return false }
func (fluidTickTestLiquid) BlastResistance() float64               { return 100 }
func (fluidTickTestLiquid) LiquidType() string                     { return "test" }
func (fluidTickTestLiquid) Harden(cube.Pos, *Tx, *cube.Pos) bool   { return false }
func (fluidTickTestLiquid) LiquidRemoveBlock(cube.Pos, *Tx, Block) {}

// Java Edition runs the fluid ticks of a tick after all its block ticks (ServerLevel.tick: blockTicks, then
// fluidTicks), whatever their priorities and the order they were scheduled in.
func TestScheduledFluidTicksRunAfterBlockTicks(t *testing.T) {
	registry := NewBlockRegistry()
	registry.RegisterBlockState(BlockState{Name: "test:tick_priority", Properties: map[string]any{}})
	registry.RegisterBlockState(BlockState{Name: "test:tick_fluid", Properties: map[string]any{}})
	registry.RegisterBlock(priorityTickTestBlock{})
	registry.RegisterBlock(fluidTickTestLiquid{})
	w := Config{Synchronous: true, Blocks: registry}.New()
	defer w.Close()
	queue := newScheduledTickQueue(100)
	var got []int
	priorityTickTestRun = func(pos cube.Pos, tx *Tx) { got = append(got, pos[0]) }
	t.Cleanup(func() { priorityTickTestRun = nil })
	runWorld(w, func(tx *Tx) {
		tx.SetBlock(cube.Pos{0, 64, 0}, fluidTickTestLiquid{}, nil)
		tx.SetBlock(cube.Pos{1, 64, 0}, priorityTickTestBlock{}, nil)
		tx.SetBlock(cube.Pos{2, 64, 0}, fluidTickTestLiquid{}, nil)
		tx.SetBlock(cube.Pos{3, 64, 0}, priorityTickTestBlock{}, nil)
		// The fluids are scheduled first and with the higher priority, one of them even a tick earlier.
		queue.schedulePriority(registry, cube.Pos{0, 64, 0}, fluidTickTestLiquid{}, time.Second/20, TickPriorityExtremelyHigh)
		queue.schedulePriority(registry, cube.Pos{2, 64, 0}, fluidTickTestLiquid{}, time.Second/20, TickPriorityNormal)
		queue.schedulePriority(registry, cube.Pos{1, 64, 0}, priorityTickTestBlock{}, 2*time.Second/20, TickPriorityNormal)
		queue.schedulePriority(registry, cube.Pos{3, 64, 0}, priorityTickTestBlock{}, time.Second/20, TickPriorityLow)
		queue.tick(tx, 102)
		if !slices.Equal(got, []int{3, 1, 0, 2}) {
			t.Fatalf("run order %v, want the block ticks (3, 1) before the fluid ticks (0, 2)", got)
		}
	})
}
