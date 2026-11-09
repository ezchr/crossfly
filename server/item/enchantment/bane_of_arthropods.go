package enchantment

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// BaneOfArthropods is an enchantment applied to a sword or axe that increases
// melee damage against arthropods and slows them down.
var BaneOfArthropods baneOfArthropods

type baneOfArthropods struct{}

// Name ...
func (baneOfArthropods) Name() string {
	return "Bane of Arthropods"
}

// MaxLevel ...
func (baneOfArthropods) MaxLevel() int {
	return 5
}

// Cost ...
func (baneOfArthropods) Cost(level int) (int, int) {
	// BDS MeleeWeaponEnchant::getMinCost and getMaxCost.
	minCost := 5 + (level-1)*8
	return minCost, minCost + 20
}

// Rarity ...
func (baneOfArthropods) Rarity() item.EnchantmentRarity {
	return item.EnchantmentRarityUncommon
}

// Addend returns the additional damage when attacking an arthropod with bane
// of arthropods: 2.5 per level (BDS MeleeWeaponEnchant::getDamageBonus).
func (baneOfArthropods) Addend(level int) float64 {
	return float64(level) * 2.5
}

// AffectsTarget reports whether bane of arthropods adds damage against, and
// slows, the target: only mobs of the "arthropod" type family (BDS
// MeleeWeaponEnchant::getDamageBonus and doPostAttack, Actor::hasFamily).
func (baneOfArthropods) AffectsTarget(target world.Entity) bool {
	return hasFamily(target, "arthropod")
}

// SlownessDuration returns how long an arthropod hit with bane of arthropods
// is slowed: 20 + rand(10*level) ticks, so 1 to 1.5 seconds at level 1 and up
// to 0.5 seconds more per level (BDS MeleeWeaponEnchant::doPostAttack).
func (baneOfArthropods) SlownessDuration(level int) time.Duration {
	ticks := 20
	if n := 10 * level; n > 0 {
		ticks += rand.IntN(n)
	}
	return time.Duration(ticks) * time.Second / 20
}

// Slowness returns the slowness an arthropod hit with bane of arthropods gets.
// BDS 1.21.3 and 26.60 both build it as MobEffectInstance(slowness, duration)
// with amplifier 0: Slowness I (Java gives Slowness IV).
func (b baneOfArthropods) Slowness(level int) effect.Effect {
	return effect.New(effect.Slowness, 1, b.SlownessDuration(level))
}

// CompatibleWithEnchantment ...
func (baneOfArthropods) CompatibleWithEnchantment(t item.EnchantmentType) bool {
	return t != Sharpness && t != Smite
}

// CompatibleWithItem ...
func (baneOfArthropods) CompatibleWithItem(i world.Item) bool {
	return meleeDamageItem(i)
}
