//go:build crossfly_full

package misc

import (
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/world"
)

// AreaAttack is minecraft:area_attack (BDS AreaAttackSystem::_tickComponent):
// every tick, entities whose box is within damage_range of the mob's box and
// that pass entity_filter take damage_per_tick from the mob with cause, at
// most once per damage_cooldown each (slimes, magma cubes, pufferfish).
//
//	damage_range       0.2
//	damage_per_tick    2
//	damage_cooldown    0 s (per entity)
//	cause              "lava"... any BDS cause; entity_attack is an attack by
//	                   the mob (BDS ActorDamageByActorSource with the cause)
//	play_attack_sound  true: level sound "attack"
type AreaAttack struct {
	rng      float64
	damage   float64
	cooldown int
	cause    string
	sound    bool
	filter   mob.Filter

	until map[*world.EntityHandle]int64
}

func newAreaAttack(key string, raw json.RawMessage) (mob.Component, error) {
	v := struct {
		DamageRange     *float64        `json:"damage_range"`
		DamagePerTick   *float64        `json:"damage_per_tick"`
		DamageCooldown  float64         `json:"damage_cooldown"`
		Cause           string          `json:"cause"`
		PlayAttackSound *bool           `json:"play_attack_sound"`
		EntityFilter    json.RawMessage `json:"entity_filter"`
	}{}
	if err := decode(raw, &v); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	c := &AreaAttack{rng: 0.2, damage: 2, cooldown: ticks(v.DamageCooldown), cause: v.Cause, sound: true, until: map[*world.EntityHandle]int64{}}
	if v.DamageRange != nil {
		c.rng = *v.DamageRange
	}
	if v.DamagePerTick != nil {
		c.damage = *v.DamagePerTick
	}
	if v.PlayAttackSound != nil {
		c.sound = *v.PlayAttackSound
	}
	f, err := mob.CompileFilter(v.EntityFilter)
	if err != nil {
		return nil, fmt.Errorf("%v: entity_filter: %w", key, err)
	}
	c.filter = f
	return c, nil
}

// Attach ...
func (*AreaAttack) Attach(*mob.Mob) {}

// Detach ...
func (*AreaAttack) Detach(*mob.Mob) {}

// Tick deals the damage.
func (c *AreaAttack) Tick(m *mob.Mob) {
	if m.Dead() {
		return
	}
	now := m.Ticks()
	// BDS prunes expired cooldown entries every 1200 ticks.
	if now%1200 == 0 {
		for h, t := range c.until {
			if t < now {
				delete(c.until, h)
			}
		}
	}
	b, r := m.BBox(), c.rng
	for _, e := range entitiesIn(m, b.Min()[0]-r, b.Min()[1]-r, b.Min()[2]-r, b.Max()[0]+r, b.Max()[1]+r, b.Max()[2]+r) {
		l, ok := livingTarget(e)
		if !ok || !c.filter.Test(otherCtx(m, e)) {
			continue
		}
		if c.cooldown > 0 {
			if t, ok := c.until[e.H()]; ok && now < t {
				continue
			}
			c.until[e.H()] = now + int64(c.cooldown)
		}
		l.Hurt(c.damage, attackSource(m, c.cause))
		if c.sound {
			m.PlaySound("attack")
		}
		if m.Dead() || m.H().Closed() {
			return
		}
	}
}

// DamageOverTime is minecraft:damage_over_time (BDS
// tickDamageOverTimeComponent): the mob hurts itself by damage_per_hurt
// (1) every time_between_hurt seconds (0: every tick, the hurt immunity
// still limits it), cause "none". Saved as DamageTime.
type DamageOverTime struct {
	damage   float64
	interval int
	left     int
}

func newDamageOverTime(key string, raw json.RawMessage) (mob.Component, error) {
	v := struct {
		DamagePerHurt   *float64 `json:"damage_per_hurt"`
		TimeBetweenHurt float64  `json:"time_between_hurt"`
	}{}
	if err := decode(raw, &v); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	c := &DamageOverTime{damage: 1, interval: int(float32(v.TimeBetweenHurt) * 20)}
	if v.DamagePerHurt != nil {
		c.damage = math.Trunc(*v.DamagePerHurt)
	}
	c.left = c.interval
	return c, nil
}

// Attach ...
func (*DamageOverTime) Attach(*mob.Mob) {}

// Detach ...
func (*DamageOverTime) Detach(*mob.Mob) {}

// Tick ...
func (c *DamageOverTime) Tick(m *mob.Mob) {
	if c.left--; c.left > 0 {
		return
	}
	c.left = c.interval
	if !m.Dead() {
		m.Hurt(c.damage, core.CauseSource{Cause: "none"})
	}
}

// EncodeNBT ...
func (c *DamageOverTime) EncodeNBT() map[string]any {
	return map[string]any{"DamageTime": int16(c.left)}
}

// DecodeNBT ...
func (c *DamageOverTime) DecodeNBT(_ *mob.Mob, d map[string]any) {
	if v, ok := d["DamageTime"].(int16); ok {
		c.left = int(v)
	}
}

// AttackCooldown is minecraft:attack_cooldown (BDS
// _tickAttackCooldownComponent): once attack_cooldown_time (seconds, a
// range rolled when the component is added) has passed, the
// attack_cooldown_complete_event fires once. Hoglins, goats, piglins and
// axolotls add it after attacking to stop attacking for a while.
type AttackCooldown struct {
	time   sense.Range
	event  sense.Trigger
	until  int64
	done   bool
	loaded bool
}

func newAttackCooldown(key string, raw json.RawMessage) (mob.Component, error) {
	v := struct {
		Time  json.RawMessage `json:"attack_cooldown_time"`
		Event json.RawMessage `json:"attack_cooldown_complete_event"`
	}{}
	if err := decode(raw, &v); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	r, err := sense.ParseRange(v.Time, sense.Range{Min: 1, Max: 1})
	if err != nil {
		return nil, fmt.Errorf("%v: attack_cooldown_time: %w", key, err)
	}
	t, err := sense.ParseTrigger(v.Event)
	if err != nil {
		return nil, fmt.Errorf("%v: attack_cooldown_complete_event: %w", key, err)
	}
	return &AttackCooldown{time: r, event: t}, nil
}

// Attach starts the cooldown.
func (c *AttackCooldown) Attach(m *mob.Mob) {
	c.until = m.Ticks() + int64(ticks(c.time.Roll(m.Rand())))
}

// Detach ...
func (*AttackCooldown) Detach(*mob.Mob) {}

// OnCooldown reports whether the cooldown is still running.
func (c *AttackCooldown) OnCooldown(m *mob.Mob) bool { return !c.done && m.Ticks() <= c.until }

func (c *AttackCooldown) tick(m *mob.Mob) {
	if c.done || m.Dead() || m.Ticks() <= c.until {
		return
	}
	c.done = true
	ctx := m.Ctx()
	ctx.Other = m
	c.event.Fire(m, ctx)
}

// EncodeNBT saves the ticks left (the mob tick counter restarts on load).
func (c *AttackCooldown) EncodeNBT() map[string]any {
	return map[string]any{"Done": boolByte(c.done), "Until": c.until}
}

// DecodeNBT ...
func (c *AttackCooldown) DecodeNBT(m *mob.Mob, d map[string]any) {
	c.done = d["Done"] == uint8(1)
	if v, ok := d["Until"].(int64); ok {
		c.until = v
	}
}

// KnockbackRules is minecraft:apply_knockback_rules: the knock back the
// mob's melee hits give (iron golems fling up, drowned pull swimmers down).
// The first preset whose filter passes (the target as "other") applies.
//
//	horizontal_power, vertical_power   default 0.4, 0.4
//	vertical_velocity_cap              caps the vertical push (towards its sign)
//	check_if_target_is_immersed_in_water  only for targets in water
type KnockbackRules struct {
	presets []knockPreset
}

type knockPreset struct {
	filter     mob.Filter
	h, v, vcap float64
	hasCap     bool
	inWater    bool
}

func newKnockbackRules(key string, raw json.RawMessage) (mob.Component, error) {
	v := struct {
		Presets []struct {
			Filter     json.RawMessage `json:"filter"`
			Horizontal *float64        `json:"horizontal_power"`
			Vertical   *float64        `json:"vertical_power"`
			Cap        *float64        `json:"vertical_velocity_cap"`
			InWater    bool            `json:"check_if_target_is_immersed_in_water"`
		} `json:"presets"`
	}{}
	if err := decode(raw, &v); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	c := &KnockbackRules{}
	for _, p := range v.Presets {
		f, err := mob.CompileFilter(p.Filter)
		if err != nil {
			return nil, fmt.Errorf("%v: filter: %w", key, err)
		}
		k := knockPreset{filter: f, h: 0.4, v: 0.4, inWater: p.InWater}
		if p.Horizontal != nil {
			k.h = *p.Horizontal
		}
		if p.Vertical != nil {
			k.v = *p.Vertical
		}
		if p.Cap != nil {
			k.vcap, k.hasCap = *p.Cap, true
		}
		c.presets = append(c.presets, k)
	}
	return c, nil
}

// Attach ...
func (*KnockbackRules) Attach(*mob.Mob) {}

// Detach ...
func (*KnockbackRules) Detach(*mob.Mob) {}

// ApplyKnockback implements sense.KnockbackApplier.
func (c *KnockbackRules) ApplyKnockback(m *mob.Mob, t world.Entity, _ float64) bool {
	l, ok := livingTarget(t)
	if !ok {
		return false
	}
	for _, p := range c.presets {
		if p.inWater && !inWater(m.Tx(), t) {
			continue
		}
		if !p.filter.Test(otherCtx(m, t)) {
			continue
		}
		vert := p.v
		if p.hasCap {
			if p.vcap >= 0 {
				vert = math.Min(vert, p.vcap)
			} else {
				vert = math.Max(vert, p.vcap)
			}
		}
		l.KnockBack(m.Position(), p.h, vert)
		return true
	}
	return false
}

// inWater reports whether e stands in water (a mob's last travel step, the
// block at the feet of anything else).
func inWater(tx *world.Tx, e world.Entity) bool {
	if m, ok := mob.Of(e); ok {
		return m.Physics().InWater
	}
	l, ok := tx.Liquid(cube.PosFromVec3(e.Position()))
	if !ok {
		return false
	}
	_, water := l.(block.Water)
	return water
}

// CombatRegeneration is minecraft:combat_regeneration (axolotl, BDS
// CombatRegenerationListener): when the mob's target dies, the mob that
// killed it gets Regeneration I for regeneration_duration (5 s) and loses
// Mining Fatigue; the mob itself only with apply_to_self, mobs of its type
// only with apply_to_family. The killer is the target's last attacker.
type CombatRegeneration struct {
	duration   int
	toSelf     bool
	toFamily   bool
	lastTarget *world.EntityHandle
}

func newCombatRegeneration(key string, raw json.RawMessage) (mob.Component, error) {
	v := struct {
		Duration *float64 `json:"regeneration_duration"`
		Self     bool     `json:"apply_to_self"`
		Family   bool     `json:"apply_to_family"`
	}{}
	if err := decode(raw, &v); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	c := &CombatRegeneration{duration: 100, toSelf: v.Self, toFamily: v.Family}
	if v.Duration != nil {
		c.duration = ticks(*v.Duration)
	}
	return c, nil
}

// Attach ...
func (*CombatRegeneration) Attach(*mob.Mob) {}

// Detach ...
func (*CombatRegeneration) Detach(*mob.Mob) {}

func (c *CombatRegeneration) tick(m *mob.Mob) {
	if h := m.TargetHandle(); h != nil {
		c.lastTarget = h
	}
	if c.lastTarget == nil {
		return
	}
	t, ok := c.lastTarget.Entity(m.Tx())
	if !ok {
		c.lastTarget = nil
		return
	}
	tm, ok := mob.Of(t)
	if !ok || !tm.Dead() {
		if l, ok := t.(interface{ Dead() bool }); ok && l.Dead() {
			c.lastTarget = nil
		}
		return
	}
	c.lastTarget = nil
	killer, _, ok := tm.LastHurtBy()
	if !ok {
		return
	}
	if killer.H() == m.H() && !c.toSelf {
		return
	}
	if km, ok := mob.Of(killer); ok && km.H() != m.H() && km.Identifier() == m.Identifier() && !c.toFamily {
		return
	}
	l, ok := livingTarget(killer)
	if !ok {
		return
	}
	l.RemoveEffect(effect.MiningFatigue)
	l.AddEffect(effect.New(effect.Regeneration, 1, time.Duration(c.duration)*time.Second/20))
}

func boolByte(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}
