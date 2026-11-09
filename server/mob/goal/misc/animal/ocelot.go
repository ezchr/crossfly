//go:build crossfly_full

package animal

import (
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/goal/combat"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/kit"
)

// ocelotAttack is minecraft:behavior.ocelotattack (BDS OcelotAttackGoal,
// OcelotAttackDefinition; control flags move|look): a cat or ocelot stalks
// its target (a chicken, rabbit or baby turtle from
// nearest_attackable_target). Every tick it looks at the target, paths to it
// and picks a gait from the squared distance to the target's feet:
//
//	within reach      walk_speed_multiplier, neither sneaking nor sprinting
//	within sprint     sprint_speed_multiplier, sprinting
//	within sneak      sneak_speed_multiplier, sneaking
//	further           walk_speed_multiplier
//
// reach is reach_multiplier x its width. Each limit grows by 0.5 while the
// attack cooldown runs, and while sprinting (sneaking) the reach shrinks by
// 0.5 and the sprint range grows (shrinks) by 0.5, so the gait does not
// flicker. Within the plain reach it bites (Mob::doHurtTarget) once every
// cooldown_time. It keeps going while the target lives within max_distance
// and it has a path (or could start again). The pounce is
// behavior.leap_at_target, a goal of its own.
type ocelotAttack struct {
	mob.GoalBase
	debugName
	cooldown                      int
	xRot, yRot                    float64
	maxDist, sneakRange, sprint   float64
	reach                         float64
	sneakSpeed, sprintSpeed, walk float64

	attackTicks int
}

func newOcelotAttack(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Cooldown    *float64 `json:"cooldown_time"`
		XRot        *float64 `json:"x_max_rotation"`
		YRot        *float64 `json:"y_max_head_rotation"`
		MaxDist     *float64 `json:"max_distance"`
		SneakRange  *float64 `json:"max_sneak_range"`
		SprintRange *float64 `json:"max_sprint_range"`
		Reach       *float64 `json:"reach_multiplier"`
		SneakSpeed  *float64 `json:"sneak_speed_multiplier"`
		SprintSpeed *float64 `json:"sprint_speed_multiplier"`
		WalkSpeed   *float64 `json:"walk_speed_multiplier"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	return &ocelotAttack{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, debugName: "OcelotAttack",
		// OcelotAttackDefinition::initialize: (int)(cooldown_time * 20), truncated.
		cooldown: int(kit.Or(v.Cooldown, 1) * 20), xRot: kit.Or(v.XRot, 30), yRot: kit.Or(v.YRot, 30),
		maxDist: kit.Or(v.MaxDist, 15), sneakRange: kit.Or(v.SneakRange, 15), sprint: kit.Or(v.SprintRange, 4),
		reach: kit.Or(v.Reach, 2), sneakSpeed: kit.Or(v.SneakSpeed, 0.6), sprintSpeed: kit.Or(v.SprintSpeed, 1.33),
		walk: kit.Or(v.WalkSpeed, 0.8)}, nil
}

func (g *ocelotAttack) params() map[string]float64 {
	return map[string]float64{"cooldown": float64(g.cooldown), "x_max_rotation": g.xRot, "y_max_head_rotation": g.yRot,
		"max_distance": g.maxDist, "max_sneak_range": g.sneakRange, "max_sprint_range": g.sprint, "reach_multiplier": g.reach,
		"sneak_speed": g.sneakSpeed, "sprint_speed": g.sprintSpeed, "walk_speed": g.walk, "flags": float64(g.Control)}
}

// CanUse ...
func (g *ocelotAttack) CanUse(m *mob.Mob) bool {
	if !m.HasNavigation() {
		return false
	}
	_, ok := m.Target()
	return ok
}

// CanContinueToUse ...
func (g *ocelotAttack) CanContinueToUse(m *mob.Mob) bool {
	t, ok := m.Target()
	if !ok || !kit.Alive(t) || kit.DistSq(m.Position(), t.Position()) > g.maxDist*g.maxDist {
		return false
	}
	return !m.Navigation().Done() || g.CanUse(m)
}

// Stop ...
func (g *ocelotAttack) Stop(m *mob.Mob) {
	kit.Stop(m)
	setGait(m, false, false)
}

// Tick ...
func (g *ocelotAttack) Tick(m *mob.Mob) {
	t, ok := m.Target()
	if !ok {
		return
	}
	m.LookControl().SetLookAtEntity(t, g.yRot, g.xRot)
	feet := t.Position()
	feet[1] = kit.BBox(t).Min()[1]
	d := kit.DistSq(m.Position(), feet)
	sneak, sprint := g.sneakRange, g.sprint
	reach := g.reach * m.BBox().Width()
	r := reach
	switch {
	case g.attackTicks > 0:
		r, sprint, sneak = reach+0.5, sprint+0.5, sneak+0.5
	case m.Sprinting():
		r, sprint, sneak = reach-0.5, sprint+0.5, sneak+0.5
	case m.Sneaking():
		r, sprint, sneak = reach-0.5, sprint-0.5, sneak+0.5
	}
	speed := g.walk
	switch {
	case d < r*r || d >= sneak*sneak:
		setGait(m, false, false)
	case d < sprint*sprint:
		speed = g.sprintSpeed
		setGait(m, false, true)
	default:
		speed = g.sneakSpeed
		setGait(m, true, false)
	}
	m.Navigation().MoveToEntity(m, t, speed)
	g.attackTicks = max(g.attackTicks-1, 0)
	if d <= reach*reach && g.attackTicks <= 0 {
		g.attackTicks = g.cooldown
		combat.DoHurtTarget(m, t)
	}
}

// setGait shows m sneaking or sprinting (BDS Actor::setSneaking,
// Mob::setSprinting).
func setGait(m *mob.Mob, sneaking, sprinting bool) {
	m.UpdateMeta(func(meta *mob.Meta) { meta.Sneaking, meta.Sprinting = sneaking, sprinting })
}
