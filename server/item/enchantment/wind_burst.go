package enchantment

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// WindBurst is a mace enchantment (Bedrock id 38).
var WindBurst windBurst

type windBurst struct{}

// Name ...
func (windBurst) Name() string { return "Wind Burst" }

// MaxLevel ...
func (windBurst) MaxLevel() int { return 3 }

// Cost ...
func (windBurst) Cost(level int) (int, int) { return 15 + (level-1)*9, 65 + (level-1)*9 }

// Rarity ...
func (windBurst) Rarity() item.EnchantmentRarity { return item.EnchantmentRarityRare }

// Multiplier returns the strength of the burst at a level: 1.25, 1.8 and 2.25, then 0.35 more per level.
func (windBurst) Multiplier(level int) float64 {
	switch level {
	case 1:
		return 1.25
	case 2:
		return 1.8
	case 3:
		return 2.25
	}
	return 2.25 + 0.35*float64(level-3)
}

// CompatibleWithEnchantment ...
func (windBurst) CompatibleWithEnchantment(item.EnchantmentType) bool { return true }

// CompatibleWithItem ...
func (windBurst) CompatibleWithItem(i world.Item) bool {
	_, ok := i.(item.Mace)
	return ok
}
