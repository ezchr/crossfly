//go:build crossfly_full

package loot

import (
	"sync"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// Looting is the Looting enchantment: more mob loot (looting_enchant,
// random_chance_with_looting) and a 1% higher equipment drop chance per
// level (BDS Mob::dropEquipmentOnDeath). Dragonfly does not register
// enchantment 14; this package does, unless something else already did.
var Looting looting

type looting struct{}

// LootingID is the Bedrock enchantment id of Looting.
const LootingID = 14

var enableLooting sync.Once

func registerLooting() {
	if _, ok := item.EnchantmentByID(LootingID); !ok {
		item.RegisterEnchantment(LootingID, Looting)
	}
}

// Name ...
func (looting) Name() string { return "Looting" }

// MaxLevel ...
func (looting) MaxLevel() int { return 3 }

// Cost ...
func (looting) Cost(level int) (int, int) {
	minCost := 15 + (level-1)*9
	return minCost, minCost + 50
}

// Rarity ...
func (looting) Rarity() item.EnchantmentRarity { return item.EnchantmentRarityRare }

// CompatibleWithEnchantment ...
func (looting) CompatibleWithEnchantment(item.EnchantmentType) bool { return true }

// CompatibleWithItem ...
func (looting) CompatibleWithItem(i world.Item) bool {
	t, ok := i.(item.Tool)
	return ok && t.ToolType() == item.TypeSword
}

// LootingLevel returns the highest Looting level of the stacks (BDS takes
// the highest over the killer's hand items).
func LootingLevel(stacks ...item.Stack) int {
	t, ok := item.EnchantmentByID(LootingID)
	if !ok {
		return 0
	}
	lvl := 0
	for _, s := range stacks {
		if s.Empty() {
			continue
		}
		if e, ok := s.Enchantment(t); ok && e.Level() > lvl {
			lvl = e.Level()
		}
	}
	return lvl
}

// HandLooting returns the Looting level of what e holds (0 if e holds
// nothing or is not an item.Carrier).
func HandLooting(e world.Entity) int {
	if e == nil {
		return 0
	}
	c, ok := e.(item.Carrier)
	if !ok {
		return 0
	}
	main, off := c.HeldItems()
	return LootingLevel(main, off)
}
