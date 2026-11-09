package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
)

// Two equal levels combine into one level higher only below the maximum level: at or past it (an
// item from /enchant) the anvil leaves the level alone, as Bedrock's ItemEnchants::canEnchant does.
func TestAnvilMergeLevelsAboveMax(t *testing.T) {
	sword := func(lvl int) item.Stack {
		return item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1).WithEnchantments(item.NewEnchantment(enchantment.Sharpness, lvl))
	}
	book := func(lvl int) item.Stack {
		return item.NewStack(item.EnchantedBook{}, 1).WithEnchantments(item.NewEnchantment(enchantment.Sharpness, lvl))
	}
	for _, c := range []struct {
		input, material int
		want            int
		merged          bool
	}{
		{3, 3, 4, true},
		{4, 4, 5, true},
		{5, 5, 5, false},
		{10, 10, 10, false},
		{10, 6, 10, false},
		{3, 10, 10, true},
	} {
		in := sword(c.input)
		res, _, skipped, _ := mergeEnchantments(in, book(c.material), in, 0, true)
		e, _ := res.Enchantment(enchantment.Sharpness)
		if e.Level() != c.want || skipped == c.merged {
			t.Errorf("Sharpness %d + book %d: level %d (merged %v), want %d (merged %v)", c.input, c.material, e.Level(), !skipped, c.want, c.merged)
		}
	}
}
