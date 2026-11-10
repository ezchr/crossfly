//go:build crossfly_full

package javamob

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob/javamob/path"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// LookControl is Java's LookControl: turns the head (and pitch) towards a point for two ticks after each
// SetLookAt.
type LookControl struct {
	yMaxRotSpeed, xMaxRotAngle float64
	cooldown                   int
	wanted                     mgl64.Vec3
	// NoResetXRot keeps the pitch between looks (resetXRotOnTick false).
	NoResetXRot bool
	// Custom, if set, replaces Tick (a mob's own look control: SmoothSwimmingLookControl, ...).
	Custom func(m *Mob, c *LookControl)
}

// SetLookAt looks at a point with the mob's head limits (setLookAt(x, y, z)).
func (c *LookControl) SetLookAt(m *Mob, p mgl64.Vec3) {
	_, xRot, speed := m.headLimits()
	c.SetLookAtWith(p, speed, xRot)
}

// SetLookAtWith looks at a point turning at most yMax degrees a tick, up to xMax degrees of pitch.
func (c *LookControl) SetLookAtWith(p mgl64.Vec3, yMax, xMax float64) {
	c.wanted, c.yMaxRotSpeed, c.xMaxRotAngle, c.cooldown = p, yMax, xMax, 2
}

// SetLookAtEntity looks at an entity's eyes (setLookAt(Entity, yMax, xMax)).
func (c *LookControl) SetLookAtEntity(e world.Entity, yMax, xMax float64) {
	c.SetLookAtWith(eyeOf(e), yMax, xMax)
}

// LookAtEntity looks at an entity with the mob's head limits (setLookAt(Entity)).
func (c *LookControl) LookAtEntity(m *Mob, e world.Entity) { c.SetLookAt(m, eyeOf(e)) }

// Looking reports whether the control looks at something (isLookingAtTarget).
func (c *LookControl) Looking() bool { return c.cooldown > 0 }

// Wanted is the point looked at.
func (c *LookControl) Wanted() mgl64.Vec3 { return c.wanted }

func (c *LookControl) tick(m *Mob) {
	if c.Custom != nil {
		c.Custom(m, c)
		return
	}
	c.DefaultTick(m)
}

// DefaultTick is LookControl.tick.
func (c *LookControl) DefaultTick(m *Mob) {
	if !c.NoResetXRot {
		m.SetXRot(0)
	}
	if c.cooldown > 0 {
		c.cooldown--
		if y, ok := c.yRotD(m); ok {
			m.SetHeadYRot(rotateTowards(m.HeadYRot(), y, c.yMaxRotSpeed))
		}
		if x, ok := c.xRotD(m); ok {
			m.SetXRot(rotateTowards(m.XRot(), x, c.xMaxRotAngle))
		}
	} else {
		m.SetHeadYRot(rotateTowards(m.HeadYRot(), m.BodyYRot(), 10))
	}
	if !m.s.nav.Done() {
		m.SetHeadYRot(rotateIfNecessary(m.HeadYRot(), m.BodyYRot(), m.MaxHeadYRot()))
	}
}

func (c *LookControl) xRotD(m *Mob) (float64, bool) {
	p := m.Position()
	xd, yd, zd := c.wanted[0]-p[0], c.wanted[1]-m.EyeY(), c.wanted[2]-p[2]
	sd := math.Sqrt(xd*xd + zd*zd)
	if !(math.Abs(yd) > 1e-5) && !(math.Abs(sd) > 1e-5) {
		return 0, false
	}
	return float64(float32(-(math.Atan2(yd, sd) * 180 / math.Pi))), true
}

func (c *LookControl) yRotD(m *Mob) (float64, bool) {
	p := m.Position()
	xd, zd := c.wanted[0]-p[0], c.wanted[2]-p[2]
	if !(math.Abs(zd) > 1e-5) && !(math.Abs(xd) > 1e-5) {
		return 0, false
	}
	return float64(float32(math.Atan2(zd, xd)*180/math.Pi) - 90), true
}

// eyeOf is the eye position of an entity.
func eyeOf(e world.Entity) mgl64.Vec3 {
	if jm, ok := Of(e); ok {
		return jm.EyePosition()
	}
	return entity.EyePosition(e)
}

// moveOp is MoveControl.Operation.
type moveOp uint8

const (
	opWait moveOp = iota
	opMoveTo
	opStrafe
	opJumping
)

// MoveControl is Java's MoveControl: walks towards a wanted position, or strafes.
type MoveControl struct {
	wanted        mgl64.Vec3
	speedModifier float64
	strafeFwd     float64
	strafeRight   float64
	op            moveOp
	// Custom, if set, replaces Tick (FlyingMoveControl, a slime's jumps, ...).
	Custom func(m *Mob, c *MoveControl)
}

// HasWanted reports whether the control moves to a position (hasWanted).
func (c *MoveControl) HasWanted() bool { return c.op == opMoveTo }

// SpeedModifier is getSpeedModifier.
func (c *MoveControl) SpeedModifier() float64 { return c.speedModifier }

// Wanted is the wanted position.
func (c *MoveControl) Wanted() mgl64.Vec3 { return c.wanted }

// SetWantedPosition is setWantedPosition.
func (c *MoveControl) SetWantedPosition(x, y, z, speed float64) {
	c.wanted, c.speedModifier = mgl64.Vec3{x, y, z}, speed
	if c.op != opJumping {
		c.op = opMoveTo
	}
}

// Strafe is strafe(forwards, right): for one tick, move relative to the facing at a quarter speed.
func (c *MoveControl) Strafe(fwd, right float64) {
	c.op, c.strafeFwd, c.strafeRight, c.speedModifier = opStrafe, fwd, right, 0.25
}

// SetWait is setWait.
func (c *MoveControl) SetWait() { c.op = opWait }

func (c *MoveControl) tick(m *Mob) {
	if c.Custom != nil {
		c.Custom(m, c)
		return
	}
	c.DefaultTick(m)
}

// DefaultTick is MoveControl.tick.
func (c *MoveControl) DefaultTick(m *Mob) {
	switch c.op {
	case opStrafe:
		speed := float32(m.Attr(AttrMovementSpeed))
		mod := float32(c.speedModifier) * speed
		xa, za := float32(c.strafeFwd), float32(c.strafeRight)
		dist := float32(math.Sqrt(float64(xa*xa + za*za)))
		if dist < 1 {
			dist = 1
		}
		dist = mod / dist
		xa *= dist
		za *= dist
		rad := float32(m.YRot()) * (math.Pi / 180)
		sin, cos := mthSin(rad), mthCos(rad)
		dx, dz := xa*cos-za*sin, za*cos+xa*sin
		if !c.walkable(m, float64(dx), float64(dz)) {
			c.strafeFwd, c.strafeRight = 1, 0
		}
		m.SetMoveSpeed(float64(mod))
		m.SetZza(c.strafeFwd)
		m.SetXxa(c.strafeRight)
		c.op = opWait
	case opMoveTo:
		c.op = opWait
		p := m.Position()
		xd, yd, zd := c.wanted[0]-p[0], c.wanted[1]-p[1], c.wanted[2]-p[2]
		if xd*xd+yd*yd+zd*zd < float64(float32(2.5000003e-7)) {
			m.SetZza(0)
			return
		}
		yRot := float64(float32(math.Atan2(zd, xd)*180/math.Pi) - 90)
		m.SetYRot(rotlerp(m.YRot(), yRot, 90))
		m.SetMoveSpeed(float64(float32(c.speedModifier * m.Attr(AttrMovementSpeed))))
		tx := m.Tx()
		pos := cube.PosFromVec3(p)
		b := tx.Block(pos)
		top, hasShape := 0.0, false
		for _, bb := range b.Model().BBox(pos, tx) {
			top, hasShape = math.Max(top, bb.Max()[1]), true
		}
		if yd > m.MaxUpStep() && xd*xd+zd*zd < math.Max(1, m.Width()) ||
			hasShape && p[1] < top+float64(pos[1]) && !path.IsDoorOrFence(b) {
			m.s.jump.Jump()
			c.op = opJumping
		}
	case opJumping:
		m.SetMoveSpeed(float64(float32(c.speedModifier * m.Attr(AttrMovementSpeed))))
		if m.OnGround() || m.InLiquid() {
			c.op = opWait
		}
	default:
		m.SetZza(0)
	}
}

// walkable is MoveControl.isWalkable.
func (c *MoveControl) walkable(m *Mob, dx, dz float64) bool {
	p := m.Position()
	pos := cube.PosFromVec3(mgl64.Vec3{p[0] + dx, math.Floor(p[1]), p[2] + dz})
	return m.s.nav.TypeAt(m, pos) == path.Walkable
}

// rotlerp is MoveControl.rotlerp: a turned towards b by at most max, kept within 0..360.
func rotlerp(a, b, max float64) float64 {
	d := wrapDegrees(b - a)
	d = math.Max(-max, math.Min(max, d))
	r := a + d
	if r < 0 {
		r += 360
	} else if r > 360 {
		r -= 360
	}
	return r
}

// JumpControl is Java's JumpControl: a jump asked for this tick.
type JumpControl struct{ jump bool }

// Jump asks for a jump.
func (c *JumpControl) Jump() { c.jump = true }

func (c *JumpControl) tick(m *Mob) {
	m.SetJumping(c.jump)
	c.jump = false
}

// BodyRotationControl is Java's BodyRotationControl: the body follows the facing while moving, and the head
// otherwise.
type BodyRotationControl struct {
	headStableTime int
	lastStableHead float64
}

func (c *BodyRotationControl) tick(m *Mob) {
	p, o := m.Position(), m.s.old
	xd, zd := p[0]-o[0], p[2]-o[2]
	if xd*xd+zd*zd > float64(float32(2.5000003e-7)) {
		m.SetBodyYRot(m.YRot())
		m.SetHeadYRot(rotateIfNecessary(m.HeadYRot(), m.BodyYRot(), m.MaxHeadYRot()))
		c.lastStableHead = m.HeadYRot()
		c.headStableTime = 0
		return
	}
	if len(m.Passengers()) > 0 {
		if _, ok := Of(m.Passengers()[0]); ok {
			return
		}
	}
	if math.Abs(m.HeadYRot()-c.lastStableHead) > 15 {
		c.headStableTime = 0
		c.lastStableHead = m.HeadYRot()
		m.SetBodyYRot(rotateIfNecessary(m.BodyYRot(), m.HeadYRot(), m.MaxHeadYRot()))
		return
	}
	c.headStableTime++
	if c.headStableTime > 10 {
		frac := math.Max(0, math.Min(1, float64(c.headStableTime-10)/10))
		m.SetBodyYRot(rotateIfNecessary(m.BodyYRot(), m.HeadYRot(), m.MaxHeadYRot()*(1-frac)))
	}
}
