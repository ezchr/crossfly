//go:build crossfly_full

package hostile

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

const (
	squidID     = "minecraft:squid"
	glowSquidID = "minecraft:glow_squid"
)

// Squid is the per-mob state of a squid or glow squid (Squid, GlowSquid).
// The tentacle and body angles are the client's; the server keeps only what
// moves the squid.
type Squid struct {
	glow bool
	// movementVector is Squid.movementVector: set by the goals, applied as
	// the velocity in the push part of each tentacle stroke.
	movementVector   mgl64.Vec3
	tentacleMovement float64
	tentacleSpeed    float64
	rotateSpeed      float64
	// xBodyRot/yBodyRot (degrees) aim the ink.
	xBodyRot float64
	// darkTicks is GlowSquid.DATA_DARK_TICKS_REMAINING: it stops glowing for
	// 100 ticks after being hurt.
	darkTicks int
}

// squidOf returns the squid state of m.
func squidOf(m *mob.Mob) *Squid {
	return stateOf(m, func() *Squid {
		return &Squid{glow: m.Identifier() == glowSquidID, tentacleSpeed: 1 / (nextFloat(m.Rand()) + 1) * 0.2}
	})
}

// SquidMaxHealth is Squid.createAttributes.
const SquidMaxHealth = 10

// hasMovementVector is Squid.hasMovementVector.
func (s *Squid) hasMovementVector() bool { return s.movementVector.LenSqr() > 1e-5 }

// DarkTicks is GlowSquid.getDarkTicksRemaining.
func (s *Squid) DarkTicks() int { return s.darkTicks }

// SquidAIStep is the server part of Squid.aiStep (after the base aiStep): a
// stroke every 2π/tentacleSpeed ticks; in water the squid takes its
// movement vector in the last quarter of a stroke and drifts (0.9 drag)
// otherwise, the body turning along the motion; out of water it falls
// straight down (levitation lifts it).
func SquidAIStep(m *mob.Mob) {
	s := squidOf(m)
	s.tentacleMovement += s.tentacleSpeed
	if s.tentacleMovement > 2*math.Pi {
		s.tentacleMovement -= 2 * math.Pi
		if nextInt(m.Rand(), 10) == 0 {
			s.tentacleSpeed = 1 / (nextFloat(m.Rand()) + 1) * 0.2
		}
		// Java broadcasts entity event 19 (SQUID_ANIM_SYNCH) here to restart
		// the client's stroke animation; it has no Bedrock counterpart and
		// the clients animate fine without it, so it is not sent.
	}
	if inWater(m) {
		if s.tentacleMovement < math.Pi {
			scale := s.tentacleMovement / math.Pi
			if scale > 0.75 {
				m.SetVelocity(s.movementVector)
				s.rotateSpeed = 1
			} else {
				s.rotateSpeed *= 0.8
			}
		} else {
			m.SetVelocity(m.Vel().Mul(0.9))
			s.rotateSpeed *= 0.99
		}
		v := m.Vel()
		body := m.BodyYaw() + (-math.Atan2(v[0], v[2])*180/math.Pi-m.BodyYaw())*0.1
		m.SetBodyYaw(body)
		m.SetYaw(body)
		s.xBodyRot += (-math.Atan2(horizontalDistance(v), v[1])*180/math.Pi - s.xBodyRot) * 0.1
		return
	}
	yd := m.Vel()[1]
	if lev, ok := m.Effect(effect.Levitation); ok {
		yd = 0.05 * float64(lev.Level())
	} else {
		yd -= 0.08 // getGravity
	}
	m.SetVelocity(mgl64.Vec3{0, yd * 0.98, 0}) // getAirDrag
	s.xBodyRot += (-90 - s.xBodyRot) * 0.02
}

// SquidTravel is Squid.travel: plain movement by the velocity (no input, no
// drag beyond aiStep's).
func SquidTravel(m *mob.Mob) { m.SetVelocity(Hooks.Move(m, m.Vel())) }

// SquidHurt is the end of Squid.hurtServer and GlowSquid.hurtServer: hurt
// by a mob (lastHurtByMob set), the squid squirts ink; a glow squid stops
// glowing for 5 seconds after any hurt.
func SquidHurt(m *mob.Mob, byMob bool, ink func(m *mob.Mob, glow bool)) {
	s := squidOf(m)
	if byMob {
		snd := "entity.squid.squirt"
		if s.glow {
			snd = "entity.glow_squid.squirt"
		}
		playSound(m, snd, 0.4, voicePitch(m.Rand()))
		if ink != nil {
			ink(m, s.glow)
		}
	}
	if s.glow {
		setDarkTicks(m, 100)
	}
}

// GlowSquidAIStep is the server part of GlowSquid.aiStep: the dark ticks
// count down.
func GlowSquidAIStep(m *mob.Mob) {
	if s := squidOf(m); s.darkTicks > 0 {
		setDarkTicks(m, s.darkTicks-1)
	}
}

// setDarkTicks sets DATA_DARK_TICKS_REMAINING. Both clients only change the
// glow when it starts or ends, so the client state is refreshed then.
func setDarkTicks(m *mob.Mob, v int) {
	s := squidOf(m)
	was := s.darkTicks > 0
	s.darkTicks = v
	if was != (v > 0) {
		m.RefreshState()
	}
}

// squidRandomMovementGoal is Squid.SquidRandomMovementGoal (priority 0, no
// flags): after 100 idle ticks the squid stops; otherwise now and then (1 in
// 25), out of water or without a vector it picks a new direction.
type squidRandomMovementGoal struct{}

func (squidRandomMovementGoal) Flags() mob.ControlFlags { return 0 }

func (squidRandomMovementGoal) CanUse(*mob.Mob) bool { return true }

func (squidRandomMovementGoal) Tick(m *mob.Mob) {
	s := squidOf(m)
	r := m.Rand()
	switch {
	case noActionTime(m) > 100:
		s.movementVector = mgl64.Vec3{}
	case nextInt(r, reducedTickDelay(50)) == 0 || !inWater(m) || !s.hasMovementVector():
		a := nextFloat(r) * 2 * math.Pi
		s.movementVector = mgl64.Vec3{math.Cos(a) * 0.2, -0.1 + nextFloat(r)*0.2, math.Sin(a) * 0.2}
	}
}

// squidFleeGoal is Squid.SquidFleeGoal (priority 1, no flags): in water and
// within 10 blocks of the mob that last hurt it, swim away from it, up to 3
// blocks a stroke (slower the further it is), staying level if the way is
// air.
type squidFleeGoal struct {
	fleeTicks int
}

func (g *squidFleeGoal) Flags() mob.ControlFlags { return 0 }

func (g *squidFleeGoal) RequiresUpdateEveryTick() bool { return true }

func (g *squidFleeGoal) CanUse(m *mob.Mob) bool {
	e, _, ok := m.LastHurtBy()
	return inWater(m) && ok && e != nil && distanceSqr(m, e) < 100
}

func (g *squidFleeGoal) Start(*mob.Mob) { g.fleeTicks = 0 }

func (g *squidFleeGoal) Tick(m *mob.Mob) {
	g.fleeTicks++
	e, _, ok := m.LastHurtBy()
	if !ok || e == nil {
		return
	}
	pos := m.Pos()
	flee := pos.Sub(e.Position())
	at := blockPos(pos.Add(flee))
	tx := m.Tx()
	_, air := tx.Block(at).(block.Air)
	liq, hasLiq := tx.Liquid(at)
	water := hasLiq && isWaterLiquid(liq)
	if water || air {
		length := flee.Len()
		if length > 0 {
			// Java normalises into a discarded value: the vector keeps its
			// length and is only scaled.
			speed := 3.0
			if length > 5 {
				speed -= (length - 5) / 5
			}
			if speed > 0 {
				flee = flee.Mul(speed)
			}
		}
		if air {
			flee[1] = 0
		}
		squidOf(m).movementVector = flee.Mul(1.0 / 20)
	}
}

// isWaterLiquid reports whether l is water.
func isWaterLiquid(l world.Liquid) bool {
	_, ok := l.(block.Water)
	return ok
}
