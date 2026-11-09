//go:build crossfly_full

package locomotion

import (
	"fmt"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	cloco "github.com/df-mc/dragonfly/server/mob/component/locomotion"
	"github.com/df-mc/dragonfly/server/mob/goal/core"
	"github.com/go-gl/mathgl/mgl64"
)

// randomSwim is minecraft:behavior.random_swim (BDS RandomSwimmingGoal, a
// RandomStrollGoal): now and then swim to a random spot in the water.
//
//	speed_multiplier 1, xz_dist 10 (>= 1), y_dist 7 (>= 1), interval 120,
//	avoid_surface true. Flags move|look.
//
// The spot is a RandomPos::getPos candidate whose height is re-rolled
// between the sea floor + 0.5 (at most y_dist below the mob, 20 below the
// surface) and 1.5 below the surface (at most y_dist above); up to 30 rolls
// while the water there is too shallow. Spots more than 50 degrees above or
// below the mob, or not in water (ceil of the spot with avoid_surface, floor
// without), are refused.
type randomSwim struct {
	mob.GoalBase
	speed        float64
	xz, y        int
	interval     int
	avoidSurface bool

	wanted mgl64.Vec3
	done   bool
}

func newRandomSwim(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Speed        *float64 `json:"speed_multiplier"`
		XZ           *int     `json:"xz_dist"`
		Y            *int     `json:"y_dist"`
		Interval     *int     `json:"interval"`
		AvoidSurface *bool    `json:"avoid_surface"`
	}{}
	if err := decodeGoal(spec, &j); err != nil {
		return nil, err
	}
	g := &randomSwim{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, speed: 1, xz: 10, y: 7, interval: 120, avoidSurface: true}
	if j.Speed != nil {
		g.speed = *j.Speed
	}
	if j.XZ != nil {
		g.xz = max(*j.XZ, 1)
	}
	if j.Y != nil {
		g.y = max(*j.Y, 1)
	}
	if j.Interval != nil {
		g.interval = *j.Interval
	}
	if j.AvoidSurface != nil {
		g.avoidSurface = *j.AvoidSurface
	}
	return g, nil
}

func (g *randomSwim) CanUse(m *mob.Mob) bool {
	if !canSwim(m) || !hasNavigation(m) || m.Riding() {
		return false
	}
	if g.interval > 0 && m.Rand().IntN(g.interval) != 0 {
		return false
	}
	return g.setWanted(m)
}

func (g *randomSwim) setWanted(m *mob.Mob) bool {
	if !m.Physics().InWater && !isOverWater(m) {
		return false
	}
	tx := m.Tx()
	mpos := m.Position()
	var pos mgl64.Vec3
	found := false
	for tries := 0; tries < 30; {
		pos, found = core.RandomPos(m, g.xz, g.y, 0, nil, 10)
		bp := cube.PosFromVec3(pos)
		top := float64(aboveTopSolidBlock(tx, bp[0], bp[2]))
		surface := top
		if int(top) < tx.Range()[1] {
			surface = float64(waterSurface(tx, bp[0], int(top), bp[2]))
		}
		hi := math.Min(float64(g.y)+mpos[1], surface-1.5)
		lo := math.Max(mpos[1]-float64(g.y), surface-20)
		if lo <= top {
			lo = top + 0.5
		}
		tries++
		pos[1] = m.Rand().Float64()*(math.Max(hi, lo)-lo) + lo
		if !(lo > surface-1.5) {
			break
		}
	}
	if !found {
		return false
	}
	d := pos.Sub(mpos)
	pitch := wrapDegrees(-mgl64.RadToDeg(math.Atan2(d[1], math.Hypot(d[0], d[2]))))
	if (pitch > 50 && pitch < 130) || (pitch < -50 && pitch > -130) {
		return false
	}
	check := cube.Pos{int(math.Floor(pos[0])), int(math.Floor(pos[1])), int(math.Floor(pos[2]))}
	if g.avoidSurface {
		check = cube.Pos{int(math.Ceil(pos[0])), int(math.Ceil(pos[1])), int(math.Ceil(pos[2]))}
	}
	if !waterAt(tx, check) {
		return false
	}
	g.wanted = pos
	return true
}

func (g *randomSwim) CanContinueToUse(m *mob.Mob) bool { return canSwim(m) && !g.done }

func (g *randomSwim) Start(m *mob.Mob) {
	if !m.Navigation().MoveTo(m, g.wanted.Add(mgl64.Vec3{0.5, 0, 0.5}), g.speed) {
		g.done = true
	}
}

func (g *randomSwim) Tick(m *mob.Mob) { g.done = m.Navigation().Done() }

func (g *randomSwim) Stop(m *mob.Mob) {
	m.Navigation().Stop(m)
	g.done = false
}

func (g *randomSwim) AppendDebugInfo(b *strings.Builder) {
	fmt.Fprintf(b, "RandomSwimming to %.1f %.1f %.1f", g.wanted[0], g.wanted[1], g.wanted[2])
}

// swimIdle is minecraft:behavior.swim_idle (BDS SwimIdleGoal): float in place
// for idle_time seconds (default 5) with a success_rate (0.1) chance a tick
// once the navigation is done. A flocking fish's heading is held while it
// idles. Flags move.
type swimIdle struct {
	mob.GoalBase
	idle  int
	rate  float64
	ticks int
}

func newSwimIdle(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Rate *float64 `json:"success_rate"`
		Idle *float64 `json:"idle_time"`
	}{}
	if err := decodeGoal(spec, &j); err != nil {
		return nil, err
	}
	g := &swimIdle{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)}, idle: 100, rate: 0.1}
	if j.Rate != nil {
		g.rate = *j.Rate
	}
	if j.Idle != nil {
		g.idle = int(*j.Idle * 20)
	}
	return g, nil
}

func (g *swimIdle) CanUse(m *mob.Mob) bool {
	return canSwim(m) && navDone(m) && m.Rand().Float64() < g.rate
}

func (g *swimIdle) CanContinueToUse(m *mob.Mob) bool { return canSwim(m) && g.ticks < g.idle }

func (g *swimIdle) Start(m *mob.Mob) {
	g.ticks = 0
	if f, ok := cloco.FlockOf(m); ok {
		f.HoldHeading(m)
	}
}

func (g *swimIdle) Tick(*mob.Mob) { g.ticks++ }

func (g *swimIdle) AppendDebugInfo(b *strings.Builder) {
	fmt.Fprintf(b, "SwimIdle %d/%d", g.ticks, g.idle)
}

// swimWander is minecraft:behavior.swim_wander (BDS SwimWanderGoal): swim
// straight ahead without a path, steering around blocks and the surface.
//
//	interval 0.01 (chance a tick), look_ahead 5, speed_multiplier 1,
//	wander_time 5 s. Flags move.
//
// Each tick it looks look_ahead blocks ahead and to both sides; when a ray
// hits a block it takes the free one of the 26 directions closest to its
// heading (or turns round), pushes away from solid blocks (and, in water,
// air) at its corners, sinks by 0.5 when there is no water above, and moves
// underwater_movement x speed_multiplier a tick that way, turning at most
// 12 degrees a tick.
type swimWander struct {
	mob.GoalBase
	interval, lookAhead, speed float64
	maxTicks, ticks            int
	wanted                     mgl64.Vec3
}

// wanderSideAngle is the angle of the two side rays. The constant of
// SwimWanderGoal::_setWantedPosition's glm::rotate calls was not recovered
// from the dump.
const wanderSideAngle = 45

func newSwimWander(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Interval  *float64 `json:"interval"`
		LookAhead *float64 `json:"look_ahead"`
		Speed     *float64 `json:"speed_multiplier"`
		Time      *float64 `json:"wander_time"`
	}{}
	if err := decodeGoal(spec, &j); err != nil {
		return nil, err
	}
	g := &swimWander{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)}, interval: 0.01, lookAhead: 5, speed: 1, maxTicks: 100}
	if j.Interval != nil {
		g.interval = *j.Interval
	}
	if j.LookAhead != nil {
		g.lookAhead = *j.LookAhead
	}
	if j.Speed != nil {
		g.speed = *j.Speed
	}
	if j.Time != nil {
		g.maxTicks = int(*j.Time * 20)
	}
	return g, nil
}

func (g *swimWander) CanUse(m *mob.Mob) bool {
	if !canSwim(m) || !hasNavigation(m) || m.Navigation().Path() != nil {
		return false
	}
	tx := m.Tx()
	p := cube.PosFromVec3(m.Position())
	water := false
	for _, f := range cube.Faces() {
		if waterAt(tx, p.Side(f)) {
			water = true
			break
		}
	}
	if !water || m.Rand().Float64() >= g.interval {
		return false
	}
	g.setWanted(m)
	return true
}

func (g *swimWander) CanContinueToUse(m *mob.Mob) bool {
	if g.ticks < g.maxTicks && navDone(m) && canSwim(m) {
		return true
	}
	g.ticks = 0
	return false
}

func (g *swimWander) Start(*mob.Mob) { g.ticks = 0 }

func (g *swimWander) setWanted(m *mob.Mob) {
	tx := m.Tx()
	p := m.Position()
	v := viewVector(m)
	l := g.lookAhead
	wanted := v
	if clip(tx, p, p.Add(v.Mul(l))) || clip(tx, p, p.Add(rotateY(v, wanderSideAngle).Mul(l-1.5))) || clip(tx, p, p.Add(rotateY(v, -wanderSideAngle).Mul(l-1.5))) {
		var best mgl64.Vec3
		clear, bestAngle := false, math.MaxFloat64
		for x := -1.0; x <= 1; x++ {
			for y := -1.0; y <= 1; y++ {
				for z := -1.0; z <= 1; z++ {
					if x == 0 && y == 0 && z == 0 {
						continue
					}
					d := mgl64.Vec3{x, y, z}.Normalize()
					if !clipWith(tx, p, p.Add(d.Mul(l)), true) {
						if a := math.Acos(mgl64.Clamp(d.Dot(v), -1, 1)); a < bestAngle {
							bestAngle, best, clear = a, d, true
						}
					} else if !clear {
						best = d
					}
				}
			}
		}
		if best == (mgl64.Vec3{}) {
			wanted = v.Mul(-1)
		} else {
			wanted = best.Mul(2)
		}
	}
	var rep mgl64.Vec3
	inWater := m.Physics().InWater
	for _, ox := range [2]float64{-0.5, 0.5} {
		for _, oy := range [2]float64{-0.5, 0.5} {
			for _, oz := range [2]float64{-0.5, 0.5} {
				q := p.Add(mgl64.Vec3{ox, oy, oz})
				bp := cube.PosFromVec3(q)
				if blocksMotion(tx, bp) || (inWater && airAt(tx, bp)) {
					d := normalize(p.Sub(q))
					if clip(tx, p, p.Sub(d)) {
						rep = rep.Add(d)
					}
				}
			}
		}
	}
	if rep != (mgl64.Vec3{}) {
		rep = normalize(rep).Mul(1.2)
	}
	g.wanted = wanted.Add(rep)
}

func (g *swimWander) Tick(m *mob.Mob) {
	g.ticks++
	g.setWanted(m)
	if !waterAt(m.Tx(), cube.PosFromVec3(m.Position().Add(mgl64.Vec3{0, 1, 0}))) {
		g.wanted[1] -= 0.5
	}
	vel := m.Vel()
	n := normalize(vel.Add(g.wanted.Sub(vel).Mul(0.22)))
	move := n.Mul(m.Attr(mob.AttrUnderwaterMovement) * g.speed)
	yaw := clampRotate(m.Yaw(), yawOf(n), 12)
	m.SetYaw(yaw)
	m.SetBodyYaw(yaw)
	// BDS moves the actor by move directly (Actor::move). Here the velocity
	// is set to it: travel moves the mob by it this tick.
	m.SetVelocity(move)
}

func (g *swimWander) AppendDebugInfo(b *strings.Builder) {
	fmt.Fprintf(b, "SwimWander %d/%d", g.ticks, g.maxTicks)
}
