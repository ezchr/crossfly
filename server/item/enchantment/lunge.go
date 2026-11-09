package enchantment

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// Lunge is a spear enchantment that propels the wielder forward on every jab attack, hit or miss, at the
// cost of exhaustion scaling with its level and one point of durability. It does not trigger on a charge
// attack, while riding, gliding or in water, or for a player in survival with too little food.
var Lunge lunge

type lunge struct{}

// Name ...
func (lunge) Name() string {
	return "Lunge"
}

// MaxLevel ...
func (lunge) MaxLevel() int {
	return 3
}

// Cost ...
func (lunge) Cost(level int) (int, int) {
	minCost := 5 + (level-1)*8
	return minCost, minCost + 20
}

// Rarity ...
func (lunge) Rarity() item.EnchantmentRarity {
	return item.EnchantmentRarityUncommon
}

// Speed returns the horizontal speed, in blocks/tick, that a jab attack with this level of Lunge adds
// along the direction the wielder looks in. Looking up or down shortens it: only the horizontal part of
// the look direction counts.
func (lunge) Speed(level int) float64 {
	return 0.458 * float64(level)
}

// ExhaustionCost returns the exhaustion applied by triggering Lunge at this level: 4 per level, which is
// one food point per level. No food is taken directly.
func (lunge) ExhaustionCost(level int) float64 {
	return 4 * float64(level)
}

// DurabilityCost returns the durability a spear loses each time its Lunge triggers.
func (lunge) DurabilityCost() int {
	return 1
}

// MinimumFood returns the food level a player outside creative needs for Lunge to trigger: more than 6.
func (lunge) MinimumFood() int {
	return 7
}

// CompatibleWithEnchantment ...
func (lunge) CompatibleWithEnchantment(item.EnchantmentType) bool {
	return true
}

// CompatibleWithItem ...
func (lunge) CompatibleWithItem(i world.Item) bool {
	_, ok := i.(item.Spear)
	return ok
}
