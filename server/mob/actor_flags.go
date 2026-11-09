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

// immuneToEffect reports whether a component makes m immune to t.
func (m *Mob) immuneToEffect(t effect.Type) bool {
	for _, c := range m.s.comps {
		if im, ok := c.comp.(EffectImmunity); ok && im.ImmuneToEffect(m, t) {
			return true
		}
	}
	return false
}
