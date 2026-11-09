//go:build crossfly_full

package control

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/physics"
	"github.com/go-gl/mathgl/mgl64"
)

// Kind is the minecraft:movement.* variant of a MoveControl.
type Kind uint8

const (
	// Basic is movement.basic (BDS MoveControl): walk on the ground.
	Basic Kind = iota
	// Generic is movement.generic (GenericMoveControl): walk, and swim in
	// three dimensions in water (drowned).
	Generic
	// Amphibious is movement.amphibious: walk on land, swim in water.
	Amphibious
	// Sway is movement.sway: swim only (fish, guardians).
	Sway
	// Fly is movement.fly (parrot), Hover movement.hover (bee, allay) and
	// Glide movement.glide (phantom): fly towards the wanted position.
	Fly
	Hover
	Glide
	// Skip is movement.skip (rabbit): walk in hops.
	Skip
	// Jump is movement.jump (slimes): jump towards the wanted position with
	// a delay between jumps.
	Jump
)

type operation uint8

const (
	opNone operation = iota
	opMoveTo
	opStrafe
)

// DefaultMaxTurn is the max_turn of movement.* (degrees a tick).
const DefaultMaxTurn = 30.0

// strafeSpeed is the speed multiplier while strafing (source: java
// MoveControl.strafe).
const strafeSpeed = 0.25

// MoveControl turns a wanted position (set by navigation) into the facing
// yaw and the movement input: the speed ramps up by 10% of the target a
// tick, slows down on the last block of a path and is capped at 0.25 going
// down; the facing turns at most max_turn degrees a tick (up to twice that
// while speeding up); a jump is asked for when the target is higher than
// the mob can step and close by.
type MoveControl struct {
	kind    Kind
	maxTurn float64

	op       operation
	wanted   mgl64.Vec3
	speedMod float64
	strafeF  float64
	strafeR  float64
	// speed is BDS Mob::mSpeed: the current speed after ramping.
	speed float64

	jumpDelayMin, jumpDelayMax float64
	jumpTimer                  int
}

// NewMoveControl returns a move control of the kind given; maxTurn <= 0
// uses DefaultMaxTurn.
func NewMoveControl(kind Kind, maxTurn float64) *MoveControl {
	if maxTurn <= 0 {
		maxTurn = DefaultMaxTurn
	}
	return &MoveControl{kind: kind, maxTurn: maxTurn, speedMod: 1, jumpDelayMin: 0.5, jumpDelayMax: 1.5}
}

// Kind returns the control's variant.
func (c *MoveControl) Kind() Kind { return c.kind }

// MaxTurn returns max_turn.
func (c *MoveControl) MaxTurn() float64 { return c.maxTurn }

// SetWantedPosition implements mob.MoveControl.
func (c *MoveControl) SetWantedPosition(pos mgl64.Vec3, speed float64) {
	c.op, c.wanted, c.speedMod = opMoveTo, pos, speed
}

// HasWanted implements mob.MoveControl.
func (c *MoveControl) HasWanted() bool { return c.op == opMoveTo }

// WantedPosition implements mob.MoveControl.
func (c *MoveControl) WantedPosition() mgl64.Vec3 { return c.wanted }

// Speed implements mob.MoveControl: the speed multiplier.
func (c *MoveControl) Speed() float64 { return c.speedMod }

// CurrentSpeed is the ramped movement speed (attribute x multiplier).
func (c *MoveControl) CurrentSpeed() float64 { return c.speed }

// Strafe implements mob.MoveControl: move forward/right (-1..1) relative
// to the facing at a quarter of the movement speed, for this tick.
func (c *MoveControl) Strafe(forward, right float64) {
	c.op, c.strafeF, c.strafeR, c.speedMod = opStrafe, forward, right, strafeSpeed
}

// Stop implements mob.MoveControl.
func (c *MoveControl) Stop() {
	c.op, c.speed = opNone, 0
}

// Tick implements mob.MoveControl.
func (c *MoveControl) Tick(m *mob.Mob) {
	in := m.Input()
	switch c.kind {
	case Fly, Hover, Glide:
		c.tickFly(m, in)
		return
	case Sway, Amphibious, Generic:
		if m.Physics().InWater {
			c.tickSwim(m, in)
			return
		}
		if c.kind == Sway {
			// Fish do not walk on land.
			in.Forward, in.Strafe, in.Vertical, in.Speed, c.speed = 0, 0, 0, 0, 0
			return
		}
	}
	if !physics.HasGravity(m) {
		c.tickNoGravity(m)
		return
	}
	c.tickWalk(m, in)
}

// diff is MoveControl::getDiffVector: from the bottom of the box.
func (c *MoveControl) diff(m *mob.Mob) mgl64.Vec3 {
	pos := m.Pos()
	return mgl64.Vec3{c.wanted[0] - pos[0], c.wanted[1] - m.BBox().Min()[1], c.wanted[2] - pos[2]}
}

// tickWalk is MoveControl::tick for a mob with gravity.
func (c *MoveControl) tickWalk(m *mob.Mob, in *mob.Input) {
	in.Forward, in.Strafe, in.Vertical = 0, 0, 0
	switch c.op {
	case opStrafe:
		speed := m.Speed() * c.speedMod
		c.speed, in.Speed = speed, speed
		in.Forward, in.Strafe = c.strafeF, c.strafeR
		c.op = opNone
		return
	case opMoveTo:
	default:
		c.speed, in.Speed = 0, 0
		return
	}
	d := c.diff(m)
	if d.LenSqr() < 0.1 && d[1] < 0.25 {
		c.op = opNone
		in.Speed = c.speed
		return
	}
	speed := m.Speed() * c.speedMod
	m.SetYaw(c.yRotation(m, d, speed))
	c.speed = c.moveSpeed(m, speed)

	if c.kind == Jump {
		c.hop(m, in)
		return
	}
	in.Forward, in.Speed = c.speed, c.speed

	step := physics.MaxAutoStep(m)
	if m.Physics().InWater {
		step = 0.009
	}
	if d[1] > step && d[0]*d[0]+d[2]*d[2] < math.Max(m.BBox().Width(), 1) {
		m.JumpControl().Jump()
	}
	if c.kind == Skip && m.OnGround() {
		// source: java Rabbit: a hopping mob jumps whenever it walks.
		m.JumpControl().Jump()
	}
}

// hop is the movement.jump step (source: java SlimeMoveControl): on the
// ground the mob waits jump_delay seconds between jumps and only moves while
// in the air.
func (c *MoveControl) hop(m *mob.Mob, in *mob.Input) {
	if !m.OnGround() {
		in.Forward, in.Speed = c.speed, c.speed
		return
	}
	if c.jumpTimer--; c.jumpTimer <= 0 {
		d := c.jumpDelayMin + m.Rand().Float64()*math.Max(c.jumpDelayMax-c.jumpDelayMin, 0)
		c.jumpTimer = max(int(d*20), 1)
		m.JumpControl().Jump()
		in.Forward, in.Speed = c.speed, c.speed
		return
	}
	in.Forward, in.Speed = 0, 0
}

// yRotation is MoveControl::calculateYRotation.
func (c *MoveControl) yRotation(m *mob.Mob, d mgl64.Vec3, speed float64) float64 {
	b := m.BBox()
	r := mgl64.Clamp((b.Width()+b.Length())*0.5*0.25, 0.05, 0.25)
	if d[0]*d[0]+d[2]*d[2] <= r*r {
		return m.Yaw()
	}
	target := mgl64.RadToDeg(math.Atan2(d[2], d[0])) - 90
	turn := c.maxTurn
	if c.speed > 0 && speed > 0 && c.speed < speed {
		if t := c.maxTurn / (c.speed / speed); t <= 2*c.maxTurn {
			turn = math.Max(c.maxTurn, t)
		} else {
			turn = 2 * c.maxTurn
		}
	}
	return ClampRotate(m.Yaw(), target, turn)
}

// moveSpeed is MoveControl::calculateMoveSpeed. BDS keeps the current speed
// when the mob follows no path; here the speed ramps up then too, so goals
// that drive the move control directly still move the mob.
func (c *MoveControl) moveSpeed(m *mob.Mob, speed float64) float64 {
	cur := c.speed
	from := m.Pos()
	if !m.OnGround() {
		b := m.BBox()
		from = b.Min().Add(b.Max()).Mul(0.5)
	}
	dist2 := from.Sub(c.wanted).LenSqr()
	v := math.Min(cur, speed)
	if p := m.Navigation().Path(); p != nil && p.Len() > 0 && p.End() == cube.PosFromVec3(c.wanted) && dist2 < 1 {
		v = math.Max(speed-(1-dist2)*speed, speed*0.5)
	} else if cur < speed {
		v = math.Min(speed*0.1+cur, speed)
	}
	if m.Pos()[1] > c.wanted[1] && v > 0.25 {
		limit := 0.25
		if attr := m.Speed(); cur > attr {
			limit = math.Max(cur-speed*0.1, attr)
		}
		v = math.Min(limit, 0.25)
	}
	return v
}

// tickNoGravity is the MoveControl::tick branch of mobs without gravity:
// the velocity is pushed towards the wanted position by 0.05 x multiplier a
// tick until the mob is within its own size of it.
func (c *MoveControl) tickNoGravity(m *mob.Mob) {
	if c.op != opMoveTo {
		c.speed = 0
		return
	}
	d := c.diff(m)
	dist := d.Len()
	b := m.BBox()
	if dist < (b.Width()+b.Height()+b.Length())/3 {
		c.op = opNone
		m.SetVelocity(m.Vel().Mul(0.5))
		return
	}
	v := m.Vel().Add(d.Mul(0.05 * c.speedMod / dist))
	m.SetVelocity(v)
	look := v
	if t, ok := m.Target(); ok {
		look = t.Position().Sub(m.Pos())
	}
	yaw := ClampRotate(m.Yaw(), -mgl64.RadToDeg(math.Atan2(look[0], look[2])), c.maxTurn)
	m.SetYaw(yaw)
	m.SetBodyYaw(yaw)
}

// tickSwim is the swimming branch of GenericMoveControl (also used for
// amphibious and sway): face the target, move along the line to it in three
// dimensions and cancel water gravity while doing so.
func (c *MoveControl) tickSwim(m *mob.Mob, in *mob.Input) {
	in.Forward, in.Strafe, in.Vertical = 0, 0, 0
	if c.op != opMoveTo {
		c.speed, in.Speed = 0, 0
		return
	}
	d := c.wanted.Sub(m.Pos())
	if d.Len() < 2.5e-7 {
		c.op = opNone
		c.speed, in.Speed = 0, 0
		return
	}
	yaw := ClampRotate(m.Yaw(), mgl64.RadToDeg(math.Atan2(d[2], d[0]))-90, c.maxTurn)
	m.SetYaw(yaw)
	m.SetBodyYaw(yaw)
	speed := m.Speed() * c.speedMod
	c.speed = c.moveSpeed(m, speed)
	a := math.Atan2(d[1], math.Hypot(d[0], d[2]))
	in.Speed = c.speed
	in.Forward = math.Cos(a) * c.speed
	in.Vertical = math.Sin(a) * c.speed
	if physics.AppliesLiquidGravity(m) {
		v := m.Vel()
		v[1] += 0.02
		m.SetVelocity(v)
	}
}

// tickFly is the fly/hover/glide step (source: java FlyingMoveControl): the
// mob flies (no gravity) while it has somewhere to go, turning its yaw and
// pitch at most max_turn a tick, at flying_speed in the air and movement
// speed on the ground. A hovering mob keeps flying when it stops.
func (c *MoveControl) tickFly(m *mob.Mob, in *mob.Input) {
	in.Forward, in.Strafe, in.Vertical = 0, 0, 0
	if c.op != opMoveTo {
		if c.kind != Hover {
			physics.SetFlying(m, false)
		}
		c.speed, in.Speed = 0, 0
		return
	}
	c.op = opNone
	physics.SetFlying(m, true)
	d := c.wanted.Sub(m.Pos())
	if d.LenSqr() < 2.5e-7 {
		c.speed, in.Speed = 0, 0
		return
	}
	m.SetYaw(ClampRotate(m.Yaw(), mgl64.RadToDeg(math.Atan2(d[2], d[0]))-90, math.Max(c.maxTurn, 90)))
	speed := c.speedMod * m.Attr(mob.AttrFlyingSpeed)
	if m.OnGround() {
		speed = c.speedMod * m.Speed()
	}
	c.speed, in.Speed, in.Forward = speed, speed, speed
	if horiz := math.Hypot(d[0], d[2]); math.Abs(d[1]) > 1e-5 || horiz > 1e-5 {
		m.SetPitch(ClampRotate(m.Pitch(), -mgl64.RadToDeg(math.Atan2(d[1], horiz)), c.maxTurn))
		if d[1] > 0 {
			in.Vertical = speed
		} else {
			in.Vertical = -speed
		}
	}
}

// JumpControl is jump.static / jump.dynamic (BDS JumpControl): a jump asked
// for during a tick is handed to travel once, through Input.Jump.
type JumpControl struct {
	power float64
	want  bool
}

// NewJumpControl returns a jump control with the jump power given (<= 0
// uses physics.DefaultJumpPower).
func NewJumpControl(power float64) *JumpControl {
	if power <= 0 {
		power = physics.DefaultJumpPower
	}
	return &JumpControl{power: power}
}

// Jump implements mob.JumpControl.
func (j *JumpControl) Jump() { j.want = true }

// Jumping implements mob.JumpControl.
func (j *JumpControl) Jumping() bool { return j.want }

// JumpPower is the jump velocity (package physics reads it).
func (j *JumpControl) JumpPower() float64 { return j.power }

// Tick implements mob.JumpControl.
func (j *JumpControl) Tick(m *mob.Mob) {
	m.Input().Jump = j.want
	j.want = false
}
