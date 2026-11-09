//go:build crossfly_full

package locomotion

import (
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// Squid movement (BDS Squid, SquidPreAiStepSystem): squids have no move
// input; the squid goals set a movement vector t (Squid tx/ty/tz) and every
// tick, after travel, the velocity eases towards it by 22% in water. Out of
// water the horizontal velocity is zeroed and ty reset. The body turns 10% of
// the way towards the velocity (the movement vector out of water) a tick.

// squidState is a squid's movement vector.
type squidState struct{ t mgl64.Vec3 }

type squidKey struct{}

// isSquid reports whether m is a squid (BDS SquidFlag: squid, glow squid).
func isSquid(m *mob.Mob) bool {
	id := m.Identifier()
	return id == "minecraft:squid" || id == "minecraft:glow_squid"
}

func squidOf(m *mob.Mob) *squidState {
	if v, ok := m.Data(squidKey{}); ok {
		return v.(*squidState)
	}
	s := &squidState{}
	m.SetData(squidKey{}, s)
	return s
}

// SquidMovement returns a squid's movement vector (Squid tx, ty, tz).
func SquidMovement(m *mob.Mob) mgl64.Vec3 { return squidOf(m).t }

// squidTravel is Squid::postAiStep's movement part, run after travel.
func squidTravel(m *mob.Mob) {
	if !isSquid(m) || m.Dead() {
		return
	}
	s := squidOf(m)
	v := m.Vel()
	if canSwim(m) {
		v = v.Add(s.t.Sub(v).Mul(0.22))
	} else {
		// BDS also applies (vy - 0.08) x 0.98 here; travel has already
		// applied the same gravity and drag this tick.
		v[0], v[2] = 0, 0
		s.t[1] = 0
	}
	m.SetVelocity(v)
	d := v
	if !m.Physics().InWater {
		d = s.t
	}
	h := math.Hypot(d[0], d[2])
	body := m.BodyYaw()
	if h > 1e-7 {
		yaw := body + 0.1*wrapDegrees(yawOf(d)-body)
		m.SetYaw(yaw)
		m.SetBodyYaw(yaw)
	}
	pitch := m.Pitch()
	if h > 1e-7 || math.Abs(d[1]) > 1e-7 {
		target := -mgl64.RadToDeg(math.Atan2(h, d[1]))
		m.SetPitch(pitch + (target-pitch)*0.1)
	}
}

// inkAction is the squid ink cloud actor event (BDS broadcastActorEvent 15).
type inkAction struct{}

func (inkAction) EntityAction()     {}
func (inkAction) ActorEvent() uint8 { return packet.ActorEventSquidFleeing }

// squidHurt is Squid::_hurt: a hit by an entity squirts ink.
func squidHurt(m *mob.Mob, dealt float64, src world.DamageSource) {
	if !isSquid(m) || dealt <= 0 || !entitySource(src) {
		return
	}
	core.ShowAction(m, inkAction{})
	if m.Identifier() == "minecraft:glow_squid" {
		m.PlaySound("glow_squid.ink_squirt")
	} else {
		m.PlaySound("squid.ink_squirt")
	}
}

// entitySource reports whether a damage source has an entity behind it
// (ActorDamageSource::isEntitySource).
func entitySource(src world.DamageSource) bool {
	switch core.CauseOf(src) {
	case "entity_attack", "projectile", "entity_explosion":
		return true
	}
	return false
}

// squidWaterHeights is FlockingUtility::getWaterHeights: the band a squid
// keeps to, from 5 to 1.5 blocks below the water surface, above the floor.
func squidWaterHeights(m *mob.Mob) (low, high float64) {
	tx := m.Tx()
	p := cube.PosFromVec3(m.Position())
	y := p[1]
	for y < tx.Range()[1] && waterAt(tx, cube.Pos{p[0], y, p[2]}) {
		y++
	}
	high, low = float64(y)-1.5, float64(y)-5
	floor := float64(aboveTopSolidBlock(tx, p[0], p[2]))
	if floor > high {
		high = floor + 0.5
	}
	if floor > low {
		low = floor + 0.5
	}
	return low, high
}

// randomSquidDirection is FlockingUtility::chooseRandomDirection.
func randomSquidDirection(m *mob.Mob) mgl64.Vec3 {
	r := m.Rand()
	a := r.Float64() * 2 * math.Pi
	return mgl64.Vec3{math.Cos(a) * 0.2, r.Float64()*0.2 - 0.1, math.Sin(a) * 0.2}
}

// steeringAngle and steeringDistance are the side-ray angle and length of
// FlockingUtility::checkForSteeringCollision as the squid goals call it;
// neither constant survived the decompile.
const (
	steeringAngle    = 45
	steeringDistance = 3
)

// steer is FlockingUtility::checkForSteeringCollision: when the way along dir
// is blocked it turns to a free side ray (or the longer blocked one) and
// reports true only when both sides are blocked.
func steer(m *mob.Mob, dir mgl64.Vec3, dist float64) (mgl64.Vec3, bool) {
	tx := m.Tx()
	p := m.Position()
	out := normalize(dir)
	if !clip(tx, p, p.Add(out.Mul(dist))) {
		return out, false
	}
	a, b := rotateY(out, steeringAngle), rotateY(out, -steeringAngle)
	ha, hb := clip(tx, p, p.Add(a.Mul(dist))), clip(tx, p, p.Add(b.Mul(dist)))
	switch {
	case !ha && !hb:
		if m.Rand().IntN(2) == 0 {
			return a, false
		}
		return b, false
	case !ha:
		return a, false
	case !hb:
		return b, false
	}
	return a, true
}

// squidGoal is the shared part of the five squid goals: none has fields,
// control flags default to none, and only squids may use them.
type squidGoal struct{ mob.GoalBase }

func newSquidGoal[T mob.Goal](mk func(squidGoal) T) mob.GoalFactory {
	return func(spec mob.GoalSpec) (mob.Goal, error) {
		return mk(squidGoal{GoalBase: mob.GoalBase{Control: spec.FlagsOr(0)}}), nil
	}
}

// squidDive is minecraft:behavior.squid_dive: out of water above air, aim
// down (0.025 a tick, a little sideways).
type squidDive struct{ squidGoal }

func (g *squidDive) CanUse(m *mob.Mob) bool {
	if !isSquid(m) || m.Physics().InWater {
		return false
	}
	return !blocksMotion(m.Tx(), cube.PosFromVec3(m.Position()).Side(cube.FaceDown))
}

func (g *squidDive) Start(m *mob.Mob) {
	r := m.Rand()
	d := normalize(mgl64.Vec3{r.Float64() - 0.5, -1, r.Float64() - 0.5})
	squidOf(m).t = d.Mul(0.025)
}

func (g *squidDive) AppendDebugInfo(b *strings.Builder) { b.WriteString("SquidDive") }

// squidFlee is minecraft:behavior.squid_flee: in water, swim away from an
// attacker closer than 10 blocks at 0.15 a tick, staying below the surface.
type squidFlee struct {
	squidGoal
	ticks int
}

func (g *squidFlee) attacker(m *mob.Mob) (world.Entity, bool) {
	e, _, ok := m.LastHurtBy()
	if !ok || !alive(m.Tx(), e) {
		return nil, false
	}
	return e, true
}

func (g *squidFlee) CanUse(m *mob.Mob) bool {
	if !isSquid(m) || !m.Physics().InWater {
		return false
	}
	e, ok := g.attacker(m)
	return ok && e.Position().Sub(m.Position()).LenSqr() < 100
}

func (g *squidFlee) Tick(m *mob.Mob) {
	g.ticks++
	e, ok := g.attacker(m)
	if !ok {
		return
	}
	d := m.Position().Sub(e.Position())
	_, high := squidWaterHeights(m)
	if dir, blocked := steer(m, d, steeringDistance); !blocked {
		l := dir.Len()
		if l > 0 {
			dir = dir.Mul(1 / l)
			s := 3.0
			if l > 5 {
				s = 3 - (l-5)*0.2
			}
			if s > 0 {
				dir = dir.Mul(s)
			}
		}
		if high < m.Position()[1] && dir[1] > 0 {
			dir[1] = 0
		}
		squidOf(m).t = dir.Mul(0.05)
	}
}

func (g *squidFlee) AppendDebugInfo(b *strings.Builder) { b.WriteString("SquidFlee") }

// squidIdle is minecraft:behavior.squid_idle: in water, every 200 ticks (or
// when it rises above the band) pick a random direction and a height inside
// the water band, at 0.025 a tick.
type squidIdle struct {
	squidGoal
	counter int
}

func (g *squidIdle) CanUse(m *mob.Mob) bool { return isSquid(m) && m.Physics().InWater }

func (g *squidIdle) Start(*mob.Mob) { g.counter = 200 }

func (g *squidIdle) Tick(m *mob.Mob) {
	low, high := squidWaterHeights(m)
	g.counter++
	s := squidOf(m)
	y := m.Position()[1]
	if (y > high && s.t[1] > 0) || g.counter > 200 {
		g.counter = 0
		r := m.Rand()
		dx := r.Float64() - 0.5
		_ = r.Float64()
		dz := r.Float64() - 0.5
		dy := (r.Float64()*(high-low) + low) - y
		v := mgl64.Vec3{dx, dy * 0.1, dz}
		if n := math.Sqrt(dy*dy*0.01 + dx*dx + dz*dz); n > 0 {
			v = v.Mul(0.5 / n)
		}
		s.t = v.Mul(0.05)
	}
}

func (g *squidIdle) AppendDebugInfo(b *strings.Builder) { b.WriteString("SquidIdle") }

// squidMoveAwayFromGround is minecraft:behavior.squid_move_away_from_ground:
// after bumping into a block, drift off in a random free direction.
type squidMoveAwayFromGround struct{ squidGoal }

func (g *squidMoveAwayFromGround) CanUse(m *mob.Mob) bool {
	p := m.Physics()
	return isSquid(m) && (p.HorizontalCollision || p.VerticalCollision)
}

func (g *squidMoveAwayFromGround) Start(m *mob.Mob) {
	d, _ := steer(m, randomSquidDirection(m), steeringDistance)
	squidOf(m).t = d.Mul(0.0025)
}

func (g *squidMoveAwayFromGround) AppendDebugInfo(b *strings.Builder) {
	b.WriteString("SquidMoveAwayFromGround")
}

// squidOutOfWater is minecraft:behavior.squid_out_of_water: on land, face a
// random horizontal direction (the velocity is zeroed out of water).
type squidOutOfWater struct{ squidGoal }

func (g *squidOutOfWater) CanUse(m *mob.Mob) bool { return isSquid(m) && !m.Physics().InWater }

func (g *squidOutOfWater) Start(m *mob.Mob) {
	d := randomSquidDirection(m)
	squidOf(m).t = mgl64.Vec3{d[0], 0, d[2]}
}

func (g *squidOutOfWater) AppendDebugInfo(b *strings.Builder) { b.WriteString("SquidOutOfWater") }

var _ = effect.Levitation
