//go:build crossfly_full

package core

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// lookAt is minecraft:behavior.look_at_player and look_at_entity: now and
// then look at a nearby player (or entity matching filters) for a while
// (BDS LookAtActorGoal; look_at_player adds the filter is_family player).
//
//	look_distance 8, probability 0.02, look_time [2, 4] seconds,
//	angle_of_view_vertical / _horizontal 360, filters. Flags look.
type lookAt struct {
	mob.GoalBase
	dist, prob float64
	minT, maxT int
	fovV, fovH float64 // half angles
	players    bool
	filter     mob.Filter

	target    *world.EntityHandle
	lookTicks int
}

func newLookAt(players bool) mob.GoalFactory {
	return func(spec mob.GoalSpec) (mob.Goal, error) {
		j := struct {
			Dist    float64         `json:"look_distance"`
			Prob    float64         `json:"probability"`
			Time    *timeRange      `json:"look_time"`
			FovV    float64         `json:"angle_of_view_vertical"`
			FovH    float64         `json:"angle_of_view_horizontal"`
			Filters json.RawMessage `json:"filters"`
		}{Dist: 8, Prob: 0.02, FovV: 360, FovH: 360}
		if err := spec.Decode(&j); err != nil {
			return nil, fmt.Errorf("%v: %w", spec.Key, err)
		}
		t := timeRange{Min: 2, Max: 4}
		if j.Time != nil {
			t = *j.Time
		}
		f, err := mob.CompileFilter(j.Filters)
		if err != nil {
			return nil, fmt.Errorf("%v: %w", spec.Key, err)
		}
		return &lookAt{
			GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagLook)},
			dist:     j.Dist, prob: j.Prob,
			minT: int(t.Min * 20), maxT: int(t.Max * 20),
			fovV: math.Trunc(j.FovV) / 2, fovH: math.Trunc(j.FovH) / 2,
			players: players, filter: f,
		}, nil
	}
}

// inView is LookAtActorGoal::_withinFieldOfView: the target's eyes within
// the half angles of the mob's head.
func (g *lookAt) inView(m *mob.Mob, e world.Entity) bool {
	if g.fovH >= 180 && g.fovV >= 180 {
		return true
	}
	d := eyesOf(e).Sub(m.EyePosition())
	yaw := wrapDegrees(math.Atan2(d[2], d[0])*180/math.Pi - 90 - m.HeadYaw())
	pitch := wrapDegrees(-math.Atan2(d[1], math.Hypot(d[0], d[2])) * 180 / math.Pi)
	return math.Abs(pitch) <= g.fovV && math.Abs(yaw) <= g.fovH
}

func (g *lookAt) valid(m *mob.Mob, e world.Entity) bool {
	if !alive(m.Tx(), e) {
		return false
	}
	if g.players {
		if _, ok := isPlayer(e); !ok {
			return false
		}
	}
	ctx := m.Ctx()
	ctx.Other = e
	if t, ok := m.Target(); ok {
		ctx.Target = t
	}
	return g.filter.Test(ctx) && g.inView(m, e)
}

func (g *lookAt) CanUse(m *mob.Mob) bool {
	if m.Rand().Float64() >= g.prob {
		return false
	}
	g.target = nil
	for _, e := range nearby(m, g.dist, g.dist) {
		if g.valid(m, e) {
			g.target = e.H()
			break
		}
	}
	if g.target == nil {
		return false
	}
	_, ok := g.target.Entity(m.Tx())
	return ok
}

func (g *lookAt) CanContinueToUse(m *mob.Mob) bool {
	if g.target == nil {
		return false
	}
	e, ok := g.target.Entity(m.Tx())
	if !ok || !alive(m.Tx(), e) {
		return false
	}
	return distSq(m.Position(), e.Position()) <= g.dist*g.dist && g.inView(m, e) && g.lookTicks > 0
}

func (g *lookAt) Start(m *mob.Mob) { g.lookTicks = randInRange(m, g.minT, g.maxT) }

func (g *lookAt) Stop(*mob.Mob) { g.target = nil }

func (g *lookAt) Tick(m *mob.Mob) {
	if g.target != nil {
		if e, ok := g.target.Entity(m.Tx()); ok {
			m.LookControl().SetLookAt(eyesOf(e), 10, maxHeadXRot)
		}
	}
	g.lookTicks--
}

func (g *lookAt) AppendDebugInfo(b *strings.Builder) {
	fmt.Fprintf(b, "LookAtEntity %d ticks", g.lookTicks)
}

// randomLookAround is minecraft:behavior.random_look_around: now and then
// turn the head to a random direction (BDS RandomLookAroundGoal).
//
//	probability 0.02, look_time [2, 4], min/max_angle_of_view_horizontal
//	-180/180. Flags move|look. BDS hands look_time to the goal as ticks
//	(only look_at_player multiplies by 20), so the glance lasts 2-4 ticks.
type randomLookAround struct {
	mob.GoalBase
	minT, maxT int
	minA, maxA float64
	prob       float64

	base, yaw float64
	ticks     int
}

func newRandomLookAround(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Time *timeRange `json:"look_time"`
		MinA *float64   `json:"min_angle_of_view_horizontal"`
		MaxA *float64   `json:"max_angle_of_view_horizontal"`
		Prob *float64   `json:"probability"`
	}{}
	if err := spec.Decode(&j); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	g := &randomLookAround{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)},
		minT: 2, maxT: 4, minA: -180, maxA: 180, prob: 0.02}
	if j.Time != nil {
		g.minT, g.maxT = int(j.Time.Min), int(j.Time.Max)
	}
	if j.MinA != nil {
		g.minA = math.Trunc(*j.MinA)
	}
	if j.MaxA != nil {
		g.maxA = math.Trunc(*j.MaxA)
	}
	if j.Prob != nil {
		g.prob = *j.Prob
	}
	return g, nil
}

func (g *randomLookAround) CanUse(m *mob.Mob) bool { return m.Rand().Float64() < g.prob }

func (g *randomLookAround) CanContinueToUse(*mob.Mob) bool { return g.ticks >= 0 }

func (g *randomLookAround) Start(m *mob.Mob) {
	g.base = m.BodyYaw()
	g.yaw = g.minA + m.Rand().Float64()*(g.maxA-g.minA) + g.base
	g.ticks = randInRange(m, g.minT, g.maxT)
}

func (g *randomLookAround) Tick(m *mob.Mob) {
	g.ticks--
	rad := g.yaw * math.Pi / 180
	dir := mgl64.Vec3{-math.Sin(rad), 0, math.Cos(rad)}
	m.LookControl().SetLookAt(m.EyePosition().Add(dir), 180, 180)
}

func (g *randomLookAround) AppendDebugInfo(b *strings.Builder) {
	fmt.Fprintf(b, "RandomLookAround yaw %.0f", g.yaw)
}
