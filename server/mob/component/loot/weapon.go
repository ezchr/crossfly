//go:build crossfly_full

package loot

import (
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
)

// WeaponDamageTo is the damage a held weapon adds to a mob's melee hit on target
// (BDS Mob::doHurtTarget: int(item attack damage + EnchantUtils::
// getMeleeDamageBonus)). Dragonfly's AttackDamage counts the bare hand's 1,
// which BDS's item damage does not. Sharpness adds 1.25 per level, Smite 2.5
// per level against undead targets and Bane of Arthropods 2.5 per level
// against arthropods.
func WeaponDamageTo(held item.Stack, target world.Entity) float64 {
	if held.Empty() {
		return 0
	}
	d := math.Max(0, held.AttackDamage()-1)
	if e, ok := held.Enchantment(enchantment.Sharpness); ok {
		d += enchantment.Sharpness.Addend(e.Level())
	}
	d += enchantment.TargetDamageBonus(held, target)
	return math.Floor(d)
}

// WeaponDamage is WeaponDamageTo against a target that is neither undead nor
// an arthropod, such as a player.
func WeaponDamage(held item.Stack) float64 {
	return WeaponDamageTo(held, nil)
}

// KnockbackBonus is the extra knock back force of the held weapon's
// Knockback enchantment (BDS doHurtTarget's KnockbackRules::doKnockbackAttack
// for enchant 12).
func KnockbackBonus(held item.Stack) float64 {
	if e, ok := held.Enchantment(enchantment.Knockback); ok {
		return enchantment.Knockback.Force(e.Level())
	}
	return 0
}

// PostHit applies the held weapon's after-hit enchantments to the target
// (BDS EnchantUtils::doPostDamageEffects): Bane of Arthropods slows an
// arthropod, Fire Aspect sets the target on fire for 4 seconds per level.
func PostHit(held item.Stack, target world.Entity) {
	enchantment.SlowArthropod(held, target)
	e, ok := held.Enchantment(enchantment.FireAspect)
	if !ok {
		return
	}
	if f, ok := target.(interface {
		OnFireDuration() time.Duration
		SetOnFire(time.Duration)
	}); ok {
		if d := enchantment.FireAspect.Duration(e.Level()); d > f.OnFireDuration() {
			f.SetOnFire(d)
		}
	}
}
