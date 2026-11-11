package enchantment

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// Lure is an enchantment for fishing rods that makes fish bite sooner (BDS FishingHook::_serverLured: 5 seconds less
// to wait per level). Fishing in water is not implemented, so it has no effect yet.
var Lure lure

type lure struct{}

// Name ...
func (lure) Name() string {
	return "Lure"
}

// MaxLevel ...
func (lure) MaxLevel() int {
	return 3
}

// Cost ...
func (lure) Cost(level int) (int, int) {
	return fishingCost(level)
}

// Rarity ...
func (lure) Rarity() item.EnchantmentRarity {
	return item.EnchantmentRarityRare
}

// CompatibleWithEnchantment ...
func (lure) CompatibleWithEnchantment(item.EnchantmentType) bool {
	return true
}

// CompatibleWithItem ...
func (lure) CompatibleWithItem(i world.Item) bool {
	_, ok := i.(item.FishingRod)
	return ok
}

// fishingCost is the enchanting cost of Lure and Luck of the Sea (BDS FishingEnchant::getMinCost 9*level+6, and
// Enchant::getMaxCost, which FishingEnchant keeps: the minimum plus 5).
func fishingCost(level int) (int, int) {
	minCost := 9*level + 6
	return minCost, minCost + 5
}
