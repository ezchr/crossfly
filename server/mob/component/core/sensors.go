//go:build crossfly_full

package core

import (
	"encoding/json"
	"fmt"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
)

// ambientSound is minecraft:ambient_sound_interval: plays the ambient sound
// every value + random(range) seconds (BDS AmbientSoundIntervalDefinition;
// defaults value 8, range 16, event "ambient"). event_names picks the first
// entry whose Molang condition holds.
type ambientSound struct {
	value, rng float64
	event      string
	names      []ambientName
	left       int
}

type ambientName struct {
	cond  Expr
	event string
}

func newAmbientSound(key string, raw json.RawMessage) (mob.Component, error) {
	j := struct {
		Value     *float64 `json:"value"`
		Range     *float64 `json:"range"`
		EventName string   `json:"event_name"`
		Names     []struct {
			Condition string `json:"condition"`
			EventName string `json:"event_name"`
		} `json:"event_names"`
	}{}
	if err := decode(raw, &j); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	c := &ambientSound{value: 8, rng: 16, event: "ambient"}
	if j.Value != nil {
		c.value = *j.Value
	}
	if j.Range != nil {
		c.rng = *j.Range
	}
	if j.EventName != "" {
		c.event = j.EventName
	}
	for _, n := range j.Names {
		e, err := CompileMolang(n.Condition)
		if err != nil {
			return nil, fmt.Errorf("%v: %w", key, err)
		}
		c.names = append(c.names, ambientName{cond: e, event: n.EventName})
	}
	return c, nil
}

func (c *ambientSound) reset(m *mob.Mob) {
	c.left = ticksOf(c.value + m.Rand().Float64()*c.rng)
}

func (c *ambientSound) Attach(m *mob.Mob) { c.reset(m) }
func (c *ambientSound) Detach(*mob.Mob)   {}

func (c *ambientSound) Tick(m *mob.Mob) {
	if c.left--; c.left > 0 {
		return
	}
	c.reset(m)
	PlayMobSound(m, c.Event(m))
}

// Event returns the ambient sound event to play now.
func (c *ambientSound) Event(m *mob.Mob) string {
	for _, n := range c.names {
		if n.cond.Eval(m) != 0 {
			return n.event
		}
	}
	return c.event
}

// hurtOnCondition is minecraft:hurt_on_condition: every tick each damage
// condition whose filters pass deals damage_per_tick of its cause (BDS
// HurtOnConditionSystem).
type hurtOnCondition struct {
	conds []hurtCondition
}

type hurtCondition struct {
	filter mob.Filter
	cause  string
	damage float64
}

func newHurtOnCondition(key string, raw json.RawMessage) (mob.Component, error) {
	var j struct {
		Conditions []struct {
			Filters json.RawMessage `json:"filters"`
			Cause   string          `json:"cause"`
			Damage  *float64        `json:"damage_per_tick"`
		} `json:"damage_conditions"`
	}
	if err := decode(raw, &j); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	c := &hurtOnCondition{}
	for _, d := range j.Conditions {
		f, err := mob.CompileFilter(d.Filters)
		if err != nil {
			return nil, fmt.Errorf("%v: %w", key, err)
		}
		hc := hurtCondition{filter: f, cause: d.Cause, damage: 1}
		if hc.cause == "" {
			hc.cause = "none"
		}
		if d.Damage != nil {
			hc.damage = *d.Damage
		}
		c.conds = append(c.conds, hc)
	}
	return c, nil
}

func (c *hurtOnCondition) Attach(*mob.Mob) {}
func (c *hurtOnCondition) Detach(*mob.Mob) {}

func (c *hurtOnCondition) Tick(m *mob.Mob) {
	for _, d := range c.conds {
		if d.filter.Test(m.Ctx()) {
			m.Hurt(d.damage, SourceOf(d.cause))
			if m.Dead() {
				return
			}
		}
	}
}

// TickStage runs hurt_on_condition with the environment damage.
func (c *hurtOnCondition) TickStage() mob.Stage { return mob.StageEnvironment }

// breathable is minecraft:breathable: air supply, refilled where the mob can
// breathe and used up elsewhere; at the end of it the mob takes 2 drowning
// (or suffocation) damage (BDS BreathableComponent + the breathe system).
//
//	total_supply     seconds of air (default 15)
//	suffocate_time   seconds below 0 before damage (default -1)
//	inhale_time      seconds to refill (default 0: at once)
//	breathes_air (true), breathes_water (false), breathes_lava (true),
//	breathes_solids (false), generates_bubbles (true)
type breathable struct {
	max, suffocate, inhale    int
	air                       int
	airOK, water, lava, solid bool
}

func newBreathable(key string, raw json.RawMessage) (mob.Component, error) {
	j := struct {
		TotalSupply    *float64 `json:"total_supply"`
		TotalSupplyOld *float64 `json:"totalSupply"`
		Suffocate      *float64 `json:"suffocate_time"`
		SuffocateOld   *float64 `json:"suffocateTime"`
		Inhale         *float64 `json:"inhale_time"`
		Air            *bool    `json:"breathes_air"`
		Water          *bool    `json:"breathes_water"`
		Lava           *bool    `json:"breathes_lava"`
		Solids         *bool    `json:"breathes_solids"`
	}{}
	if err := decode(raw, &j); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	f := func(a, b *float64, def float64) float64 {
		if a != nil {
			return *a
		}
		if b != nil {
			return *b
		}
		return def
	}
	bo := func(v *bool, def bool) bool {
		if v != nil {
			return *v
		}
		return def
	}
	c := &breathable{
		max:       ticksOf(f(j.TotalSupply, j.TotalSupplyOld, 15)),
		suffocate: ticksOf(f(j.Suffocate, j.SuffocateOld, -1)),
		inhale:    ticksOf(f(j.Inhale, nil, 0)),
		airOK:     bo(j.Air, true), water: bo(j.Water, false), lava: bo(j.Lava, true), solid: bo(j.Solids, false),
	}
	c.air = c.max
	return c, nil
}

func (c *breathable) Attach(*mob.Mob) {}
func (c *breathable) Detach(*mob.Mob) {}

// Air returns the air supply and its maximum in ticks.
func (c *breathable) Air() (air, maxAir int) { return c.air, c.max }

// medium returns what the mob's eyes are in: 0 air, 1 water, 2 lava, 3 a solid block.
func medium(m *mob.Mob) int {
	tx := m.Tx()
	eye := m.EyePosition()
	pos := cube.PosFromVec3(eye)
	if l, ok := tx.Liquid(pos); ok {
		top := float64(pos[1]) + 1 - (1-float64(l.LiquidDepth())/8)*0.9
		if eye[1] < top {
			if _, water := l.(block.Water); water {
				return 1
			}
			return 2
		}
	}
	b := tx.Block(pos)
	if _, air := b.(block.Air); air {
		return 0
	}
	for _, bb := range b.Model().BBox(pos, tx) {
		if bb.Translate(pos.Vec3()).Grow(-0.001).Vec3Within(eye) && b.Model().FaceSolid(pos, cube.FaceUp, tx) {
			return 3
		}
	}
	return 0
}

func (c *breathable) canBreathe(m *mob.Mob, med int) bool {
	if _, ok := m.Effect(effect.WaterBreathing); ok {
		return true
	}
	if _, ok := m.Effect(effect.ConduitPower); ok {
		return true
	}
	switch med {
	case 1:
		return c.water
	case 2:
		return c.lava
	case 3:
		return c.solid
	}
	return c.airOK
}

func (c *breathable) Tick(m *mob.Mob) {
	med := medium(m)
	if c.canBreathe(m, med) {
		if c.air < c.max {
			if c.inhale > 0 {
				c.air = min(c.max, max(c.air, 0)+max(c.max/c.inhale, 1))
			} else {
				c.air = c.max
			}
		}
		return
	}
	c.air--
	if c.air > c.suffocate {
		return
	}
	c.air = 0
	var src world.DamageSource = entity.SuffocationDamageSource{}
	if med == 1 {
		src = entity.DrowningDamageSource{}
	}
	m.Hurt(2, src)
}

func (c *breathable) EncodeNBT() map[string]any { return map[string]any{"Air": int16(c.air)} }
func (c *breathable) DecodeNBT(_ *mob.Mob, d map[string]any) {
	if v, ok := d["Air"].(int16); ok {
		c.air = int(v)
	}
}
