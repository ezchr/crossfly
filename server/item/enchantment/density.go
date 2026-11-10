package enchantment

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// Density is a mace enchantment (Bedrock id 39).
var Density density

type density struct{}

// Name ...
func (density) Name() string { return "Density" }

// MaxLevel ...
func (density) MaxLevel() int { return 5 }

// Cost ...
func (density) Cost(level int) (int, int) { return 5 + (level-1)*8, 25 + (level-1)*8 }

// Rarity ...
func (density) Rarity() item.EnchantmentRarity { return item.EnchantmentRarityUncommon }

// Addend returns the damage added per block fallen at a level.
func (density) Addend(level int) float64 { return 0.5 * float64(level) }

// CompatibleWithEnchantment ...
func (density) CompatibleWithEnchantment(t item.EnchantmentType) bool {
	return t != Breach && t != Smite && t != BaneOfArthropods
}

// CompatibleWithItem ...
func (density) CompatibleWithItem(i world.Item) bool {
	_, ok := i.(item.Mace)
	return ok
}
