package enchantment

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// LuckOfTheSea is an enchantment for fishing rods that raises the luck of the fishing loot (BDS
// FishingHook::retrieve: luck plus the level). Fishing in water is not implemented, so it has no effect yet.
var LuckOfTheSea luckOfTheSea

type luckOfTheSea struct{}

// Name ...
func (luckOfTheSea) Name() string {
	return "Luck of the Sea"
}

// MaxLevel ...
func (luckOfTheSea) MaxLevel() int {
	return 3
}

// Cost ...
func (luckOfTheSea) Cost(level int) (int, int) {
	return fishingCost(level)
}

// Rarity ...
func (luckOfTheSea) Rarity() item.EnchantmentRarity {
	return item.EnchantmentRarityRare
}

// CompatibleWithEnchantment ...
func (luckOfTheSea) CompatibleWithEnchantment(item.EnchantmentType) bool {
	return true
}

// CompatibleWithItem ...
func (luckOfTheSea) CompatibleWithItem(i world.Item) bool {
	_, ok := i.(item.FishingRod)
	return ok
}
