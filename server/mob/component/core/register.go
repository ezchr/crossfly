//go:build crossfly_full

package core

import "github.com/df-mc/dragonfly/server/mob"

func init() {
	// Attributes.
	mob.RegisterComponent("minecraft:health", newHealth)
	mob.RegisterComponent("minecraft:movement", newAttributeFactory(mob.AttrMovement))
	mob.RegisterComponent("minecraft:underwater_movement", newAttributeFactory(mob.AttrUnderwaterMovement))
	mob.RegisterComponent("minecraft:lava_movement", newAttributeFactory(mob.AttrLavaMovement))
	mob.RegisterComponent("minecraft:flying_speed", newAttributeFactory(mob.AttrFlyingSpeed))
	mob.RegisterComponent("minecraft:follow_range", newAttributeFactory(mob.AttrFollowRange))
	mob.RegisterComponent("minecraft:knockback_resistance", newAttributeFactory(mob.AttrKnockbackResistance))
	mob.RegisterComponent("minecraft:attack_damage", newAttributeFactory(mob.AttrAttackDamage))
	mob.RegisterComponent("minecraft:horse.jump_strength", newAttributeFactory(mob.AttrJumpStrength))
	mob.RegisterComponent("minecraft:attack", newAttack)

	// Shape and identity.
	mob.RegisterComponent("minecraft:type_family", newTypeFamily)
	mob.RegisterComponent("minecraft:collision_box", newCollisionBox)
	mob.RegisterComponent("minecraft:scale", newScale)

	// Client flags and values.
	mob.RegisterComponent("minecraft:is_baby", newFlag(func(m *mob.Meta, v bool) { m.Baby = v }))
	mob.RegisterComponent("minecraft:is_sheared", newFlag(func(m *mob.Meta, v bool) { m.Sheared = v }))
	mob.RegisterComponent("minecraft:is_saddled", newFlag(func(m *mob.Meta, v bool) { m.Saddled = v }))
	mob.RegisterComponent("minecraft:is_shaking", newFlag(func(m *mob.Meta, v bool) { m.Shaking = v }))
	mob.RegisterComponent("minecraft:is_tamed", newFlag(func(m *mob.Meta, v bool) { m.Tamed = v }))
	mob.RegisterComponent("minecraft:is_chested", newFlag(func(m *mob.Meta, v bool) { m.Chested = v }))
	mob.RegisterComponent("minecraft:is_charged", newFlag(func(m *mob.Meta, v bool) { m.Charged = v }))
	mob.RegisterComponent("minecraft:variant", newIntValue(setVariant))
	mob.RegisterComponent("minecraft:mark_variant", newIntValue(setMarkVariant))
	mob.RegisterComponent("minecraft:skin_id", newIntValue(setSkinID))
	mob.RegisterComponent("minecraft:color", newIntValue(setColor))
	mob.RegisterComponent("minecraft:color2", newIntValue(setColor2))

	// Nothing to do server-side, or stubs.
	mob.RegisterComponent("minecraft:is_hidden_when_invisible", newNop) // client-side
	mob.RegisterComponent("minecraft:leashable_to", newNop)             // stub
	mob.RegisterComponent("minecraft:balloonable", newNop)              // stub: balloons are education edition

	// Life cycle.
	mob.RegisterComponent("minecraft:ageable", newAgeable)
	mob.RegisterComponent("minecraft:breedable", newBreedable)
	mob.RegisterComponent("minecraft:offspring", newOffspring)
	mob.RegisterComponent("minecraft:interact", newInteract)
	mob.RegisterComponent("minecraft:loot", newLoot)
	mob.RegisterComponent("minecraft:experience_reward", newExperienceReward)
	mob.RegisterComponent("minecraft:equipment", newEquipment)
	mob.RegisterComponent("minecraft:spawn_entity", newSpawnEntity)
	mob.RegisterComponent("minecraft:timer", newTimer)
	mob.RegisterComponent("minecraft:ambient_sound_interval", newAmbientSound)
	mob.RegisterComponent("minecraft:hurt_on_condition", newHurtOnCondition)
	mob.RegisterComponent("minecraft:breathable", newBreathable)
}
