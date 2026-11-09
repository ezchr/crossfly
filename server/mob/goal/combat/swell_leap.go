//go:build crossfly_full

package combat

import (
	"fmt"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// swell is minecraft:behavior.swell (BDS SwellGoal): the creeper stops and
// swells (Meta.Swell 1) while its fuse is lit (Meta.Ignited, set by
// minecraft:explode) and the target is within stop_distance and visible,
// and shrinks (-1) otherwise.
type swell struct {
	mob.GoalBase
	start, stop float64 // +80, +84
	target      *world.EntityHandle
}

func newSwell(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		StartDistance *float64 `json:"start_distance"`
		StopDistance  *float64 `json:"stop_distance"`
	}
	if err := spec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	return &swell{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)}, start: or(v.StartDistance, 10), stop: or(v.StopDistance, 2)}, nil
}

// CanUse is SwellGoal::canUse.
func (g *swell) CanUse(m *mob.Mob) bool {
	if m.Swell() > 0 {
		return true
	}
	t, ok := m.Target()
	return ok && sense.BoxDistance(m.BBox(), sense.BBox(t)) < g.start && sense.CanSee(m, t)
}

// Start is SwellGoal::start.
func (g *swell) Start(m *mob.Mob) {
	m.Navigation().Stop(m)
	m.MoveControl().Stop()
	if t, ok := m.Target(); ok {
		g.target = t.H()
	}
}

// Stop is SwellGoal::stop.
func (g *swell) Stop(*mob.Mob) { g.target = nil }

// Tick is SwellGoal::tick.
func (g *swell) Tick(m *mob.Mob) {
	if g.target == nil {
		return
	}
	t, ok := g.target.Entity(m.Tx())
	if !ok {
		return
	}
	dir := int32(-1)
	if m.Ignited() && sense.BoxDistance(m.BBox(), sense.BBox(t)) <= g.stop && sense.CanSee(m, t) {
		dir = 1
	}
	m.UpdateMeta(func(meta *mob.Meta) { meta.Swell = dir })
}

// AppendDebugInfo ...
func (g *swell) AppendDebugInfo(b *strings.Builder) { b.WriteString("Swell") }

// leapAtTarget is minecraft:behavior.leap_at_target (BDS LeapAtTargetGoal):
// a 1-in-5 chance per tick to jump at a target 2..4 blocks away.
type leapAtTarget struct {
	mob.GoalBase
	yd             float64 // +96
	mustBeOnGround bool    // +100
	setPersistent  bool    // +112
	target         *world.EntityHandle
}

func newLeapAtTarget(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Yd             *float64 `json:"yd"`
		MustBeOnGround *bool    `json:"must_be_on_ground"`
		SetPersistent  bool     `json:"set_persistent"`
	}
	if err := spec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	g := &leapAtTarget{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagJump)}, yd: or(v.Yd, 0), mustBeOnGround: true, setPersistent: v.SetPersistent}
	if v.MustBeOnGround != nil {
		g.mustBeOnGround = *v.MustBeOnGround
	}
	return g, nil
}

// CanUse is LeapAtTargetGoal::canUse.
func (g *leapAtTarget) CanUse(m *mob.Mob) bool {
	t, ok := m.Target()
	if !ok {
		return false
	}
	d := m.Pos().Sub(t.Position()).LenSqr()
	if d < 4 || d > 16 || (g.mustBeOnGround && !m.OnGround()) {
		return false
	}
	if m.Rand().IntN(5) != 0 {
		return false
	}
	g.target = t.H()
	if g.setPersistent && sense.IsPlayer(t) {
		m.SetPersistent(true)
	}
	return true
}

// CanContinueToUse is LeapAtTargetGoal::canContinueToUse: until landing.
func (g *leapAtTarget) CanContinueToUse(m *mob.Mob) bool { return !m.OnGround() }

// Start is LeapAtTargetGoal::start: 0.4 towards the target plus 1.2x the
// current horizontal motion, and yd upwards.
func (g *leapAtTarget) Start(m *mob.Mob) {
	if g.target == nil {
		return
	}
	t, ok := g.target.Entity(m.Tx())
	if !ok {
		return
	}
	v := m.Velocity()
	dx, dz := t.Position()[0]-m.Pos()[0], t.Position()[2]-m.Pos()[2]
	if l := math.Sqrt(dx*dx + dz*dz); l != 0 {
		v[0] = dx/l*0.4 + v[0]*1.2
		v[2] = dz/l*0.4 + v[2]*1.2
	}
	v[1] = g.yd
	m.SetVelocity(v)
}

// AppendDebugInfo ...
func (g *leapAtTarget) AppendDebugInfo(b *strings.Builder) { b.WriteString("LeapAtTarget") }

// fleeSun is minecraft:behavior.flee_sun (BDS FleeSunGoal over
// FindCoverGoal): during the day, a burning mob under open sky walks to a
// random shaded position within 10 blocks.
type fleeSun struct {
	mob.GoalBase
	speed    float64
	cooldown int // ticks after stopping (flee_sun: 0)
	next     int64
	wanted   mgl64.Vec3
	sunOnly  bool
}

func newFleeSun(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		SpeedMultiplier *float64 `json:"speed_multiplier"`
	}
	if err := spec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	return &fleeSun{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)}, speed: or(v.SpeedMultiplier, 1), sunOnly: true}, nil
}

// CanUse is FleeSunGoal::canUse: day, burning, then FindCoverGoal::canUse.
func (g *fleeSun) CanUse(m *mob.Mob) bool {
	tx := m.Tx()
	if g.sunOnly && (!sense.IsDay(tx) || m.OnFireDuration() <= 0) {
		return false
	}
	if m.Ticks() < g.next {
		return false
	}
	b := m.BBox()
	if !sense.SkyVisible(tx, cube.Pos{int(math.Floor(m.Pos()[0])), int(math.Floor(b.Min()[1])), int(math.Floor(m.Pos()[2]))}) {
		return false
	}
	p, ok := FindShade(m)
	if !ok {
		return false
	}
	g.wanted = p
	return true
}

// FindShade tries 10 random positions within 10 blocks (3 up/down) for one
// without sky light that is brighter than half (FindCoverGoal::canUse; the
// walk target value check is Monster's 0.5 - brightness < 0).
func FindShade(m *mob.Mob) (mgl64.Vec3, bool) {
	tx, r := m.Tx(), m.Rand()
	pos, minY := m.Pos(), m.BBox().Min()[1]
	for i := 0; i < 10; i++ {
		p := cube.Pos{
			int(math.Floor(float64(r.IntN(20)) + pos[0] - 10)),
			int(math.Floor(float64(r.IntN(6)) + minY - 3)),
			int(math.Floor(float64(r.IntN(20)) + pos[2] - 10)),
		}
		if !sense.SkyVisible(tx, p) && 0.5-sense.Brightness(tx, p) < 0 {
			return mgl64.Vec3{float64(p[0]), float64(p[1]), float64(p[2])}, true
		}
	}
	return mgl64.Vec3{}, false
}

// CanContinueToUse is FindCoverGoal::canContinueToUse.
func (g *fleeSun) CanContinueToUse(m *mob.Mob) bool { return !m.Navigation().Done() }

// Start is FindCoverGoal::start.
func (g *fleeSun) Start(m *mob.Mob) { m.Navigation().MoveTo(m, g.wanted, g.speed) }

// Stop is FindCoverGoal::stop.
func (g *fleeSun) Stop(m *mob.Mob) { g.next = m.Ticks() + int64(g.cooldown) }

// AppendDebugInfo ...
func (g *fleeSun) AppendDebugInfo(b *strings.Builder) { b.WriteString("FleeSunGoal") }
