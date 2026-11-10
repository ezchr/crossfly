//go:build crossfly_full

package jnether

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/item/potion"
)

// The server registers the mob blocks and items (the dried ghast) before
// any world exists; so do the tests.
func init() {
	block.RegisterMobBlocks()
	item.RegisterMobItems()
}

// TestBarterOdds: every entry of the table comes out as often as its weight
// says (out of 469) with counts in its range.
func TestBarterOdds(t *testing.T) {
	w := BarterWeights()
	total := 0
	for _, n := range w {
		total += n
	}
	if total != 469 {
		t.Fatalf("total weight %d, want 469", total)
	}
	r := rand.New(rand.NewPCG(3, 4))
	const n = 469 * 400
	got := map[string]int{}
	ranges := map[string][2]int{
		"minecraft:iron_nugget": {10, 36}, "minecraft:ender_pearl": {2, 4}, "minecraft:string": {3, 9},
		"minecraft:quartz": {5, 12}, "minecraft:crying_obsidian": {1, 3}, "minecraft:leather": {2, 4},
		"minecraft:soul_sand": {2, 8}, "minecraft:netherbrick": {2, 8}, "minecraft:arrow": {6, 12},
		"minecraft:gravel": {8, 16}, "minecraft:blackstone": {8, 16},
	}
	for range n {
		out := Barter(r)
		if len(out) != 1 {
			t.Fatalf("roll gave %d stacks", len(out))
		}
		st := out[0]
		name, _ := st.Item().EncodeItem()
		switch it := st.Item().(type) {
		case item.Potion:
			name = "minecraft:potion"
			if it.Type == potion.FireResistance() {
				name = "minecraft:potion_fire"
			}
		case item.SplashPotion:
			name = "minecraft:splash_potion"
		}
		if rg, ok := ranges[name]; ok && (st.Count() < rg[0] || st.Count() > rg[1]) {
			t.Errorf("%v x%d out of %v", name, st.Count(), rg)
		}
		if name == "minecraft:enchanted_book" || name == "minecraft:iron_boots" {
			e, ok := st.Enchantment(enchantment.SoulSpeed)
			if !ok || e.Level() < 1 || e.Level() > 3 {
				t.Errorf("%v without soul speed I-III", name)
			}
		}
		got[name]++
	}
	want := map[string]int{
		"minecraft:enchanted_book": 5, "minecraft:iron_boots": 8, "minecraft:potion_fire": 8,
		"minecraft:splash_potion": 8, "minecraft:potion": 10, "minecraft:iron_nugget": 10,
		"minecraft:ender_pearl": 10, "minecraft:dried_ghast": 10, "minecraft:string": 20, "minecraft:quartz": 20,
		"minecraft:obsidian": 40, "minecraft:crying_obsidian": 40, "minecraft:fire_charge": 40,
		"minecraft:leather": 40, "minecraft:soul_sand": 40, "minecraft:netherbrick": 40, "minecraft:arrow": 40,
		"minecraft:gravel": 40, "minecraft:blackstone": 40,
	}
	for name, wt := range want {
		p := float64(wt) / 469
		exp := p * n
		sd := math.Sqrt(n * p * (1 - p))
		if math.Abs(float64(got[name])-exp) > 5*sd {
			t.Errorf("%v: %d times, want about %.0f", name, got[name], exp)
		}
	}
	if len(got) != len(want) {
		t.Errorf("items %v", got)
	}
}
