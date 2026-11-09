package enchantment

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// Smite is an enchantment applied to a sword or axe that increases melee
// damage against undead mobs.
var Smite smite

type smite struct{}

// Name ...
func (smite) Name() string {
	return "Smite"
}

// MaxLevel ...
func (smite) MaxLevel() int {
	return 5
}

// Cost ...
func (smite) Cost(level int) (int, int) {
	// BDS MeleeWeaponEnchant::getMinCost and getMaxCost.
	minCost := 5 + (level-1)*8
	return minCost, minCost + 20
}

// Rarity ...
func (smite) Rarity() item.EnchantmentRarity {
	return item.EnchantmentRarityUncommon
}

// Addend returns the additional damage when attacking an undead target with
// smite: 2.5 per level (BDS MeleeWeaponEnchant::getDamageBonus).
func (smite) Addend(level int) float64 {
	return float64(level) * 2.5
}

// AffectsTarget reports whether smite adds damage against the target: only
// mobs of the "undead" type family (BDS getDamageBonus, Actor::hasFamily).
func (smite) AffectsTarget(target world.Entity) bool {
	return hasFamily(target, "undead")
}

// CompatibleWithEnchantment ...
func (smite) CompatibleWithEnchantment(t item.EnchantmentType) bool {
	return t != Sharpness && t != BaneOfArthropods
}

// CompatibleWithItem ...
func (smite) CompatibleWithItem(i world.Item) bool {
	return meleeDamageItem(i)
}
