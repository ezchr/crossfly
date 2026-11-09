package block_test

import (
	"math"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
)

// Efficiency adds level²+1 to the tool's speed at any level (Bedrock's Item::destroySpeedBonus).
func TestBreakDurationEfficiencyAboveMax(t *testing.T) {
	pick := func(lvl int) item.Stack {
		return item.NewStack(item.Pickaxe{Tier: item.ToolTierDiamond}, 1).WithEnchantments(item.NewEnchantment(enchantment.Efficiency, lvl))
	}
	// Obsidian, with a diamond pickaxe (speed 8): (8+level²+1)/hardness/30 progress per tick, within
	// the tick once that is 1 or more.
	hardness := block.Obsidian{}.BreakInfo().Hardness
	for _, level := range []int{1, 5, 10, 20, 50, 255} {
		ticks := 0.0
		if damage := float64(8+level*level+1) / hardness / 30; damage < 1 {
			ticks = math.Ceil(1 / damage)
		}
		want := time.Duration(ticks) * time.Second / 20
		if got := block.BreakDuration(block.Obsidian{}, pick(level), block.BreakContext{}); got != want {
			t.Errorf("Efficiency %d on obsidian: %v, want %v", level, got, want)
		}
	}
}
