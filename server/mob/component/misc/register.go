//go:build crossfly_full

// Package misc holds the damage, effect, trigger and sensor components:
// fire/freezing immunity, mob effects, area attacks, attack cooldowns,
// knock back rules, entity and block sensors, on_hurt/on_death/
// on_friendly_anger triggers, the scheduler, genetics and a few flags. See
// README.md in server/mob/goal/misc.
package misc

import (
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

func init() {
	// Damage and effect rules.
	mob.RegisterComponent("minecraft:fire_immune", newFireImmune)
	mob.RegisterComponent("minecraft:freezing_immune", newFreezing)
	mob.RegisterComponent("minecraft:freezing_vulnerable", newFreezing)
	mob.RegisterComponent("minecraft:mob_effect_immunity", newEffectImmunity)
	mob.RegisterComponent("minecraft:cannot_be_attacked", newCannotBeAttacked)
	mob.RegisterComponent("minecraft:mob_effect", newMobEffect)
	mob.RegisterComponent("minecraft:spell_effects", newSpellEffects)
	mob.RegisterComponent("minecraft:area_attack", newAreaAttack)
	mob.RegisterComponent("minecraft:damage_over_time", newDamageOverTime)
	mob.RegisterComponent("minecraft:attack_cooldown", newAttackCooldown)
	mob.RegisterComponent("minecraft:apply_knockback_rules", newKnockbackRules)
	mob.RegisterComponent("minecraft:combat_regeneration", newCombatRegeneration)

	// Triggers and sensors.
	mob.RegisterComponent("minecraft:on_hurt", newOnHurt)
	mob.RegisterComponent("minecraft:on_hurt_by_player", newOnHurt)
	mob.RegisterComponent("minecraft:on_death", newOnDeath)
	mob.RegisterComponent("minecraft:on_friendly_anger", newOnFriendlyAnger)
	mob.RegisterComponent("minecraft:on_equipment_changed", newOnEquipmentChanged)
	mob.RegisterComponent("minecraft:entity_sensor", newEntitySensor)
	mob.RegisterComponent("minecraft:block_sensor", newBlockSensor)

	// State.
	mob.RegisterComponent("minecraft:genetics", newGenetics)
	mob.RegisterComponent("minecraft:strength", newStrength)
	mob.RegisterComponent("minecraft:is_stunned", newActorFlag(protocol.EntityDataFlagStunned, nil))
	mob.RegisterComponent("minecraft:is_ignited", newActorFlag(0, func(m *mob.Meta, v bool) { m.Ignited = v }))
	mob.RegisterComponent("minecraft:is_pregnant", newActorFlag(protocol.EntityDataFlagPregnant, nil))
	// Mobs never change dimension in Dragonfly (no portal travel for mobs).
	mob.RegisterComponent("minecraft:dimension_bound", newNop)

	// Event-firing work runs as systems (components may change while an
	// event is applied), in StageSensors after the combat sensors.
	sys := func(name string, order int, f func(m *mob.Mob)) {
		mob.RegisterSystem(mob.System{Name: "misc/" + name, Stage: mob.StageSensors, Order: 100 + order, Tick: f})
	}
	sys("fire_immune", 0, fireImmuneTick)
	sys("genetics", 1, func(m *mob.Mob) {
		if c, ok := mob.ComponentOf[*Genetics](m); ok {
			c.tick(m)
		}
	})
	sys("attack_cooldown", 2, func(m *mob.Mob) {
		if c, ok := mob.ComponentOf[*AttackCooldown](m); ok {
			c.tick(m)
		}
	})
	sys("entity_sensor", 3, func(m *mob.Mob) {
		if c, ok := mob.ComponentOf[*EntitySensor](m); ok {
			c.tick(m)
		}
	})
	sys("on_equipment_changed", 5, func(m *mob.Mob) {
		if c, ok := mob.ComponentOf[*OnEquipmentChanged](m); ok {
			c.tick(m)
		}
	})
	sys("combat_regeneration", 6, func(m *mob.Mob) {
		if c, ok := mob.ComponentOf[*CombatRegeneration](m); ok {
			c.tick(m)
		}
	})
	sys("friendly_anger", 7, friendlyAngerTick)
	// Blocks (lava, fire) set mobs burning in StageEnvironment: put a fire
	// immune mob out again right after.
	mob.RegisterSystem(mob.System{Name: "misc/fire_immune_env", Stage: mob.StageEnvironment, Tick: fireImmuneTick})
}
