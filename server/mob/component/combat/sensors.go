//go:build crossfly_full

package combat

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/world"
)

// EnvironmentSensor is minecraft:environment_sensor: every tick each trigger
// whose filters pass fires its event (BDS EnvironmentSensorSystem). It runs
// as a StageSensors system so the events it fires never change the ticker
// list mid-iteration.
type EnvironmentSensor struct {
	triggers []sense.Trigger
	// fctx is the filter context of run, kept to not allocate one per
	// trigger and tick.
	fctx mob.FilterContext
}

func newEnvironmentSensor(_ string, raw json.RawMessage) (mob.Component, error) {
	var v struct {
		Triggers json.RawMessage `json:"triggers"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("minecraft:environment_sensor: %w", err)
	}
	t, err := sense.ParseTriggers(v.Triggers)
	if err != nil {
		return nil, fmt.Errorf("minecraft:environment_sensor: %w", err)
	}
	return &EnvironmentSensor{triggers: t}, nil
}

// Attach ...
func (*EnvironmentSensor) Attach(*mob.Mob) {}

// Detach ...
func (*EnvironmentSensor) Detach(*mob.Mob) {}

func (s *EnvironmentSensor) run(m *mob.Mob) {
	// A fired event may replace this component; keep going with the
	// triggers read at the start of the tick, as BDS does (the list is never
	// changed, a new component gets its own).
	for _, t := range s.triggers {
		if t.Empty() {
			continue
		}
		ctx := &s.fctx
		*ctx = mob.FilterContext{Tx: m.Tx(), Self: m}
		if tg, ok := m.Target(); ok {
			ctx.Target = tg
		}
		pass := t.Test(m, ctx)
		var fire *mob.FilterContext
		if pass {
			// The event gets a context of its own.
			fire = new(mob.FilterContext)
			*fire = *ctx
		}
		*ctx = mob.FilterContext{}
		if pass {
			t.Fire(m, fire)
		}
		if m.Dead() || m.H().Closed() {
			return
		}
	}
}

// TargetNearbySensor is minecraft:target_nearby_sensor: events when the
// target comes within inside_range (and is seen, with must_see), goes beyond
// outside_range, or is lost from sight inside the range.
type TargetNearbySensor struct {
	inside, outside           float64
	mustSee                   bool
	onInside, onOutside, lost sense.Trigger
	wasInside, wasOutside     bool
}

func newTargetNearbySensor(_ string, raw json.RawMessage) (mob.Component, error) {
	var v struct {
		InsideRange  *float64        `json:"inside_range"`
		OutsideRange *float64        `json:"outside_range"`
		MustSee      bool            `json:"must_see"`
		OnInside     json.RawMessage `json:"on_inside_range"`
		OnOutside    json.RawMessage `json:"on_outside_range"`
		OnVisionLost json.RawMessage `json:"on_vision_lost_inside_range"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("minecraft:target_nearby_sensor: %w", err)
	}
	s := &TargetNearbySensor{inside: 1, outside: 5, mustSee: v.MustSee}
	if v.InsideRange != nil {
		s.inside = *v.InsideRange
	}
	if v.OutsideRange != nil {
		s.outside = *v.OutsideRange
	}
	var err error
	for _, p := range []struct {
		raw json.RawMessage
		t   *sense.Trigger
	}{{v.OnInside, &s.onInside}, {v.OnOutside, &s.onOutside}, {v.OnVisionLost, &s.lost}} {
		if *p.t, err = sense.ParseTrigger(p.raw); err != nil {
			return nil, fmt.Errorf("minecraft:target_nearby_sensor: %w", err)
		}
	}
	return s, nil
}

// Attach ...
func (*TargetNearbySensor) Attach(*mob.Mob) {}

// Detach ...
func (*TargetNearbySensor) Detach(*mob.Mob) {}

func (s *TargetNearbySensor) run(m *mob.Mob) {
	t, ok := m.Target()
	if !ok {
		s.wasInside, s.wasOutside = false, false
		return
	}
	// BDS _tickTargetNearbyComponent: AABB::distanceTo(AABB), the gap
	// between the two boxes (as SwellGoal), not the feet distance.
	dist := sense.BoxDistance(m.BBox(), sense.BBox(t))
	seen := !s.mustSee || sense.CanSee(m, t)
	inside := dist <= s.inside && seen
	outside := dist > s.outside
	ctx := m.Ctx()
	ctx.Target, ctx.Other = t, t
	wasInside, wasOutside := s.wasInside, s.wasOutside
	s.wasInside, s.wasOutside = inside, outside
	switch {
	case inside && !wasInside:
		s.onInside.Run(m, ctx)
	case dist <= s.inside && !seen && wasInside:
		s.lost.Run(m, ctx)
	case outside && !wasOutside:
		s.onOutside.Run(m, ctx)
	}
}

// OnTarget is minecraft:on_target_acquired / minecraft:on_target_escape:
// an event when the mob gets a target, or loses it.
type OnTarget struct {
	acquired bool
	trigger  sense.Trigger
}

func newOnTarget(key string, raw json.RawMessage) (mob.Component, error) {
	t, err := sense.ParseTrigger(raw)
	if err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	return &OnTarget{acquired: key == "minecraft:on_target_acquired", trigger: t}, nil
}

// Attach ...
func (*OnTarget) Attach(*mob.Mob) {}

// Detach ...
func (*OnTarget) Detach(*mob.Mob) {}

// OnTargetChanged ...
func (o *OnTarget) OnTargetChanged(m *mob.Mob, old, new world.Entity) {
	ctx := m.Ctx()
	switch {
	case o.acquired && new != nil:
		ctx.Target, ctx.Other = new, new
	case !o.acquired && new == nil && old != nil:
		ctx.Other = old
	default:
		return
	}
	o.trigger.Run(m, ctx)
}

// damageTrigger is one entry of damage_sensor "triggers".
type damageTrigger struct {
	cause      string
	onDamage   sense.Trigger
	deals      bool
	multiplier float64
	modifier   float64
}

// DamageSensor is minecraft:damage_sensor: for damage of a cause whose
// on_damage filters pass, fires on_damage's event and cancels or scales the
// damage. Filters see the attacker as "other"/"damager".
type DamageSensor struct {
	triggers []damageTrigger
}

func newDamageSensor(_ string, raw json.RawMessage) (mob.Component, error) {
	var v struct {
		Triggers json.RawMessage `json:"triggers"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("minecraft:damage_sensor: %w", err)
	}
	var list []json.RawMessage
	if len(v.Triggers) > 0 && v.Triggers[0] == '[' {
		if err := json.Unmarshal(v.Triggers, &list); err != nil {
			return nil, fmt.Errorf("minecraft:damage_sensor: %w", err)
		}
	} else if len(v.Triggers) > 0 {
		list = []json.RawMessage{v.Triggers}
	}
	s := &DamageSensor{}
	for _, r := range list {
		var t struct {
			Cause            string          `json:"cause"`
			OnDamage         json.RawMessage `json:"on_damage"`
			DealsDamage      json.RawMessage `json:"deals_damage"`
			DamageMultiplier *float64        `json:"damage_multiplier"`
			DamageModifier   float64         `json:"damage_modifier"`
		}
		if err := json.Unmarshal(r, &t); err != nil {
			return nil, fmt.Errorf("minecraft:damage_sensor: %w", err)
		}
		on, err := sense.ParseTrigger(t.OnDamage)
		if err != nil {
			return nil, fmt.Errorf("minecraft:damage_sensor: %w", err)
		}
		dt := damageTrigger{cause: t.Cause, onDamage: on, deals: true, multiplier: 1, modifier: t.DamageModifier}
		if t.DamageMultiplier != nil {
			dt.multiplier = *t.DamageMultiplier
		}
		switch string(t.DealsDamage) {
		case `false`, `"no"`, `"no_but_side_effects_apply"`:
			dt.deals = false
		}
		s.triggers = append(s.triggers, dt)
	}
	return s, nil
}

// Attach ...
func (*DamageSensor) Attach(*mob.Mob) {}

// Detach ...
func (*DamageSensor) Detach(*mob.Mob) {}

// ModifyDamage ...
func (s *DamageSensor) ModifyDamage(m *mob.Mob, d *mob.Damage) {
	cause := sense.DamageCause(d.Source)
	ctx := m.Ctx()
	ctx.Other, ctx.Damager, ctx.Damage = d.Attacker, d.Attacker, d.Source
	// DamageSensorComponent::recordGenericDamage: fatal is the damage before
	// armour and the sensor change it, against the health (an int in BDS).
	raw := d.Raw
	if raw == 0 {
		raw = d.Amount
	}
	ctx.DamageFatal = raw >= math.Trunc(m.Health())
	if d.Attacker == nil {
		if _, ok := d.Source.(entity.LightningDamageSource); ok {
			ctx.Other = nearbyLightning(m)
			ctx.Damager = ctx.Other
		}
	}
	if sense.IsPlayer(d.Attacker) {
		ctx.Player = d.Attacker
	}
	if t, ok := m.Target(); ok {
		ctx.Target = t
	}
	for _, t := range slices.Clone(s.triggers) {
		if t.cause != "" && t.cause != "all" && t.cause != "any" && t.cause != cause {
			continue
		}
		if !t.onDamage.Test(m, ctx) {
			continue
		}
		t.onDamage.Fire(m, ctx)
		d.Amount = d.Amount*t.multiplier + t.modifier
		if !t.deals {
			d.Cancel = true
			return
		}
	}
}

// nearbyLightning finds the lightning bolt that struck m, for the
// "other is_family lightning" filters (Dragonfly's lightning damage source
// does not carry the bolt).
func nearbyLightning(m *mob.Mob) world.Entity {
	for e := range m.Tx().EntitiesWithin(m.BBox().Grow(6)) {
		if e.H().Type() == entity.LightningType {
			return e
		}
	}
	return nil
}
