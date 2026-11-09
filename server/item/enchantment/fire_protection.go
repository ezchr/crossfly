package enchantment

import (
	"math"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// FireProtection is an armour enchantment that decreases fire damage.
var FireProtection fireProtection

type fireProtection struct{}

// Name ...
func (fireProtection) Name() string {
	return "Fire Protection"
}

// MaxLevel ...
func (fireProtection) MaxLevel() int {
	return 4
}

// Cost ...
func (fireProtection) Cost(level int) (int, int) {
	minCost := 10 + (level-1)*8
	return minCost, minCost + 8
}

// Rarity ...
func (fireProtection) Rarity() item.EnchantmentRarity {
	return item.EnchantmentRarityUncommon
}

// Modifier returns the base protection modifier for the enchantment.
func (fireProtection) Modifier() float64 {
	return 0.08
}

// BurnTicks returns for how many ticks an entity burns that is set on fire for ticks while the Fire
// Protection levels of its armour add up to totalLevel: 15% shorter per level, rounded, and not at
// all from 7 levels on (Bedrock Edition's OnFireSystem, which adds up the levels of every piece).
func (fireProtection) BurnTicks(ticks int64, totalLevel int) int64 {
	if totalLevel <= 0 {
		return ticks
	}
	f := min(max(1-0.15*float64(totalLevel), 0), 1)
	return int64(math.Round(float64(ticks) * f))
}

// CompatibleWithEnchantment ...
func (fireProtection) CompatibleWithEnchantment(t item.EnchantmentType) bool {
	return t != BlastProtection && t != ProjectileProtection && t != Protection
}

// CompatibleWithItem ...
func (fireProtection) CompatibleWithItem(i world.Item) bool {
	_, ok := i.(item.Armour)
	return ok
}
