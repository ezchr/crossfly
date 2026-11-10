//go:build crossfly_full

// Package goal holds Java Edition's common mob goals (net.minecraft.world.entity.ai.goal, 26.3) for package
// javamob's mobs. A mob package adds them in its Behaviour.RegisterGoals; goals of one mob only live in that mob's
// package.
package goal

import (
	"math"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Float is FloatGoal: jump (swim up) in water deeper than the jump threshold and in lava. Adding it lets the
// navigation path through water (setCanFloat).
type Float struct{ javamob.GoalBase }

// NewFloat returns a FloatGoal for m.
func NewFloat(m *javamob.Mob) *Float {
	m.Navigation().SetCanFloat(true)
	return &Float{GoalBase: javamob.GoalBase{Control: javamob.FlagJump}}
}

func (g *Float) CanUse(m *javamob.Mob) bool {
	return m.InWater() && m.FluidHeight(false) > m.FluidJumpThreshold() || m.InLava()
}
func (g *Float) RequiresUpdateEveryTick() bool { return true }
func (g *Float) Tick(m *javamob.Mob) {
	if m.Rand().Float32() < 0.8 {
		m.JumpControl().Jump()
	}
}

// LookAt is LookAtPlayerGoal: now and then look at the nearest entity that matches (a player by default) within
// Distance for 2-4 seconds.
type LookAt struct {
	javamob.GoalBase
	// Match picks the entities to look at; nil means players.
	Match       func(world.Entity) bool
	Distance    float64
	Probability float32
	// OnlyHorizontal keeps the head level.
	OnlyHorizontal bool

	lookAt   *world.EntityHandle
	lookTime int
}

// NewLookAtPlayer returns LookAtPlayerGoal(mob, Player.class, dist).
func NewLookAtPlayer(dist float64) *LookAt {
	return &LookAt{GoalBase: javamob.GoalBase{Control: javamob.FlagLook}, Distance: dist, Probability: 0.02}
}

func (g *LookAt) conditions(m *javamob.Mob) javamob.TargetingConditions {
	c := javamob.ForNonCombat(g.Distance)
	if g.Match == nil {
		c.Selector = func(e world.Entity, _ *world.Tx) bool {
			// EntitySelector.notRiding(mob).
			r, ok := e.(interface{ VehicleHandle() *world.EntityHandle })
			return !ok || r.VehicleHandle() != m.H()
		}
	}
	return c
}

func (g *LookAt) CanUse(m *javamob.Mob) bool {
	if m.Rand().Float32() >= g.Probability {
		return false
	}
	var e world.Entity
	var ok bool
	if g.Match == nil {
		var p *player.Player
		p, ok = m.NearestPlayer(g.conditions(m))
		e = p
	} else {
		box := m.BBox().GrowVec3(mgl64.Vec3{g.Distance, 3, g.Distance})
		e, ok = m.NearestEntity(box, g.Match, g.conditions(m))
	}
	if !ok {
		return false
	}
	g.lookAt = e.H()
	return true
}

func (g *LookAt) target(m *javamob.Mob) (world.Entity, bool) {
	if g.lookAt == nil {
		return nil, false
	}
	e, ok := g.lookAt.Entity(m.Tx())
	if !ok {
		return nil, false
	}
	if l, ok := e.(entity.Living); ok && l.Dead() {
		return nil, false
	}
	return e, true
}

func (g *LookAt) CanContinueToUse(m *javamob.Mob) bool {
	e, ok := g.target(m)
	if !ok {
		return false
	}
	if e.Position().Sub(m.Position()).LenSqr() > g.Distance*g.Distance {
		return false
	}
	return g.lookTime > 0
}
func (g *LookAt) Start(m *javamob.Mob) {
	g.lookTime = javamob.AdjustedTickDelay(g, 40+m.Rand().IntN(40))
}
func (g *LookAt) Stop(*javamob.Mob) { g.lookAt = nil }
func (g *LookAt) Tick(m *javamob.Mob) {
	e, ok := g.target(m)
	if !ok {
		return
	}
	eye := entity.EyePosition(e)
	y := eye[1]
	if g.OnlyHorizontal {
		y = m.EyeY()
	}
	m.LookControl().SetLookAt(m, mgl64.Vec3{e.Position()[0], y, e.Position()[2]})
	g.lookTime--
}

// RandomLookAround is RandomLookAroundGoal: now and then look in a random direction for 1-2 seconds.
type RandomLookAround struct {
	javamob.GoalBase
	rel      mgl64.Vec3
	lookTime int
}

// NewRandomLookAround returns a RandomLookAroundGoal.
func NewRandomLookAround() *RandomLookAround {
	return &RandomLookAround{GoalBase: javamob.GoalBase{Control: javamob.FlagMove | javamob.FlagLook}}
}

func (g *RandomLookAround) CanUse(m *javamob.Mob) bool         { return m.Rand().Float32() < 0.02 }
func (g *RandomLookAround) CanContinueToUse(*javamob.Mob) bool { return g.lookTime >= 0 }
func (g *RandomLookAround) RequiresUpdateEveryTick() bool      { return true }
func (g *RandomLookAround) Start(m *javamob.Mob) {
	a := math.Pi * 2 * m.Rand().Float64()
	g.rel = mgl64.Vec3{math.Cos(a), 0, math.Sin(a)}
	g.lookTime = 20 + m.Rand().IntN(20)
}
func (g *RandomLookAround) Tick(m *javamob.Mob) {
	g.lookTime--
	p := m.Position()
	m.LookControl().SetLookAt(m, mgl64.Vec3{p[0] + g.rel[0], m.EyeY(), p[2] + g.rel[2]})
}

// RandomStroll is RandomStrollGoal (and WaterAvoidingRandomStrollGoal with AvoidWater): every Interval ticks on
// average, walk to a random spot.
type RandomStroll struct {
	javamob.GoalBase
	Speed    float64
	Interval int
	// CheckNoActionTime skips strolling for a mob without action for 100 ticks (far from players).
	CheckNoActionTime bool
	// AvoidWater is WaterAvoidingRandomStrollGoal: land positions, and out of water first. Probability is the
	// chance of an ordinary random position instead (0.001).
	AvoidWater  bool
	Probability float32
	// Position, if set, replaces getPosition.
	Position func(m *javamob.Mob) (mgl64.Vec3, bool)

	wanted mgl64.Vec3
	force  bool
}

// NewRandomStroll returns RandomStrollGoal(mob, speed): every 120 ticks on average.
func NewRandomStroll(speed float64) *RandomStroll {
	return &RandomStroll{GoalBase: javamob.GoalBase{Control: javamob.FlagMove}, Speed: speed, Interval: 120, CheckNoActionTime: true}
}

// NewWaterAvoidingRandomStroll returns WaterAvoidingRandomStrollGoal(mob, speed, probability) (probability 0.001
// by default).
func NewWaterAvoidingRandomStroll(speed float64, probability float32) *RandomStroll {
	g := NewRandomStroll(speed)
	g.AvoidWater, g.Probability = true, probability
	return g
}

// Trigger makes the goal start at its next check (trigger).
func (g *RandomStroll) Trigger() { g.force = true }

func (g *RandomStroll) CanUse(m *javamob.Mob) bool {
	if _, ok := m.ControllingPassenger(); ok {
		return false
	}
	if !g.force {
		if g.CheckNoActionTime && m.NoActionTime() >= 100 {
			return false
		}
		if m.Rand().IntN(javamob.ReducedTickDelay(g.Interval)) != 0 {
			return false
		}
	}
	pos, ok := g.position(m)
	if !ok {
		return false
	}
	g.wanted, g.force = pos, false
	return true
}

func (g *RandomStroll) position(m *javamob.Mob) (mgl64.Vec3, bool) {
	if g.Position != nil {
		return g.Position(m)
	}
	if g.AvoidWater {
		if m.InWater() {
			if p, ok := m.LandRandomPos(15, 7); ok {
				return p, true
			}
			return m.DefaultRandomPos(10, 7)
		}
		if m.Rand().Float32() >= g.Probability {
			return m.LandRandomPos(10, 7)
		}
	}
	return m.DefaultRandomPos(10, 7)
}

func (g *RandomStroll) CanContinueToUse(m *javamob.Mob) bool {
	_, ridden := m.ControllingPassenger()
	return !m.Navigation().Done() && !ridden
}
func (g *RandomStroll) Start(m *javamob.Mob) {
	m.Navigation().MoveTo(m, g.wanted, 1, g.Speed)
}
func (g *RandomStroll) Stop(m *javamob.Mob) { m.Navigation().Stop() }
