package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
)

// Trades that take an enchanted book must tell books apart by their enchantments.
func TestTradeEnchantedBookInput(t *testing.T) {
	book := func(e ...item.Enchantment) item.Stack {
		return item.NewStack(item.EnchantedBook{}, 1).WithEnchantments(e...)
	}
	want := book(item.NewEnchantment(enchantment.Efficiency, 6))
	for _, c := range []struct {
		have item.Stack
		ok   bool
	}{
		{book(item.NewEnchantment(enchantment.Efficiency, 6)), true},
		{book(item.NewEnchantment(enchantment.Efficiency, 5)), false},
		{book(item.NewEnchantment(enchantment.Fortune, 4)), false},
		{item.NewStack(item.Book{}, 1), false},
	} {
		if got := tradeInputMatches(c.have, want, 1); got != c.ok {
			t.Errorf("%v for %v: got %v, want %v", c.have, want, got, c.ok)
		}
	}
	if got := tradeEnchantNames(want); got != "Efficiency VI" {
		t.Errorf("names: got %q", got)
	}
	for n, s := range map[int]string{1: "I", 4: "IV", 9: "IX", 10: "X", 39: "XXXIX"} {
		if got := romanNumeral(n); got != s {
			t.Errorf("roman %d: got %q, want %q", n, got, s)
		}
	}
}
