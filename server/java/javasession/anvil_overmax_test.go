package javasession

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/session"
)

// TestAnvilOverMax: the anvil of a Java player gives the result and the cost of a Bedrock player's
// (session/anvil_rules_test.go), without session.AnvilRules and with them.
func TestAnvilOverMax(t *testing.T) {
	testServer()
	defer session.SetAnvilRules(session.AnvilRules{})
	sharp, eff := enchantment.Sharpness, enchantment.Efficiency
	e := item.NewEnchantment
	sword := func(en ...item.Enchantment) item.Stack {
		return stack(item.Sword{Tier: item.ToolTierDiamond}, 1).WithEnchantments(en...)
	}
	book := func(en ...item.Enchantment) item.Stack { return stack(item.EnchantedBook{}, 1).WithEnchantments(en...) }
	pick := stack(item.Pickaxe{Tier: item.ToolTierDiamond}, 1).WithEnchantments(e(enchantment.Unbreaking, 3), e(enchantment.Fortune, 3)).WithAnvilCost(3)

	for _, c := range []struct {
		name            string
		input, material item.Stack
		t               item.EnchantmentType
		// The level of t in the result and the cost without rules and with them; level 0: no result.
		level, cost, ruleLevel, ruleCost int
	}{
		{"item without the enchantment + above-max book", sword(), book(e(sharp, 10)), sharp, 10, 10, 10, 10},
		{"lower level below max + above-max book", sword(e(sharp, 3)), book(e(sharp, 10)), sharp, 10, 7, 10, 10},
		{"lower level below max + book at max", sword(e(sharp, 3)), book(e(sharp, 5)), sharp, 5, 2, 5, 2},
		{"equal levels below max", sword(e(sharp, 4)), book(e(sharp, 4)), sharp, 5, 1, 5, 1},
		{"item at max + above-max book", sword(e(sharp, 5)), book(e(sharp, 10)), sharp, 10, 5, 10, 10},
		{"item above max + higher book", sword(e(sharp, 7)), book(e(sharp, 10)), sharp, 10, 3, 10, 10},
		{"item above max + lower book", sword(e(sharp, 10)), book(e(sharp, 7)), sharp, 0, 0, 0, 0},
		{"two equal levels above max", sword(e(sharp, 10)), book(e(sharp, 10)), sharp, 0, 0, 0, 0},
		{"two items, both above max", sword(e(sharp, 7)), sword(e(sharp, 10)), sharp, 10, 3, 10, 10},
		{"two items, equal above max", sword(e(sharp, 10)), sword(e(sharp, 10)), sharp, 0, 0, 0, 0},
		{"reported pickaxe: Efficiency 5 + Efficiency 10 book", pick.WithEnchantments(e(eff, 5)), book(e(eff, 10)), eff, 10, 5 + 3, 10, 10},
		{"reported pickaxe with Efficiency 4", pick.WithEnchantments(e(eff, 4)), book(e(eff, 10)), eff, 10, 6 + 3, 10, 10},
		{"reported pickaxe without Efficiency", pick, book(e(eff, 10)), eff, 10, 10 + 3, 10, 10},
	} {
		for _, rules := range []bool{false, true} {
			session.SetAnvilRules(session.AnvilRules{OverMax: rules, NoPriorWork: rules})
			wantLevel, wantCost := c.level, c.cost
			if rules {
				wantLevel, wantCost = c.ruleLevel, c.ruleCost
			}
			res, cost, _ := anvilResult(c.input, c.material, nil, false, nil)
			if wantLevel == 0 {
				if !res.Empty() {
					t.Errorf("%s (rules %v): got %v, want nothing", c.name, rules, res.Enchantments())
				}
				continue
			}
			got, _ := res.Enchantment(c.t)
			if res.Empty() || got.Level() != wantLevel || cost != wantCost {
				t.Errorf("%s (rules %v): level %d for %d levels, want %d for %d", c.name, rules, got.Level(), cost, wantLevel, wantCost)
				continue
			}
			for _, in := range c.input.Enchantments() {
				if out, _ := res.Enchantment(in.Type()); out.Level() < in.Level() {
					t.Errorf("%s (rules %v): %s went from %d to %d", c.name, rules, in.Type().Name(), in.Level(), out.Level())
				}
			}
			if want := max(c.input.AnvilCost(), c.material.AnvilCost())*2 + 1; res.AnvilCost() != want {
				t.Errorf("%s (rules %v): result counts %d anvil uses, want %d", c.name, rules, res.AnvilCost(), want)
			}
		}
	}

	// A worn item is not too expensive without the prior work penalty, and is without the rules.
	worn := sword().WithAnvilCost(31)
	session.SetAnvilRules(session.AnvilRules{})
	if res, cost, _ := anvilResult(worn, book(e(sharp, 10)), nil, false, nil); !res.Empty() || cost < 40 {
		t.Errorf("31 uses + Sharpness 10 book: %v for %d levels, want too expensive", res, cost)
	}
	session.SetAnvilRules(session.AnvilRules{NoPriorWork: true})
	if res, cost, _ := anvilResult(worn, book(e(sharp, 10)), nil, false, nil); res.Empty() || cost != 10 || res.AnvilCost() != 63 {
		t.Errorf("31 uses + Sharpness 10 book without prior work: %v for %d levels", res, cost)
	}
}
