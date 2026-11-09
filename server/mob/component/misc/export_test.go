//go:build crossfly_full

package misc

import (
	"encoding/json"

	"github.com/df-mc/dragonfly/server/mob"
)

// Factories are the component factories by key, for the external tests.
var Factories = map[string]mob.ComponentFactory{
	"minecraft:area_attack":           newAreaAttack,
	"minecraft:mob_effect":            newMobEffect,
	"minecraft:damage_over_time":      newDamageOverTime,
	"minecraft:attack_cooldown":       newAttackCooldown,
	"minecraft:entity_sensor":         newEntitySensor,
	"minecraft:block_sensor":          newBlockSensor,
	"minecraft:combat_regeneration":   newCombatRegeneration,
	"minecraft:strength":              newStrength,
	"minecraft:genetics":              newGenetics,
	"minecraft:apply_knockback_rules": newKnockbackRules,
}

// Build builds a component from JSON.
func Build(key, raw string) (mob.Component, error) {
	return Factories[key](key, json.RawMessage(raw))
}

// Params returns the parsed values of a component for default checks.
func Params(c mob.Component) map[string]any {
	switch c := c.(type) {
	case *AreaAttack:
		return map[string]any{"range": c.rng, "damage": c.damage, "cooldown": c.cooldown, "cause": c.cause, "sound": c.sound}
	case *MobEffect:
		return map[string]any{"range": c.rng, "duration": c.duration, "cooldown": c.cooldown, "ambient": c.ambient}
	case *DamageOverTime:
		return map[string]any{"damage": c.damage, "interval": c.interval}
	case *AttackCooldown:
		return map[string]any{"min": c.time.Min, "max": c.time.Max}
	case *EntitySensor:
		s := c.subs[0]
		return map[string]any{"relative": c.relative, "players": c.playersOnly, "h": s.h, "v": s.v, "min": s.min, "max": s.max, "cooldown": s.cooldown, "subs": len(c.subs)}
	case *BlockSensor:
		return map[string]any{"radius": c.radius, "lists": len(c.onBreak)}
	case *CombatRegeneration:
		return map[string]any{"duration": c.duration, "self": c.toSelf, "family": c.toFamily}
	case *Strength:
		return map[string]any{"value": c.Value, "max": c.Max}
	case *Genetics:
		return map[string]any{"mutation": c.mutation, "genes": len(c.genes)}
	case *KnockbackRules:
		p := c.presets[0]
		return map[string]any{"h": p.h, "v": p.v, "cap": p.vcap, "water": p.inWater}
	}
	return nil
}
