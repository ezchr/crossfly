//go:build crossfly_full

package mob

import "github.com/df-mc/dragonfly/server/entity/effect"

// ActorFlags is a set of extra Bedrock actor data flags (the gophertunnel
// protocol.EntityDataFlag* bit numbers) that goals and components turn on
// for the client to animate: timer_flag_1..3, ram_attack, rolling,
// sneezing, croaking, stunned, ... Flags with a Meta field of their own
// (Sitting, Angry, InLove, ...) are not kept here. server/session sets every
// flag in the set on top of the other mob flags (ExtraActorFlags).
type ActorFlags [3]uint64

// Has reports whether flag is set.
func (f ActorFlags) Has(flag uint8) bool {
	if int(flag)/64 >= len(f) {
		return false
	}
	return f[flag/64]&(1<<(flag%64)) != 0
}

// Set turns flag on or off.
func (f *ActorFlags) Set(flag uint8, v bool) {
	if int(flag)/64 >= len(f) {
		return
	}
	if v {
		f[flag/64] |= 1 << (flag % 64)
	} else {
		f[flag/64] &^= 1 << (flag % 64)
	}
}

// SetActorFlag turns an extra actor flag on or off and shows the change.
func (m *Mob) SetActorFlag(flag uint8, v bool) {
	m.UpdateMeta(func(meta *Meta) { meta.ActorFlags.Set(flag, v) })
}

// ActorFlag reports whether an extra actor flag is on.
func (m *Mob) ActorFlag(flag uint8) bool { return m.s.meta.ActorFlags.Has(flag) }

// ExtraActorFlags returns the extra actor flags (server/session probes it on
// the mob and its behaviour).
func (s *state) ExtraActorFlags() [3]uint64 { return s.meta.ActorFlags }
func (m *Mob) ExtraActorFlags() [3]uint64   { return m.s.ExtraActorFlags() }

// EffectImmunity is an optional component hook: AddEffect ignores effects a
// component reports immunity to (minecraft:mob_effect_immunity).
type EffectImmunity interface {
	ImmuneToEffect(m *Mob, t effect.Type) bool
}

// immuneToEffect reports whether m cannot get t: the hard-coded rules of
// canBeAffected, or a component.
func (m *Mob) immuneToEffect(t effect.Type) bool {
	if !m.canBeAffected(t) {
		return true
	}
	for _, c := range m.s.comps {
		if im, ok := c.comp.(EffectImmunity); ok && im.ImmuneToEffect(m, t) {
			return true
		}
	}
	return false
}

// canBeAffected is Actor::canBeAffected with its overrides: undead mobs
// (family undead) never get regeneration or poison, spiders and cave
// spiders (Spider::canBeAffected) never poison, wither skeletons
// (Skeleton::canBeAffected, variant 1) never wither, and the wither
// (WitherBoss::canBeAffected) only instant health and instant damage.
func (m *Mob) canBeAffected(t effect.Type) bool {
	switch m.Identifier() {
	case "minecraft:wither":
		return t == effect.InstantHealth || t == effect.InstantDamage
	case "minecraft:spider", "minecraft:cave_spider":
		if t == effect.Poison {
			return false
		}
	case "minecraft:wither_skeleton":
		if t == effect.Wither {
			return false
		}
	}
	return !m.InvertedHealAndHarm() || (t != effect.Regeneration && t != effect.Poison)
}

// InvertedHealAndHarm is Actor::isInvertedHealAndHarm: undead mobs (the
// UndeadMob actor type class, which is the vanilla family undead) are hurt by
// instant health and healed by instant damage.
func (m *Mob) InvertedHealAndHarm() bool { return m.HasFamily("undead") }
