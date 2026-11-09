//go:build crossfly_full

// Package physics is the travel step of definition-driven mobs (StageTravel):
// ground, air, water and lava movement with block collision, auto step,
// climbing, entity pushing, fall damage and knock back. It reads the input
// the controls (package mob/control) leave in Mob.Input and writes
// Mob.Physics.
//
// Sources: BDS 1.21.3 MobMovement{Speed,Friction,Drag,Gravity,Climb,
// ClimbOutOfLiquid}, MobJumpSystem, MobJumpFromGroundSystemImpl,
// MobTravelIntentSystemImpl, GroundTravelTypeSystemImpl, WaterTravelSystem,
// AutoStepSystem, CheckFallDamageSystemImpl, Mob::knockback, Mob::pushActors,
// PushableComponent::push, KnockbackRules. Where the dump only names a system
// (the order of the travel steps, liquid detection) Java's LivingEntity.travel
// is followed; those spots say "source: java".
package physics

import (
	"bytes"
	"encoding/json"
	"github.com/df-mc/dragonfly/server/block/cube"

	"github.com/df-mc/dragonfly/server/mob"
)

// DefaultMaxAutoStep is the step height of a mob without
// minecraft:variable_max_auto_step (BDS Mob mMaxAutoStep).
const DefaultMaxAutoStep = 0.5625

// DefaultJumpPower is the jump velocity of jump.static without jump_power.
const DefaultJumpPower = 0.42

// state is the per-mob physics state, kept in Mob.Data.
type state struct {
	// Set by components; the defaults are what a mob without them gets.
	gravity, collision bool
	canClimb           bool
	pushable           bool
	pushThrough        float64
	waterDrag          float64
	maxStep            float64

	// flying is set by fly/hover move controls: no gravity, air drag on Y.
	flying bool

	// Per-tick bookkeeping.
	noJumpDelay int
	horizontal  bool // the last move collided horizontally
	navRaw      []byte
	navParsed   bool
	navCanSink  bool
	// boxes, stepBoxes are the block box buffers of move.
	boxes, stepBoxes []cube.BBox
}

type stateKey struct{}

func newState() *state {
	return &state{gravity: true, collision: true, waterDrag: 0.8, maxStep: DefaultMaxAutoStep}
}

// stateOf returns m's physics state, creating it on first use.
func stateOf(m *mob.Mob) *state {
	if v, ok := m.Data(stateKey{}); ok {
		return v.(*state)
	}
	s := newState()
	m.SetData(stateKey{}, s)
	return s
}

// MaxAutoStep is how high m walks up without jumping (0.5625 by default,
// minecraft:variable_max_auto_step base_value).
func MaxAutoStep(m *mob.Mob) float64 { return stateOf(m).maxStep }

// HasGravity reports whether gravity pulls m down (minecraft:physics
// has_gravity, and not flying through SetFlying).
func HasGravity(m *mob.Mob) bool {
	s := stateOf(m)
	return s.gravity && !s.flying && !m.Meta().NoGravity
}

// HasCollision reports whether m collides with blocks (minecraft:physics
// has_collision).
func HasCollision(m *mob.Mob) bool { return stateOf(m).collision }

// SetFlying makes m fly (no gravity, air drag on every axis) or stop flying.
// The fly and hover move controls call it, like Java's FlyingMoveControl
// toggles setNoGravity.
func SetFlying(m *mob.Mob, v bool) { stateOf(m).flying = v }

// Flying reports whether SetFlying(m, true) is in effect.
func Flying(m *mob.Mob) bool { return stateOf(m).flying }

// CanClimb reports whether m climbs ladders and vines (minecraft:can_climb).
func CanClimb(m *mob.Mob) bool { return stateOf(m).canClimb }

// Pushable reports whether other entities push m (minecraft:pushable_by_entity
// or minecraft:pushable is_pushable).
func Pushable(m *mob.Mob) bool { return stateOf(m).pushable }

// AppliesLiquidGravity reports whether water or lava pulls m down this tick
// (Mob::shouldApplyWaterGravity): swimming move controls cancel it.
func AppliesLiquidGravity(m *mob.Mob) bool {
	p := m.Physics()
	if !p.InWater && !p.InLava {
		return false
	}
	return stateOf(m).liquidGravity(m, m.Tx(), m.Pos())
}

// navKeys are the navigation components whose can_sink matters for water
// gravity (BDS MobMovementGravity::shouldApplyLiquidGravity).
var navKeys = [...]string{
	"minecraft:navigation.walk", "minecraft:navigation.climb", "minecraft:navigation.generic",
	"minecraft:navigation.swim", "minecraft:navigation.float", "minecraft:navigation.fly",
	"minecraft:navigation.hover",
}

// navigation returns whether m has a navigation component and its can_sink
// (default true).
func (s *state) navigation(m *mob.Mob) (has, canSink bool) {
	for _, k := range navKeys {
		raw, ok := m.ComponentJSON(k)
		if !ok {
			continue
		}
		if !s.navParsed || !bytes.Equal(raw, s.navRaw) {
			v := struct {
				CanSink *bool `json:"can_sink"`
			}{}
			_ = json.Unmarshal(raw, &v)
			s.navCanSink = v.CanSink == nil || *v.CanSink
			s.navRaw, s.navParsed = append(s.navRaw[:0], raw...), true
		}
		return true, s.navCanSink
	}
	return false, true
}
