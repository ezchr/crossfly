//go:build crossfly_full

package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
)

// rangeValue is a JSON number or {"range_min": a, "range_max": b} (BDS
// FloatRange / the "value" of attribute components).
type rangeValue struct {
	Min, Max float64
	Set      bool
}

func (r *rangeValue) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	var f float64
	if json.Unmarshal(b, &f) == nil {
		*r = rangeValue{Min: f, Max: f, Set: true}
		return nil
	}
	var arr []float64
	if json.Unmarshal(b, &arr) == nil && len(arr) > 0 {
		r.Min, r.Max, r.Set = arr[0], arr[len(arr)-1], true
		return nil
	}
	var obj struct {
		RangeMin *float64 `json:"range_min"`
		RangeMax *float64 `json:"range_max"`
		Min      *float64 `json:"min"`
		Max      *float64 `json:"max"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return fmt.Errorf("range %s: %w", b, err)
	}
	pick := func(a, c *float64) float64 {
		if a != nil {
			return *a
		}
		if c != nil {
			return *c
		}
		return 0
	}
	r.Min, r.Max, r.Set = pick(obj.RangeMin, obj.Min), pick(obj.RangeMax, obj.Max), true
	return nil
}

// roll returns a value in [Min, Max].
func (r rangeValue) roll(m *mob.Mob) float64 {
	if r.Max <= r.Min {
		return r.Min
	}
	return r.Min + m.Rand().Float64()*(r.Max-r.Min)
}

// rollInt returns an integer in [Min, Max].
func (r rangeValue) rollInt(m *mob.Mob) int {
	lo, hi := int(math.Round(r.Min)), int(math.Round(r.Max))
	if hi <= lo {
		return lo
	}
	return lo + m.Rand().IntN(hi-lo+1)
}

// attributeComponent sets one attribute from {"value": v, "max": m}
// (minecraft:movement, follow_range, knockback_resistance, ...; BDS
// AttributeDefinition: the value rolls once from a range, "max" raises the
// attribute's maximum).
type attributeComponent struct {
	attr  string
	value rangeValue
	max   *float64
	prev  mob.Attribute
}

func newAttributeFactory(attr string) mob.ComponentFactory {
	return func(key string, raw json.RawMessage) (mob.Component, error) {
		var j struct {
			Value rangeValue `json:"value"`
			Max   *float64   `json:"max"`
		}
		var bare float64
		if json.Unmarshal(raw, &bare) == nil {
			// "minecraft:follow_range": 64
			return &attributeComponent{attr: attr, value: rangeValue{Min: bare, Max: bare, Set: true}}, nil
		}
		if err := decode(raw, &j); err != nil {
			return nil, fmt.Errorf("%v: %w", key, err)
		}
		return &attributeComponent{attr: attr, value: j.Value, max: j.Max}, nil
	}
}

func (c *attributeComponent) Attach(m *mob.Mob) {
	a := m.Attributes().Get(c.attr)
	c.prev = *a
	if c.max != nil {
		m.Attributes().SetRange(c.attr, a.Min, math.Max(*c.max, a.Min))
	}
	if c.value.Set {
		v := c.value.roll(m)
		if c.max == nil && v > a.Max {
			m.Attributes().SetRange(c.attr, a.Min, v)
		}
		a.SetBase(v)
	}
}

func (c *attributeComponent) Detach(m *mob.Mob) {
	a := m.Attributes().Get(c.attr)
	*a = c.prev
}

// healthComponent is minecraft:health {"value", "max"}: the maximum and the
// starting health (BDS HealthAttributeDelegate). It also records the cause
// of each hit for behavior.panic (LastHurtOf).
type healthComponent struct {
	value rangeValue
	max   *float64
}

func newHealth(key string, raw json.RawMessage) (mob.Component, error) {
	var j struct {
		Value rangeValue `json:"value"`
		Max   *float64   `json:"max"`
	}
	if err := decode(raw, &j); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	return &healthComponent{value: j.Value, max: j.Max}, nil
}

func (c *healthComponent) Attach(m *mob.Mob) {
	v := 10.0
	if c.value.Set {
		v = math.Round(c.value.roll(m))
	}
	maxHealth := v
	if c.max != nil {
		maxHealth = *c.max
	}
	if maxHealth < v {
		maxHealth = v
	}
	m.SetMaxHealth(maxHealth)
	m.SetHealth(v)
}

func (c *healthComponent) Detach(*mob.Mob) {}

// OnHurt records the damage cause (panic reads it).
func (c *healthComponent) OnHurt(m *mob.Mob, _ float64, src world.DamageSource) { recordHurt(m, src) }

// attackComponent is minecraft:attack {"damage", "effect_name",
// "effect_duration"}: the attack_damage attribute (rolled per attack when a
// range, BDS AttackDamage) and the effect a melee hit applies. Combat goals
// read it with AttackOf.
type attackComponent struct {
	Damage         rangeValue
	EffectName     string
	EffectDuration float64
	prev           mob.Attribute
}

func newAttack(key string, raw json.RawMessage) (mob.Component, error) {
	var j struct {
		Damage         rangeValue `json:"damage"`
		EffectName     string     `json:"effect_name"`
		EffectDuration *float64   `json:"effect_duration"`
	}
	if err := decode(raw, &j); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	c := &attackComponent{Damage: j.Damage, EffectName: j.EffectName, EffectDuration: 0}
	if j.EffectDuration != nil {
		c.EffectDuration = *j.EffectDuration
	}
	return c, nil
}

func (c *attackComponent) Attach(m *mob.Mob) {
	a := m.Attributes().Get(mob.AttrAttackDamage)
	c.prev = *a
	if c.Damage.Set {
		a.SetBase(c.Damage.Max)
	}
}

func (c *attackComponent) Detach(m *mob.Mob) { *m.Attributes().Get(mob.AttrAttackDamage) = c.prev }

// Attack is the minecraft:attack data of a mob, for melee goals.
type Attack struct {
	// Damage is the damage of one hit (rolled when the JSON gives a range).
	Damage float64
	// Effect is the effect a hit applies (nil without one).
	Effect *effect.Effect
}

// AttackOf rolls a hit of m's minecraft:attack component. Without one it
// returns the attack_damage attribute.
func AttackOf(m *mob.Mob) Attack {
	c, ok := mob.ComponentOf[*attackComponent](m)
	if !ok {
		return Attack{Damage: m.Attr(mob.AttrAttackDamage)}
	}
	a := Attack{Damage: m.Attr(mob.AttrAttackDamage)}
	if c.Damage.Set {
		a.Damage = float64(c.Damage.rollInt(m))
	}
	if c.EffectName != "" {
		if t, ok := effectByName(c.EffectName); ok {
			dur := c.EffectDuration
			if dur <= 0 {
				dur = 0
			}
			e := effect.New(t, 1, secondsToDuration(dur))
			a.Effect = &e
		}
	}
	return a
}

// decode unmarshals component JSON, accepting empty JSON.
func decode(raw json.RawMessage, v any) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	return json.Unmarshal(raw, v)
}
