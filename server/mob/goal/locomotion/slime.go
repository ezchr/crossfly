//go:build crossfly_full

package locomotion

import (
	"fmt"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/go-gl/mathgl/mgl64"
)

// The slime goals drive SlimeMoveControl (movement.jump) the BDS way: they
// set the control's speed modifier and "has wanted position", which makes it
// hop along the facing. This package's move controls steer to a point, so
// the goals ask for a point ahead along the facing instead.

// hopAhead asks the move control to hop forward along the facing.
func hopAhead(m *mob.Mob, speed float64) {
	yaw := mgl64.DegToRad(m.Yaw())
	ahead := m.Position().Add(mgl64.Vec3{-math.Sin(yaw) * 2, 0, math.Cos(yaw) * 2})
	m.MoveControl().SetWantedPosition(ahead, speed)
}

// slimeFloat is minecraft:behavior.slime_float (BDS SlimeFloatGoal): in water
// or lava, jump with jump_chance_percentage (0.8) a tick and keep moving at
// speed_multiplier (1.2). Flags move|jump.
type slimeFloat struct {
	mob.GoalBase
	chance, speed float64
}

func newSlimeFloat(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Chance *float64 `json:"jump_chance_percentage"`
		Speed  *float64 `json:"speed_multiplier"`
	}{}
	if err := decodeGoal(spec, &j); err != nil {
		return nil, err
	}
	g := &slimeFloat{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagJump)}, chance: 0.8, speed: 1.2}
	if j.Chance != nil {
		g.chance = *j.Chance
	}
	if j.Speed != nil {
		g.speed = *j.Speed
	}
	return g, nil
}

func (g *slimeFloat) CanUse(m *mob.Mob) bool { return inLiquid(m) }

func (g *slimeFloat) Tick(m *mob.Mob) {
	if m.Rand().Float64() < g.chance {
		m.JumpControl().Jump()
	}
	hopAhead(m, g.speed)
}

func (g *slimeFloat) AppendDebugInfo(b *strings.Builder) { b.WriteString("SlimeFloat") }

// slimeKeepOnJumping is minecraft:behavior.slime_keep_on_jumping (BDS
// SlimeKeepOnJumpingGoal): always hop forward at speed_multiplier (1). Flags
// move|jump.
type slimeKeepOnJumping struct {
	mob.GoalBase
	speed float64
}

func newSlimeKeepOnJumping(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Speed *float64 `json:"speed_multiplier"`
	}{}
	if err := decodeGoal(spec, &j); err != nil {
		return nil, err
	}
	g := &slimeKeepOnJumping{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagJump)}, speed: 1}
	if j.Speed != nil {
		g.speed = *j.Speed
	}
	return g, nil
}

// CanUse: the BDS body is not in the dump; Java's SlimeKeepOnJumpingGoal
// runs whenever the mob is not riding.
func (g *slimeKeepOnJumping) CanUse(m *mob.Mob) bool { return !m.Riding() }

func (g *slimeKeepOnJumping) Tick(m *mob.Mob) { hopAhead(m, g.speed) }

func (g *slimeKeepOnJumping) AppendDebugInfo(b *strings.Builder) {
	b.WriteString("SlimeKeepOnJumping")
}

// slimeRandomDirection is minecraft:behavior.slime_random_direction (BDS
// SlimeRandomDirectionGoal): without a target, on the ground or in a liquid,
// face a new random yaw in [0, turn_range) every min_change_direction_time +
// 0..add_random_time_range seconds, turning at most max_turn a tick.
//
//	add_random_time_range 3, turn_range 360, min_change_direction_time 2.
//	Flags look.
type slimeRandomDirection struct {
	mob.GoalBase
	minTicks, addTicks, turnRange int
	timer                         int
	yaw                           float64
}

func newSlimeRandomDirection(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Add  *int     `json:"add_random_time_range"`
		Turn *int     `json:"turn_range"`
		Min  *float64 `json:"min_change_direction_time"`
	}{}
	if err := decodeGoal(spec, &j); err != nil {
		return nil, err
	}
	g := &slimeRandomDirection{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagLook)}, minTicks: 40, addTicks: 60, turnRange: 360}
	if j.Add != nil {
		g.addTicks = *j.Add * 20
	}
	if j.Turn != nil {
		g.turnRange = *j.Turn
	}
	if j.Min != nil {
		g.minTicks = int(*j.Min * 20)
	}
	return g, nil
}

func (g *slimeRandomDirection) CanUse(m *mob.Mob) bool {
	if _, ok := m.Target(); ok {
		return false
	}
	return m.OnGround() || inLiquid(m)
}

func (g *slimeRandomDirection) Tick(m *mob.Mob) {
	if g.timer--; g.timer <= 0 {
		g.timer = g.minTicks
		if g.addTicks > 0 {
			g.timer += m.Rand().IntN(g.addTicks)
		}
		if g.turnRange > 0 {
			g.yaw = float64(m.Rand().IntN(g.turnRange))
		}
	}
	m.SetYaw(clampRotate(m.Yaw(), g.yaw, maxTurn(m)))
}

// maxTurn is the move control's max_turn (30 by default).
func maxTurn(m *mob.Mob) float64 {
	if c, ok := m.MoveControl().(interface{ MaxTurn() float64 }); ok {
		return c.MaxTurn()
	}
	return 30
}

func (g *slimeRandomDirection) AppendDebugInfo(b *strings.Builder) {
	fmt.Fprintf(b, "SlimeRandomDirection yaw %.0f", g.yaw)
}

// slimeAttack is minecraft:behavior.slime_attack (BDS SlimeAttackGoal): face
// the target (x/y_max_rotation 10) and hop at it at speed_multiplier (1) for
// grow_tired_cooldown_time seconds (15) at a time; with set_persistent a
// player target makes the slime persistent. The damage itself is
// minecraft:area_attack. Flags look.
type slimeAttack struct {
	mob.GoalBase
	xMax, yMax float64
	speed      float64
	tired      int
	persistent bool
	timer      int
}

func newSlimeAttack(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		XMax       *float64 `json:"x_max_rotation"`
		YMax       *float64 `json:"y_max_rotation"`
		Speed      *float64 `json:"speed_multiplier"`
		Tired      *float64 `json:"grow_tired_cooldown_time"`
		Persistent bool     `json:"set_persistent"`
	}{}
	if err := decodeGoal(spec, &j); err != nil {
		return nil, err
	}
	g := &slimeAttack{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagLook)}, xMax: 10, yMax: 10, speed: 1, tired: 300, persistent: j.Persistent}
	if j.XMax != nil {
		g.xMax = *j.XMax
	}
	if j.YMax != nil {
		g.yMax = *j.YMax
	}
	if j.Speed != nil {
		g.speed = *j.Speed
	}
	if j.Tired != nil {
		g.tired = int(*j.Tired * 20)
	}
	return g, nil
}

func (g *slimeAttack) CanUse(m *mob.Mob) bool {
	if !hasNavigation(m) {
		return false
	}
	t, ok := m.Target()
	if !ok || !alive(m.Tx(), t) {
		return false
	}
	if g.persistent && sense.IsPlayer(t) {
		m.SetPersistent(true)
	}
	return true
}

func (g *slimeAttack) CanContinueToUse(m *mob.Mob) bool {
	t, ok := m.Target()
	if !ok || !alive(m.Tx(), t) {
		return false
	}
	g.timer--
	return g.timer > 0
}

func (g *slimeAttack) Start(*mob.Mob) { g.timer = g.tired }

func (g *slimeAttack) Tick(m *mob.Mob) {
	t, ok := m.Target()
	if !ok {
		return
	}
	// Mob::lookAt turns the body straight at the target.
	yaw := clampRotate(m.Yaw(), sense.YawTowards(m.Position(), t.Position()), g.yMax)
	m.SetYaw(yaw)
	m.LookControl().SetLookAtEntity(t, g.yMax, g.xMax)
	m.Navigation().MoveTo(m, t.Position(), g.speed)
}

func (g *slimeAttack) Stop(m *mob.Mob) { m.Navigation().Stop(m) }

func (g *slimeAttack) AppendDebugInfo(b *strings.Builder) { b.WriteString("SlimeAttack") }
