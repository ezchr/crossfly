//go:build crossfly_full

package misc

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

func boxOf(minX, minY, minZ, maxX, maxY, maxZ float64) cube.BBox {
	return cube.Box(minX, minY, minZ, maxX, maxY, maxZ)
}

func causeSource(cause string) world.DamageSource { return core.SourceOf(cause) }

// FireImmune is minecraft:fire_immune (BDS FireImmuneDefinition: the actor
// flag fire_immune, Actor::isFireImmune). The mob takes no fire, fire tick,
// lava or magma damage and cannot be set on fire (Actor::setOnFire returns
// early); the client stops drawing flames from the actor flag.
type FireImmune struct{}

func newFireImmune(string, json.RawMessage) (mob.Component, error) { return &FireImmune{}, nil }

// Attach ...
func (*FireImmune) Attach(m *mob.Mob) {
	m.SetActorFlag(protocol.EntityDataFlagFireImmune, true)
	m.Extinguish()
}

// Detach ...
func (*FireImmune) Detach(m *mob.Mob) { m.SetActorFlag(protocol.EntityDataFlagFireImmune, false) }

// ModifyDamage cancels fire damage.
func (*FireImmune) ModifyDamage(_ *mob.Mob, d *mob.Damage) {
	switch core.CauseOf(d.Source) {
	case "fire", "fire_tick", "lava", "magma", "campfire", "soul_campfire":
		d.Cancel = true
	default:
		if d.Source != nil && d.Source.Fire() {
			d.Cancel = true
		}
	}
}

// fireImmuneTick puts the fire out: entities set burning by Dragonfly blocks
// (lava, fire) go through Ent.SetOnFire, which the mob does not override.
func fireImmuneTick(m *mob.Mob) {
	if m.OnFireDuration() > 0 {
		if _, ok := mob.ComponentOf[*FireImmune](m); ok {
			m.Extinguish()
		}
	}
}

// IsFireImmune reports whether e is fire immune (a mob with fire_immune, or
// any entity with fire resistance).
func IsFireImmune(e world.Entity) bool {
	if m, ok := mob.Of(e); ok {
		if _, ok := mob.ComponentOf[*FireImmune](m); ok {
			return true
		}
	}
	if l, ok := e.(interface {
		Effect(effect.Type) (effect.Effect, bool)
	}); ok {
		_, ok := l.Effect(effect.FireResistance)
		return ok
	}
	return false
}

// Freezing is minecraft:freezing_immune and minecraft:freezing_vulnerable
// (BDS FreezingComponent): immune mobs take no freezing damage, vulnerable
// ones take five times as much (source: java/paper, the
// freeze_hurts_extra_types tag). Dragonfly has no powder snow, so freezing
// damage only comes from other code using the "freezing" cause.
type Freezing struct{ Immune bool }

func newFreezing(key string, _ json.RawMessage) (mob.Component, error) {
	return &Freezing{Immune: key == "minecraft:freezing_immune"}, nil
}

// Attach ...
func (*Freezing) Attach(*mob.Mob) {}

// Detach ...
func (*Freezing) Detach(*mob.Mob) {}

// ModifyDamage ...
func (f *Freezing) ModifyDamage(_ *mob.Mob, d *mob.Damage) {
	if core.CauseOf(d.Source) != "freezing" {
		return
	}
	if f.Immune {
		d.Cancel = true
		return
	}
	d.Amount *= 5
}

// EffectImmunity is minecraft:mob_effect_immunity (BDS
// MobEffectImmunityComponent): the listed effects are never added.
type EffectImmunity struct {
	types []effect.Type
	names []string
}

func newEffectImmunity(key string, raw json.RawMessage) (mob.Component, error) {
	var v struct {
		MobEffects []string `json:"mob_effects"`
	}
	if err := decode(raw, &v); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	c := &EffectImmunity{names: v.MobEffects}
	for _, n := range v.MobEffects {
		if t, ok := EffectByName(n); ok {
			c.types = append(c.types, t)
		}
	}
	return c, nil
}

// Attach removes effects the mob already has and is now immune to.
func (c *EffectImmunity) Attach(m *mob.Mob) {
	for _, t := range c.types {
		if _, ok := m.Effect(t); ok {
			m.RemoveEffect(t)
		}
	}
}

// Detach ...
func (*EffectImmunity) Detach(*mob.Mob) {}

// ImmuneToEffect implements mob.EffectImmunity.
func (c *EffectImmunity) ImmuneToEffect(_ *mob.Mob, t effect.Type) bool {
	return slices.Contains(c.types, t)
}

// Names are the effect names of the JSON, Dragonfly-known or not.
func (c *EffectImmunity) Names() []string { return c.names }

// CannotBeAttacked is minecraft:cannot_be_attacked: other mobs cannot hurt
// the mob unless they have minecraft:ignore_cannot_be_attacked (BDS
// "cannot_be_attacked" on the attack paths). Players and non-entity damage
// are not affected.
type CannotBeAttacked struct{}

func newCannotBeAttacked(string, json.RawMessage) (mob.Component, error) {
	return &CannotBeAttacked{}, nil
}

// Attach ...
func (*CannotBeAttacked) Attach(*mob.Mob) {}

// Detach ...
func (*CannotBeAttacked) Detach(*mob.Mob) {}

// ModifyDamage ...
func (*CannotBeAttacked) ModifyDamage(_ *mob.Mob, d *mob.Damage) {
	if d.Attacker == nil {
		return
	}
	if _, ok := d.Attacker.(*player.Player); ok {
		return
	}
	if am, ok := mob.Of(d.Attacker); ok && !am.HasComponent("minecraft:ignore_cannot_be_attacked") {
		d.Cancel = true
	}
}

// CannotBeAttackedBy reports whether attacker may not hurt e
// (cannot_be_attacked), for target goals that want to skip it.
func CannotBeAttackedBy(e, attacker world.Entity) bool {
	m, ok := mob.Of(e)
	if !ok || !m.HasComponent("minecraft:cannot_be_attacked") {
		return false
	}
	am, ok := mob.Of(attacker)
	return ok && !am.HasComponent("minecraft:ignore_cannot_be_attacked")
}

// actorFlag is a JSON-less component that sets one actor flag while active
// (is_stunned, is_ignited, is_pregnant).
type actorFlag struct {
	flag uint8
	meta func(*mob.Meta, bool)
}

func newActorFlag(flag uint8, meta func(*mob.Meta, bool)) mob.ComponentFactory {
	return func(string, json.RawMessage) (mob.Component, error) { return &actorFlag{flag: flag, meta: meta}, nil }
}

// Attach ...
func (c *actorFlag) Attach(m *mob.Mob) { c.set(m, true) }

// Detach ...
func (c *actorFlag) Detach(m *mob.Mob) { c.set(m, false) }

func (c *actorFlag) set(m *mob.Mob, v bool) {
	if c.meta != nil {
		m.UpdateMeta(func(meta *mob.Meta) { c.meta(meta, v) })
		return
	}
	m.SetActorFlag(c.flag, v)
}

// IsStunned reports whether m has minecraft:is_stunned (ravager stun).
func IsStunned(m *mob.Mob) bool { return m.HasComponent("minecraft:is_stunned") }

// Strength is minecraft:strength (llama, BDS StrengthDescription): the
// strength value (chest slots = 3 x value) and maximum.
type Strength struct {
	Value, Max int
}

func newStrength(key string, raw json.RawMessage) (mob.Component, error) {
	v := struct {
		Value *int `json:"value"`
		Max   *int `json:"max"`
	}{}
	if err := decode(raw, &v); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	s := &Strength{Value: 1, Max: 5}
	if v.Value != nil {
		s.Value = *v.Value
	}
	if v.Max != nil {
		s.Max = *v.Max
	}
	s.Value = min(max(s.Value, 0), s.Max)
	return s, nil
}

// Attach sets the mob's strength and maximum (StrengthDescription: actor
// data 75/76, kept when the component goes; see core.Strength).
func (s *Strength) Attach(m *mob.Mob) {
	core.SetStrengthMax(m, s.Max)
	core.SetStrength(m, s.Value)
}

// Detach ...
func (*Strength) Detach(*mob.Mob) {}

// nop is a component with no server-side behaviour.
type nop struct{}

func newNop(string, json.RawMessage) (mob.Component, error) { return nop{}, nil }

func (nop) Attach(*mob.Mob) {}
func (nop) Detach(*mob.Mob) {}
