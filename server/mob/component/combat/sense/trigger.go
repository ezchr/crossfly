//go:build crossfly_full

package sense

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// Trigger is a BDS ActorDefinitionTrigger: an event fired on a subject when
// its filters pass ({"event", "target", "filters"}).
type Trigger struct {
	Event  string
	Target string
	Filter mob.Filter
}

// Empty reports whether the trigger has no event.
func (t Trigger) Empty() bool { return t.Event == "" }

// ParseTrigger reads a trigger object. A bare string is the event name.
func ParseTrigger(raw json.RawMessage) (Trigger, error) {
	t := Trigger{Target: "self", Filter: mob.AlwaysTrue}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return t, nil
	}
	if raw[0] == '"' {
		return t, json.Unmarshal(raw, &t.Event)
	}
	var v struct {
		Event   string          `json:"event"`
		Target  string          `json:"target"`
		Filters json.RawMessage `json:"filters"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return t, err
	}
	t.Event = v.Event
	if v.Target != "" {
		t.Target = v.Target
	}
	f, err := mob.CompileFilter(v.Filters)
	if err != nil {
		return t, err
	}
	t.Filter = f
	return t, nil
}

// ParseTriggers reads one trigger object or an array of them.
func ParseTriggers(raw json.RawMessage) ([]Trigger, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	if raw[0] != '[' {
		t, err := ParseTrigger(raw)
		if err != nil || t.Empty() {
			return nil, err
		}
		return []Trigger{t}, nil
	}
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	out := make([]Trigger, 0, len(list))
	for _, r := range list {
		t, err := ParseTrigger(r)
		if err != nil {
			return nil, err
		}
		if !t.Empty() {
			out = append(out, t)
		}
	}
	return out, nil
}

// Test evaluates the filters for m with ctx (nil: Self only).
func (t Trigger) Test(m *mob.Mob, ctx *mob.FilterContext) bool {
	if ctx == nil {
		ctx = m.Ctx()
	}
	ctx.Self, ctx.Tx = m, m.Tx()
	return t.Filter.Test(ctx)
}

// Run tests the filters and, when they pass, fires the event on the
// trigger's target. It reports whether the event fired.
func (t Trigger) Run(m *mob.Mob, ctx *mob.FilterContext) bool {
	if t.Empty() || !t.Test(m, ctx) {
		return false
	}
	t.Fire(m, ctx)
	return true
}

// Fire fires the event on the trigger's target without testing filters.
// Targets: self (default), target, other, player, parent, baby, damager.
func (t Trigger) Fire(m *mob.Mob, ctx *mob.FilterContext) {
	if t.Empty() {
		return
	}
	if ctx == nil {
		ctx = m.Ctx()
	}
	var subject world.Entity = m
	switch t.Target {
	case "target":
		subject = ctx.Target
		if subject == nil {
			subject, _ = m.Target()
		}
	case "other":
		subject = ctx.Other
	case "player":
		subject = ctx.Player
	case "parent":
		subject = ctx.Parent
	case "baby":
		subject = ctx.Baby
	case "damager":
		subject = ctx.Damager
	}
	if subject == nil {
		return
	}
	sm, ok := mob.Of(subject)
	if !ok || sm.Dead() {
		return
	}
	c := *ctx
	if sm.H() != m.H() {
		c.Other = m
	}
	sm.Fire(t.Event, &c)
}

// Range is a min/max value range (BDS FloatRange / RandomValueBounds).
type Range struct{ Min, Max float64 }

// ParseRange reads a number, [min, max], {"min","max"} or
// {"range_min","range_max"}; def is used when raw is absent.
func ParseRange(raw json.RawMessage, def Range) (Range, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return def, nil
	}
	switch raw[0] {
	case '[':
		var v []float64
		if err := json.Unmarshal(raw, &v); err != nil {
			return def, err
		}
		switch len(v) {
		case 0:
			return def, nil
		case 1:
			return Range{v[0], v[0]}, nil
		}
		return Range{v[0], v[1]}, nil
	case '{':
		var v struct {
			Min      *float64 `json:"min"`
			Max      *float64 `json:"max"`
			RangeMin *float64 `json:"range_min"`
			RMax     *float64 `json:"range_max"`
			Value    *float64 `json:"value"`
		}
		if err := json.Unmarshal(raw, &v); err != nil {
			return def, err
		}
		r := def
		if v.Value != nil {
			return Range{*v.Value, *v.Value}, nil
		}
		if v.Min != nil {
			r.Min = *v.Min
			r.Max = math.Max(r.Max, r.Min)
		}
		if v.RangeMin != nil {
			r.Min = *v.RangeMin
		}
		if v.Max != nil {
			r.Max = *v.Max
		}
		if v.RMax != nil {
			r.Max = *v.RMax
		}
		if r.Max < r.Min {
			r.Max = r.Min
		}
		return r, nil
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil {
		return def, fmt.Errorf("range: %w", err)
	}
	return Range{f, f}, nil
}

// Roll returns a random value in the range.
func (r Range) Roll(rnd *rand.Rand) float64 {
	if r.Max <= r.Min {
		return r.Min
	}
	return r.Min + rnd.Float64()*(r.Max-r.Min)
}

// RollInt returns a random integer in [Min, Max] (BDS IntRange).
func (r Range) RollInt(rnd *rand.Rand) int {
	lo, hi := int(math.Round(r.Min)), int(math.Round(r.Max))
	if hi <= lo {
		return lo
	}
	return lo + rnd.IntN(hi-lo+1)
}

// Ticks converts seconds to ticks.
func Ticks(seconds float64) int { return int(math.Round(seconds * 20)) }

// DamageCause returns the BDS ActorDamageCause name of a damage source, as
// used by damage_sensor "cause" and the has_damage filter.
func DamageCause(src world.DamageSource) string {
	switch s := src.(type) {
	case entity.AttackDamageSource:
		return "entity_attack"
	case entity.ProjectileDamageSource:
		return "projectile"
	case entity.VoidDamageSource:
		return "void"
	case entity.SuffocationDamageSource:
		return "suffocation"
	case entity.DrowningDamageSource:
		return "drowning"
	case entity.FallDamageSource:
		return "fall"
	case entity.GlideDamageSource:
		return "fly_into_wall"
	case entity.LightningDamageSource:
		return "lightning"
	case entity.ExplosionDamageSource:
		if _, ok := s.Source.(world.EntityExplosionSource); ok {
			return "entity_explosion"
		}
		return "block_explosion"
	case block.FireDamageSource:
		return "fire"
	case block.LavaDamageSource:
		return "lava"
	case block.MagmaDamageSource:
		return "magma"
	case block.DamageSource:
		return "contact"
	case effect.InstantDamageSource:
		return "magic"
	case effect.PoisonDamageSource:
		return "magic"
	case effect.WitherDamageSource:
		return "wither"
	case enchantment.ThornsDamageSource:
		return "thorns"
	case player.StarvationDamageSource:
		return "starve"
	}
	return "none"
}
