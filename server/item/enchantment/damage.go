package enchantment

import (
	"math"

	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// AffectedDamageSource represents a world.DamageSource whose damage may be
// affected by an enchantment. A world.DamageSource does not need to implement
// AffectedDamageSource to let protection affect the damage. This happens
// depending on the (world.DamageSource).ReducedByResistance() method.
type AffectedDamageSource interface {
	world.DamageSource
	// AffectedByEnchantment specifies if a world.DamageSource is affected by
	// the item.EnchantmentType passed.
	AffectedByEnchantment(e item.EnchantmentType) bool
}

// DamageModifier is an item.EnchantmentType that can reduce damage through a
// modifier if an AffectedDamageSource returns true for it.
type DamageModifier interface {
	Modifier() float64
}

// ProtectionFactor calculates the combined protection factor for a slice of
// item.Enchantment. The factor depends on the world.DamageSource passed and is
// in a range of [0, 0.8], where 0.8 means incoming damage would be reduced by
// 80%.
func ProtectionFactor(src world.DamageSource, enchantments []item.Enchantment) float64 {
	f := 0.0
	for _, e := range enchantments {
		t := e.Type()
		modifier, ok := t.(DamageModifier)
		if !ok {
			continue
		}
		reduced := false
		if _, ok := t.(protection); ok && src.ReducedByResistance() {
			// Special case for protection, because it applies to all damage
			// sources by default, except those not reduced by resistance.
			reduced = true
		} else if asrc, ok := src.(AffectedDamageSource); ok && asrc.AffectedByEnchantment(t) {
			reduced = true
		}

		if reduced {
			f += float64(e.Level()) * modifier.Modifier()
		}
	}
	return math.Min(f, 0.8)
}

// TargetDamageBonus returns the extra melee damage the smite or bane of
// arthropods on held deals to target: 2.5 per level against an undead or an
// arthropod mob, 0 against anything else (BDS MeleeWeaponEnchant::
// getDamageBonus). Sharpness is not included: it applies to every target.
func TargetDamageBonus(held item.Stack, target world.Entity) float64 {
	d := 0.0
	if e, ok := held.Enchantment(Smite); ok && Smite.AffectsTarget(target) {
		d += Smite.Addend(e.Level())
	}
	if e, ok := held.Enchantment(BaneOfArthropods); ok && BaneOfArthropods.AffectsTarget(target) {
		d += BaneOfArthropods.Addend(e.Level())
	}
	return d
}

// SlowArthropod gives target the slowness of the bane of arthropods on held
// if target is a living arthropod (BDS MeleeWeaponEnchant::doPostAttack).
func SlowArthropod(held item.Stack, target world.Entity) {
	e, ok := held.Enchantment(BaneOfArthropods)
	if !ok || !BaneOfArthropods.AffectsTarget(target) {
		return
	}
	if d, ok := target.(interface{ Dead() bool }); ok && d.Dead() {
		return
	}
	if l, ok := target.(interface{ AddEffect(effect.Effect) }); ok {
		l.AddEffect(BaneOfArthropods.Slowness(e.Level()))
	}
}

// familyHolder is a target with Bedrock type families, such as a mob.
// Players have none, so they are never undead or arthropods.
type familyHolder interface {
	HasFamily(family string) bool
}

// hasFamily reports whether e has the type family passed.
func hasFamily(e world.Entity, family string) bool {
	f, ok := e.(familyHolder)
	return ok && f.HasFamily(family)
}

// meleeDamageItem reports whether the damage enchantments (sharpness, smite
// and bane of arthropods) can be applied to an item.
func meleeDamageItem(i world.Item) bool {
	t, ok := i.(item.Tool)
	if _, mace := i.(item.Mace); mace {
		return true
	}
	return ok && (t.ToolType() == item.TypeSword || t.ToolType() == item.TypeAxe || t.ToolType() == item.TypeSpear)
}
