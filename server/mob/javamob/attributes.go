//go:build crossfly_full

package javamob

import "math"

// Attribute is one of Java Edition's attributes (net.minecraft.world.entity.ai.attributes.Attributes), by its
// registry name without the namespace.
type Attribute string

// The attributes. Their defaults and ranges are in attributeInfos (Attributes.java, 26.3).
const (
	AttrAirDragModifier         Attribute = "air_drag_modifier"
	AttrArmor                   Attribute = "armor"
	AttrArmorToughness          Attribute = "armor_toughness"
	AttrAttackDamage            Attribute = "attack_damage"
	AttrAttackKnockback         Attribute = "attack_knockback"
	AttrAttackSpeed             Attribute = "attack_speed"
	AttrBounciness              Attribute = "bounciness"
	AttrBurningTime             Attribute = "burning_time"
	AttrExplosionKnockbackRes   Attribute = "explosion_knockback_resistance"
	AttrFallDamageMultiplier    Attribute = "fall_damage_multiplier"
	AttrFlyingSpeed             Attribute = "flying_speed"
	AttrFollowRange             Attribute = "follow_range"
	AttrFrictionModifier        Attribute = "friction_modifier"
	AttrGravity                 Attribute = "gravity"
	AttrJumpStrength            Attribute = "jump_strength"
	AttrKnockbackResistance     Attribute = "knockback_resistance"
	AttrMaxAbsorption           Attribute = "max_absorption"
	AttrMaxHealth               Attribute = "max_health"
	AttrMovementEfficiency      Attribute = "movement_efficiency"
	AttrMovementSpeed           Attribute = "movement_speed"
	AttrOxygenBonus             Attribute = "oxygen_bonus"
	AttrSafeFallDistance        Attribute = "safe_fall_distance"
	AttrScale                   Attribute = "scale"
	AttrSpawnReinforcements     Attribute = "spawn_reinforcements"
	AttrStepHeight              Attribute = "step_height"
	AttrTemptRange              Attribute = "tempt_range"
	AttrWaterMovementEfficiency Attribute = "water_movement_efficiency"
)

// attributeInfo is a RangedAttribute: the default value and the range values are clamped to.
type attributeInfo struct{ def, min, max float64 }

var attributeInfos = map[Attribute]attributeInfo{
	AttrAirDragModifier: {1, 0, 2048}, AttrArmor: {0, 0, 30}, AttrArmorToughness: {0, 0, 20},
	AttrAttackDamage: {2, 0, 2048}, AttrAttackKnockback: {0, 0, 5}, AttrAttackSpeed: {4, 0, 1024},
	AttrBounciness: {0, 0, 1}, AttrBurningTime: {1, 0, 1024}, AttrExplosionKnockbackRes: {0, 0, 1},
	AttrFallDamageMultiplier: {1, 0, 100}, AttrFlyingSpeed: {0.4, 0, 1024}, AttrFollowRange: {32, 0, 2048},
	AttrFrictionModifier: {1, 0, 2048}, AttrGravity: {0.08, -1, 1}, AttrJumpStrength: {0.42, 0, 32},
	AttrKnockbackResistance: {0, -2, 1}, AttrMaxAbsorption: {0, 0, 2048}, AttrMaxHealth: {20, 1, 1024},
	AttrMovementEfficiency: {0, 0, 1}, AttrMovementSpeed: {0.7, 0, 1024}, AttrOxygenBonus: {0, 0, 1024},
	AttrSafeFallDistance: {3, -1024, 1024}, AttrScale: {1, 0.0625, 16}, AttrSpawnReinforcements: {0, 0, 1},
	AttrStepHeight: {0.6, 0, 10}, AttrTemptRange: {10, 0, 2048}, AttrWaterMovementEfficiency: {0, 0, 1},
}

// ModifierOp is AttributeModifier.Operation.
type ModifierOp uint8

const (
	// OpAddValue adds the amount to the base.
	OpAddValue ModifierOp = iota
	// OpAddMultipliedBase adds the amount times the base (after OpAddValue).
	OpAddMultipliedBase
	// OpAddMultipliedTotal multiplies the total by 1 + the amount.
	OpAddMultipliedTotal
)

// Modifier is an AttributeModifier. ID identifies it ("minecraft:baby"); adding a modifier with the ID of one
// already on the attribute replaces it.
type Modifier struct {
	ID     string
	Amount float64
	Op     ModifierOp
	// Transient modifiers are not saved (addTransientModifier); the others are (addPermanentModifier).
	Transient bool
}

// AttributeInstance is one attribute of a mob: its base value and modifiers (AttributeInstance).
type AttributeInstance struct {
	attr      Attribute
	base      float64
	modifiers []Modifier
	value     float64
	dirty     bool
}

// Base is the base value.
func (a *AttributeInstance) Base() float64 { return a.base }

// SetBase sets the base value.
func (a *AttributeInstance) SetBase(v float64) { a.base, a.dirty = v, true }

// Modifier returns the modifier with the ID.
func (a *AttributeInstance) Modifier(id string) (Modifier, bool) {
	for _, m := range a.modifiers {
		if m.ID == id {
			return m, true
		}
	}
	return Modifier{}, false
}

// HasModifier reports whether a modifier with the ID is on the attribute.
func (a *AttributeInstance) HasModifier(id string) bool {
	_, ok := a.Modifier(id)
	return ok
}

// AddModifier adds m, replacing a modifier with the same ID (addOrReplacePermanentModifier,
// addOrUpdateTransientModifier).
func (a *AttributeInstance) AddModifier(m Modifier) {
	a.RemoveModifier(m.ID)
	a.modifiers = append(a.modifiers, m)
	a.dirty = true
}

// RemoveModifier removes the modifier with the ID and reports whether there was one.
func (a *AttributeInstance) RemoveModifier(id string) bool {
	for i, m := range a.modifiers {
		if m.ID == id {
			a.modifiers = append(a.modifiers[:i], a.modifiers[i+1:]...)
			a.dirty = true
			return true
		}
	}
	return false
}

// Modifiers returns the modifiers.
func (a *AttributeInstance) Modifiers() []Modifier { return a.modifiers }

// Value is the value with the modifiers applied, clamped to the attribute's range
// (AttributeInstance.calculateValue, RangedAttribute.sanitizeValue).
func (a *AttributeInstance) Value() float64 {
	if !a.dirty {
		return a.value
	}
	base := a.base
	for _, m := range a.modifiers {
		if m.Op == OpAddValue {
			base += m.Amount
		}
	}
	v := base
	for _, m := range a.modifiers {
		if m.Op == OpAddMultipliedBase {
			v += base * m.Amount
		}
	}
	for _, m := range a.modifiers {
		if m.Op == OpAddMultipliedTotal {
			v *= 1 + m.Amount
		}
	}
	info := attributeInfos[a.attr]
	if math.IsNaN(v) {
		v = info.min
	} else if info.max > info.min {
		v = min(max(v, info.min), info.max)
	}
	a.value, a.dirty = v, false
	return v
}

// Attributes is a mob's AttributeMap. Attributes the type's supplier does not list read as their default.
type Attributes struct {
	m map[Attribute]*AttributeInstance
}

// NewAttributes returns an attribute map with the bases given (an AttributeSupplier).
func NewAttributes(bases map[Attribute]float64) *Attributes {
	a := &Attributes{m: make(map[Attribute]*AttributeInstance, len(bases))}
	for k, v := range bases {
		a.m[k] = &AttributeInstance{attr: k, base: v, dirty: true}
	}
	return a
}

// Has reports whether the mob has the attribute (AttributeMap.hasAttribute).
func (a *Attributes) Has(attr Attribute) bool { _, ok := a.m[attr]; return ok }

// Instance returns the attribute, adding it with its default if the mob does not have it yet.
func (a *Attributes) Instance(attr Attribute) *AttributeInstance {
	if i, ok := a.m[attr]; ok {
		return i
	}
	i := &AttributeInstance{attr: attr, base: attributeInfos[attr].def, dirty: true}
	a.m[attr] = i
	return i
}

// Value is the current value of the attribute (its default if the mob does not have it).
func (a *Attributes) Value(attr Attribute) float64 {
	if i, ok := a.m[attr]; ok {
		return i.Value()
	}
	return attributeInfos[attr].def
}

// Base is the base value of the attribute.
func (a *Attributes) Base(attr Attribute) float64 {
	if i, ok := a.m[attr]; ok {
		return i.base
	}
	return attributeInfos[attr].def
}

// LivingAttributes are LivingEntity.createLivingAttributes plus Mob.createMobAttributes (follow range 16): the
// bases every Java mob starts from. A type adds its own in Type.Attributes.
func LivingAttributes() map[Attribute]float64 {
	m := map[Attribute]float64{}
	for _, k := range []Attribute{AttrMaxHealth, AttrKnockbackResistance, AttrMovementSpeed, AttrArmor, AttrArmorToughness,
		AttrMaxAbsorption, AttrStepHeight, AttrScale, AttrGravity, AttrSafeFallDistance, AttrFallDamageMultiplier,
		AttrJumpStrength, AttrOxygenBonus, AttrBurningTime, AttrExplosionKnockbackRes, AttrWaterMovementEfficiency,
		AttrMovementEfficiency, AttrAttackKnockback, AttrAirDragModifier, AttrFrictionModifier} {
		m[k] = attributeInfos[k].def
	}
	m[AttrFollowRange] = 16
	return m
}

// MonsterAttributes are Monster.createMonsterAttributes: LivingAttributes with attack damage.
func MonsterAttributes() map[Attribute]float64 {
	m := LivingAttributes()
	m[AttrAttackDamage] = attributeInfos[AttrAttackDamage].def
	return m
}
