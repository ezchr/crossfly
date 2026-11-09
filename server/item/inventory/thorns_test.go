package inventory

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
)

func thornsArmour(helmet, chest int) *Armour {
	a := NewArmour(nil)
	h := item.NewStack(item.Helmet{Tier: item.ArmourTierDiamond{}}, 1)
	if helmet > 0 {
		h = h.WithEnchantments(item.NewEnchantment(enchantment.Thorns, helmet))
	}
	c := item.NewStack(item.Chestplate{Tier: item.ArmourTierDiamond{}}, 1)
	if chest > 0 {
		c = c.WithEnchantments(item.NewEnchantment(enchantment.Thorns, chest))
	}
	a.SetHelmet(h)
	a.SetChestplate(c)
	a.SetBoots(item.NewStack(item.Boots{Tier: item.ArmourTierDiamond{}}, 1))
	return a
}

func damageFunc(s item.Stack, d int) item.Stack { return s.Damage(d) }

func TestThornsAboveLevelTen(t *testing.T) {
	// Thorns XI and up always activate and deal level-10 damage; the item loses 3 durability.
	a := thornsArmour(0, 15)
	maxDur := a.Chestplate().MaxDurability()
	if dmg := a.ThornsDamage(damageFunc); dmg != 5 {
		t.Fatalf("Thorns XV dealt %v, want 5", dmg)
	}
	if got := maxDur - a.Chestplate().Durability(); got != 3 {
		t.Errorf("Thorns XV chestplate lost %d durability, want 3", got)
	}
	if a.Helmet().Durability() != a.Helmet().MaxDurability() || a.Boots().Durability() != a.Boots().MaxDurability() {
		t.Errorf("armour without Thorns lost durability")
	}
	// The highest of the pieces that activate counts: Thorns 100 deals 90.
	if dmg := thornsArmour(100, 12).ThornsDamage(damageFunc); dmg != 90 {
		t.Errorf("Thorns C + XII dealt %v, want 90", dmg)
	}
}

func TestThornsLevelsSevenToTen(t *testing.T) {
	// From VII on (105%) Thorns always activates, dealing 1-4.
	for range 200 {
		a := thornsArmour(0, 8)
		maxDur := a.Chestplate().MaxDurability()
		if dmg := a.ThornsDamage(damageFunc); dmg < 1 || dmg > 4 {
			t.Fatalf("Thorns VIII dealt %v, want 1-4", dmg)
		}
		if got := maxDur - a.Chestplate().Durability(); got != 3 {
			t.Fatalf("Thorns VIII chestplate lost %d durability, want 3", got)
		}
	}
}

func TestThornsLowLevel(t *testing.T) {
	activated := 0
	const n = 20000
	for range n {
		a := thornsArmour(0, 1)
		maxDur := a.Chestplate().MaxDurability()
		dmg := a.ThornsDamage(damageFunc)
		lost := maxDur - a.Chestplate().Durability()
		switch {
		case dmg == 0 && lost == 1:
		case dmg >= 1 && dmg <= 4 && lost == 3:
			activated++
		default:
			t.Fatalf("Thorns I: damage %v, durability lost %d", dmg, lost)
		}
	}
	if f := float64(activated) / n; f < 0.13 || f > 0.17 {
		t.Errorf("Thorns I activated %.3f of the time, want 0.15", f)
	}
}

func TestTotalEnchantmentLevel(t *testing.T) {
	a := thornsArmour(3, 20)
	if got := a.TotalEnchantmentLevel(enchantment.Thorns); got != 23 {
		t.Errorf("TotalEnchantmentLevel = %d, want 23", got)
	}
	if got := a.TotalEnchantmentLevel(enchantment.Protection); got != 0 {
		t.Errorf("TotalEnchantmentLevel(Protection) = %d, want 0", got)
	}
}
