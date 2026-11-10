package enchantment

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// Breach is a mace enchantment (Bedrock id 40).
var Breach breach

type breach struct{}

// Name ...
func (breach) Name() string { return "Breach" }

// MaxLevel ...
func (breach) MaxLevel() int { return 4 }

// Cost ...
func (breach) Cost(level int) (int, int) { return 15 + (level-1)*9, 65 + (level-1)*9 }

// Rarity ...
func (breach) Rarity() item.EnchantmentRarity { return item.EnchantmentRarityRare }

// ArmourReduction returns the fraction of the target's armour a hit ignores at a level.
func (breach) ArmourReduction(level int) float64 { return 0.15 * float64(level) }

// CompatibleWithEnchantment ...
func (breach) CompatibleWithEnchantment(t item.EnchantmentType) bool {
	return t != Density && t != Smite && t != BaneOfArthropods
}

// CompatibleWithItem ...
func (breach) CompatibleWithItem(i world.Item) bool {
	_, ok := i.(item.Mace)
	return ok
}
