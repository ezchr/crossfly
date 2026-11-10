//go:build crossfly_full

package physics

import (
	"encoding/json"
	"math"
	"slices"

	"github.com/df-mc/dragonfly/server/mob"
)

func decode(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, v)
}

// physicsComponent is minecraft:physics: has_gravity and has_collision
// (default true). push_towards_closest_space is not modelled.
type physicsComponent struct{ gravity, collision bool }

func newPhysics(_ string, raw json.RawMessage) (mob.Component, error) {
	v := struct {
		HasGravity   *bool `json:"has_gravity"`
		HasCollision *bool `json:"has_collision"`
	}{}
	if err := decode(raw, &v); err != nil {
		return nil, err
	}
	return &physicsComponent{gravity: v.HasGravity == nil || *v.HasGravity, collision: v.HasCollision == nil || *v.HasCollision}, nil
}

func (c *physicsComponent) Attach(m *mob.Mob) {
	s := stateOf(m)
	s.gravity, s.collision = c.gravity, c.collision
	if m.Meta().NoGravity != !c.gravity {
		m.UpdateMeta(func(meta *mob.Meta) { meta.NoGravity = !c.gravity })
	}
}

func (c *physicsComponent) Detach(m *mob.Mob) {
	s := stateOf(m)
	s.gravity, s.collision = true, true
	if m.Meta().NoGravity && !c.gravity {
		m.UpdateMeta(func(meta *mob.Meta) { meta.NoGravity = false })
	}
}

// flagComponent sets one boolean of the physics state while it is active
// (minecraft:can_climb, minecraft:pushable_by_entity, minecraft:pushable).
type flagComponent struct {
	set func(s *state, v bool)
	v   bool
}

func (c flagComponent) Attach(m *mob.Mob) { c.set(stateOf(m), c.v) }
func (c flagComponent) Detach(m *mob.Mob) { c.set(stateOf(m), false) }

func setCanClimb(s *state, v bool) { s.canClimb = v }
func setPushable(s *state, v bool) { s.pushable = v }

func newCanClimb(string, json.RawMessage) (mob.Component, error) {
	return flagComponent{set: setCanClimb, v: true}, nil
}

// newPushableByEntity is minecraft:pushable_by_entity. The mob is pushed
// normally by everything; presets add special push modes against given
// entities (the ball push of sulfur cubes, push_presets.go).
func newPushableByEntity(_ string, raw json.RawMessage) (mob.Component, error) {
	if c, ok, err := newPushPresets(raw); ok || err != nil {
		return c, err
	}
	return flagComponent{set: setPushable, v: true}, nil
}

// newPushable is the legacy minecraft:pushable {is_pushable,
// is_pushable_by_piston}; only is_pushable (default true) matters here.
func newPushable(_ string, raw json.RawMessage) (mob.Component, error) {
	v := struct {
		IsPushable *bool `json:"is_pushable"`
	}{}
	if err := decode(raw, &v); err != nil {
		return nil, err
	}
	return flagComponent{set: setPushable, v: v.IsPushable == nil || *v.IsPushable}, nil
}

// floatComponent sets one number of the physics state while it is active,
// and restores the default when it goes away.
type floatComponent struct {
	set    func(s *state, v float64)
	v, def float64
}

func (c floatComponent) Attach(m *mob.Mob) { c.set(stateOf(m), c.v) }
func (c floatComponent) Detach(m *mob.Mob) { c.set(stateOf(m), c.def) }

// newWaterMovement is minecraft:water_movement {drag_factor} (default 0.8,
// MobMovementDrag::tickApplyWaterDrag).
func newWaterMovement(_ string, raw json.RawMessage) (mob.Component, error) {
	v := struct {
		DragFactor *float64 `json:"drag_factor"`
	}{}
	if err := decode(raw, &v); err != nil {
		return nil, err
	}
	f := 0.8
	if v.DragFactor != nil {
		f = *v.DragFactor
	}
	return floatComponent{set: func(s *state, v float64) { s.waterDrag = v }, v: f, def: 0.8}, nil
}

// newVariableMaxAutoStep is minecraft:variable_max_auto_step. Without riding
// only base_value (default 0.5625) is used.
func newVariableMaxAutoStep(_ string, raw json.RawMessage) (mob.Component, error) {
	v := struct {
		BaseValue *float64 `json:"base_value"`
	}{}
	if err := decode(raw, &v); err != nil {
		return nil, err
	}
	step := DefaultMaxAutoStep
	if v.BaseValue != nil {
		step = math.Max(*v.BaseValue, 0)
	}
	return floatComponent{set: func(s *state, v float64) { s.maxStep = v }, v: step, def: DefaultMaxAutoStep}, nil
}

// newPushThrough is minecraft:push_through {value}: how much of a push the
// mob lets through (BDS ActorCollision push-through, 0 by default).
func newPushThrough(_ string, raw json.RawMessage) (mob.Component, error) {
	v := struct {
		Value float64 `json:"value"`
	}{}
	if err := decode(raw, &v); err != nil {
		return nil, err
	}
	return floatComponent{set: func(s *state, v float64) { s.pushThrough = v }, v: v.Value, def: 0}, nil
}

// newSlowdownImmunity is minecraft:block_movement_slowdown_immunity
// {blocks}. Only "minecraft:web" has an effect (Dragonfly's cobweb is the
// only block that slows mobs).
func newSlowdownImmunity(_ string, raw json.RawMessage) (mob.Component, error) {
	v := struct {
		Blocks []string `json:"blocks"`
	}{}
	if err := decode(raw, &v); err != nil {
		return nil, err
	}
	web := slices.Contains(v.Blocks, "minecraft:web") || slices.Contains(v.Blocks, "web")
	return webImmunityComponent{web: web}, nil
}

// webImmunityComponent makes the core environment step skip cobwebs.
type webImmunityComponent struct{ web bool }

func (c webImmunityComponent) Attach(m *mob.Mob) { m.SetIgnoresCobwebs(c.web) }
func (c webImmunityComponent) Detach(m *mob.Mob) { m.SetIgnoresCobwebs(false) }
