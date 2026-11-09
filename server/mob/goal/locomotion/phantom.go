//go:build crossfly_full

package locomotion

import (
	"fmt"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/goal/combat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// seaLevel is the overworld sea level the anchor stays above (BDS
// Dimension sea level, 62 in the overworld; Dragonfly worlds have none).
const seaLevel = 62

// circleAroundAnchor is minecraft:behavior.circle_around_anchor (BDS
// CircleAroundAnchorGoal, phantom): fly circles around an anchor point
// height_above_target_range above the ground (or above the target), angle
// steps of angle_change degrees on a radius of radius_range.
//
//	radius_change 1, radius_adjustment_chance 0.004,
//	height_adjustment_chance 0.002857, goal_radius 0.5, speed_multiplier 1,
//	angle_change 15, radius_range [5, 15], height_offset_range [0, 0],
//	height_above_target_range [0, 0]. Flags move|look.
//
// The radius grows by radius_change now and then and wraps to the minimum
// (turning the other way); the height offset is re-rolled now and then; a
// mob about to fly into the ground or ceiling moves its waypoint up or down
// a block. The next waypoint is taken once the current one is within
// goal_radius. The minimum of radius_range is not in the dump; 5 is Java's
// (Phantom circle radius 5 + 0..10).
type circleAroundAnchor struct {
	mob.GoalBase
	radiusChange, radiusChance, heightChance float64
	goalRadiusSq, speed, angleChange         float64
	radiusRange, heightOffsetRange           floatRange
	heightAbove                              floatRange

	anchor            mgl64.Vec3
	radius, heightOff float64
	angle, dir        float64
	wanted            mgl64.Vec3
}

func newCircleAroundAnchor(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		RadiusChange *float64   `json:"radius_change"`
		RadiusChance *float64   `json:"radius_adjustment_chance"`
		HeightChance *float64   `json:"height_adjustment_chance"`
		GoalRadius   *float64   `json:"goal_radius"`
		Speed        *float64   `json:"speed_multiplier"`
		AngleChange  *float64   `json:"angle_change"`
		Radius       floatRange `json:"radius_range"`
		HeightOffset floatRange `json:"height_offset_range"`
		HeightAbove  floatRange `json:"height_above_target_range"`
	}{}
	if err := decodeGoal(spec, &j); err != nil {
		return nil, err
	}
	g := &circleAroundAnchor{
		GoalBase:     mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)},
		radiusChange: 1, radiusChance: 0.004, heightChance: 0.002857, goalRadiusSq: 0.25, speed: 1, angleChange: 15,
		radiusRange: floatRange{Min: 5, Max: 15}, heightOffsetRange: j.HeightOffset, heightAbove: j.HeightAbove, dir: 1,
	}
	set := func(dst *float64, v *float64) {
		if v != nil {
			*dst = *v
		}
	}
	set(&g.radiusChange, j.RadiusChange)
	set(&g.radiusChance, j.RadiusChance)
	set(&g.heightChance, j.HeightChance)
	set(&g.speed, j.Speed)
	set(&g.angleChange, j.AngleChange)
	if j.GoalRadius != nil {
		g.goalRadiusSq = *j.GoalRadius * *j.GoalRadius
	}
	if j.Radius.set {
		g.radiusRange = j.Radius
	}
	return g, nil
}

func (g *circleAroundAnchor) CanUse(m *mob.Mob) bool { return m.MoveControl() != nil }

func (g *circleAroundAnchor) heightOffset(m *mob.Mob) float64 {
	r := g.heightOffsetRange
	if r.Max-r.Min > 0 {
		return r.Min + m.Rand().Float64()*(r.Max-r.Min)
	}
	return r.Min
}

func (g *circleAroundAnchor) Start(m *mob.Mob) {
	g.radius = g.radiusRange.Min + m.Rand().Float64()*(g.radiusRange.Max-g.radiusRange.Min)
	g.heightOff = g.heightOffset(m)
	g.dir = 1
	if m.Rand().IntN(2) == 0 {
		g.dir = -1
	}
	if t, ok := m.Target(); ok {
		g.anchorAbove(m, t)
	} else {
		h := g.heightAbove.roll(m)
		p := cube.PosFromVec3(m.Position())
		ground := int(m.Tx().HighestBlock(p[0], p[2])) + 1
		a := mgl64.Vec3{float64(p[0]), float64(ground), float64(p[2])}
		if h >= 0 {
			a[1] += math.Floor(h)
		}
		if a[1] <= seaLevel {
			a[1] = seaLevel + 1
		}
		g.anchor = a
	}
	g.selectNext(m)
}

func (g *circleAroundAnchor) anchorAbove(m *mob.Mob, t world.Entity) {
	p := t.Position()
	g.anchor = mgl64.Vec3{p[0], p[1] + g.heightAbove.roll(m), p[2]}
	if g.anchor[1] <= seaLevel {
		g.anchor[1] = seaLevel + 1
	}
}

func (g *circleAroundAnchor) selectNext(m *mob.Mob) {
	g.angle += g.dir * g.angleChange * math.Pi / 180
	g.wanted = mgl64.Vec3{
		g.anchor[0] + math.Cos(g.angle)*g.radius,
		g.anchor[1] + g.heightOff,
		g.anchor[2] + math.Sin(g.angle)*g.radius,
	}
	g.steer(m)
}

func (g *circleAroundAnchor) steer(m *mob.Mob) {
	if m.HasNavigation() && hasNavigation(m) {
		m.Navigation().MoveTo(m, g.wanted, g.speed)
	}
	m.MoveControl().SetWantedPosition(g.wanted, g.speed)
}

func (g *circleAroundAnchor) Tick(m *mob.Mob) {
	if t, ok := m.Target(); ok {
		g.anchorAbove(m, t)
	}
	r := m.Rand()
	if g.heightOffsetRange.Min < g.heightOffsetRange.Max && r.Float64() < g.heightChance {
		g.heightOff = g.heightOffset(m)
	}
	if g.radiusRange.Min < g.radiusRange.Max && r.Float64() < g.radiusChance {
		g.radius += g.radiusChange
		if g.radius > g.radiusRange.Max {
			g.dir = -g.dir
			g.radius = g.radiusRange.Min
		}
	}
	tx := m.Tx()
	pos := m.Position()
	h2 := (pos[0]-g.wanted[0])*(pos[0]-g.wanted[0]) + (pos[2]-g.wanted[2])*(pos[2]-g.wanted[2])
	if h2 < g.goalRadiusSq && g.wanted[1] < pos[1] && !airAt(tx, cube.PosFromVec3(pos).Side(cube.FaceDown)) {
		g.heightOff = math.Max(g.heightOff, 1)
		g.selectNext(m)
	}
	if h2 < g.goalRadiusSq && g.wanted[1] > pos[1] && !airAt(tx, cube.PosFromVec3(pos).Side(cube.FaceUp)) {
		g.heightOff = math.Min(g.heightOff, -1)
		g.selectNext(m)
	}
	reached := pos.Sub(g.wanted).LenSqr() < g.goalRadiusSq
	if hasNavigation(m) {
		reached = m.Navigation().Done()
	}
	if reached {
		g.selectNext(m)
		return
	}
	// BDS keeps the wanted position until the control reaches it; this
	// package's move controls clear it every tick, so it is set again.
	g.steer(m)
}

func (g *circleAroundAnchor) Stop(m *mob.Mob) {
	if hasNavigation(m) {
		m.Navigation().Stop(m)
	}
	m.MoveControl().Stop()
}

func (g *circleAroundAnchor) AppendDebugInfo(b *strings.Builder) {
	fmt.Fprintf(b, "CircleAroundAnchor r %.1f h %.1f", g.radius, g.heightOff)
}

// swoopAttack is minecraft:behavior.swoop_attack (BDS SwoopAttackGoal,
// phantom): dive at the target and hit it when the boxes (grown by
// damage_reach) touch.
//
//	damage_reach 0.2, speed_multiplier 1, delay_range [10, 20] s. Flags
//	move|look.
//
// It aborts after a hit, a horizontal collision or being hurt. Between dives
// it waits delay_min + rand x (delay_max - delay_min) x 20 ticks: BDS
// multiplies the already converted ticks by 20 again (200..4200 ticks with the
// defaults), which is kept.
type swoopAttack struct {
	mob.GoalBase
	reach, speed       float64
	delayMin, delayMax float64
	timer              int
	hit                bool
	startTick          int64
}

func newSwoopAttack(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Reach *float64   `json:"damage_reach"`
		Speed *float64   `json:"speed_multiplier"`
		Delay floatRange `json:"delay_range"`
	}{}
	if err := decodeGoal(spec, &j); err != nil {
		return nil, err
	}
	g := &swoopAttack{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, reach: 0.2, speed: 1, delayMin: 200, delayMax: 400}
	if j.Reach != nil {
		g.reach = *j.Reach
	}
	if j.Speed != nil {
		g.speed = *j.Speed
	}
	if j.Delay.set {
		g.delayMin, g.delayMax = j.Delay.Min*20, j.Delay.Max*20
	}
	return g, nil
}

func (g *swoopAttack) CanUse(m *mob.Mob) bool {
	if _, ok := m.Target(); !ok || m.MoveControl() == nil {
		return false
	}
	if g.timer > 0 {
		g.timer--
		return false
	}
	g.timer = int(m.Rand().Float64()*((g.delayMax-g.delayMin)*20) + g.delayMin)
	return true
}

func (g *swoopAttack) Start(m *mob.Mob) {
	g.hit = false
	g.startTick = m.Ticks()
	m.PlaySound("swoop")
}

func (g *swoopAttack) CanContinueToUse(m *mob.Mob) bool {
	if _, ok := m.Target(); !ok || g.hit || m.Physics().HorizontalCollision {
		return false
	}
	if v, ok := m.Data(lastHurtKey{}); ok && v.(lastHurt).tick >= g.startTick {
		return false
	}
	return true
}

func (g *swoopAttack) Tick(m *mob.Mob) {
	t, ok := m.Target()
	if !ok || !alive(m.Tx(), t) {
		return
	}
	box := entityBox(t)
	p := t.Position()
	p[1] += box.Height() * 0.5
	if hasNavigation(m) {
		m.Navigation().MoveTo(m, p, g.speed)
	} else {
		m.MoveControl().SetWantedPosition(p, g.speed)
	}
	if m.BBox().Grow(g.reach).IntersectsWith(box) {
		combat.DoHurtTarget(m, t)
		g.hit = true
	}
}

func (g *swoopAttack) Stop(m *mob.Mob) {
	if hasNavigation(m) {
		m.Navigation().Stop(m)
	} else {
		m.MoveControl().Stop()
	}
}

func (g *swoopAttack) AppendDebugInfo(b *strings.Builder) { b.WriteString("SwoopAttack") }
