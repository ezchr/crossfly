//go:build crossfly_full

// Package control holds the per-mob controls of definition-driven mobs: the
// move control (minecraft:movement.*), jump control (minecraft:jump.*), look
// control and body control. They turn what goals and navigation want into
// rotation and into the movement input package mob/physics travels with.
//
// Sources: BDS 1.21.3 MoveControl, GenericMoveControl, JumpControl,
// JumpControlComponent, LookControl, BodyControl, mce::Math::clampRotate.
// Variants the dump has only as names (fly, hover, glide, skip, jump, sway)
// follow Java's FlyingMoveControl / SlimeMoveControl and are marked
// "source: java".
package control

import (
	"math"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// HeadRange is how far (degrees) the head turns away from the body
// (LookControl::tick, BodyControl::clientTick).
const HeadRange = 75.0

// idleTurn is how fast head and pitch return to rest without a look target.
const idleTurn = 10.0

// WrapDegrees maps a into [-180, 180) (mce::Math::wrapDegrees).
func WrapDegrees(a float64) float64 {
	a = math.Mod(a+180, 360)
	if a < 0 {
		a += 360
	}
	return a - 180
}

// ClampRotate turns from towards to by at most max degrees
// (mce::Math::clampRotate). The result is not wrapped.
func ClampRotate(from, to, max float64) float64 {
	d := WrapDegrees(to - from)
	return from + mgl64.Clamp(d, -max, max)
}

// LookControl turns the head (head yaw) and the pitch towards a point set
// for this tick, at most yawMax/pitchMax degrees a tick; without one it
// eases both back to rest at 10 degrees a tick. The head never ends more
// than 75 degrees from the body. A look target lasts one tick: goals set it
// every tick they want to look.
type LookControl struct {
	wanted           bool
	pos              mgl64.Vec3
	yawMax, pitchMax float64
}

// NewLookControl returns the look control every mob gets.
func NewLookControl() *LookControl { return &LookControl{} }

// SetLookAt looks at pos this tick.
func (l *LookControl) SetLookAt(pos mgl64.Vec3, yawMax, pitchMax float64) {
	l.wanted, l.pos, l.yawMax, l.pitchMax = true, pos, yawMax, pitchMax
}

// SetLookAtEntity looks at e's eyes this tick.
func (l *LookControl) SetLookAtEntity(e world.Entity, yawMax, pitchMax float64) {
	l.SetLookAt(EyePosition(e), yawMax, pitchMax)
}

// HasWanted reports whether a look target is set for this tick.
func (l *LookControl) HasWanted() bool { return l.wanted }

// Tick implements mob.LookControl.
func (l *LookControl) Tick(m *mob.Mob) {
	body, head := m.BodyYaw(), m.HeadYaw()
	if l.wanted {
		l.wanted = false
		d := l.pos.Sub(m.EyePosition())
		yaw := mgl64.RadToDeg(math.Atan2(d[2], d[0])) - 90
		pitch := -mgl64.RadToDeg(math.Atan2(d[1], math.Hypot(d[0], d[2])))
		m.SetPitch(ClampRotate(m.Pitch(), pitch, l.pitchMax))
		head = ClampRotate(head, yaw, l.yawMax)
	} else {
		m.SetPitch(ClampRotate(m.Pitch(), 0, idleTurn))
		head = ClampRotate(head, body, idleTurn)
	}
	switch d := WrapDegrees(head - body); {
	case d < -HeadRange:
		head = body - HeadRange
	case d > HeadRange:
		head = body + HeadRange
	}
	m.SetHeadYaw(head)
}

// EyePosition returns the eye position of any entity: a mob's or player's
// own, or 0.85 of the box height for others.
func EyePosition(e world.Entity) mgl64.Vec3 {
	switch v := e.(type) {
	case interface{ EyePosition() mgl64.Vec3 }:
		return v.EyePosition()
	case interface{ EyeHeight() float64 }:
		return e.Position().Add(mgl64.Vec3{0, v.EyeHeight()})
	}
	return e.Position().Add(mgl64.Vec3{0, e.H().Type().BBox(e).Height() * 0.85})
}

// BodyControl turns the body after the facing yaw while the mob moves
// (at most 25 degrees a tick) and after the head while it stands: the body
// stays within 75 degrees of the head, and once the head has been still for
// 10 ticks that allowance shrinks to nothing over the next 10.
type BodyControl struct {
	lastHead float64
	steady   int
}

// NewBodyControl returns the body control every mob gets.
func NewBodyControl() *BodyControl { return &BodyControl{} }

// bodyTurn is the body's turn rate while moving.
const bodyTurn = 25.0

// Tick implements mob.BodyControl.
func (b *BodyControl) Tick(m *mob.Mob) {
	v := m.Vel()
	head, body := m.HeadYaw(), m.BodyYaw()
	if v[0]*v[0]+v[2]*v[2] > 2.5e-7 {
		body = ClampRotate(body, m.Yaw(), bodyTurn)
		m.SetBodyYaw(body)
		// source: java BodyRotationControl.rotateHeadIfNecessary: the look
		// control clamped the head to the body before it turned.
		if d := WrapDegrees(head - body); math.Abs(d) > HeadRange {
			head = body + math.Copysign(HeadRange, d)
			m.SetHeadYaw(head)
		}
		b.lastHead, b.steady = head, 0
		return
	}
	allow := HeadRange
	if math.Abs(head-b.lastHead) <= 15 {
		b.steady++
		if b.steady > 10 {
			allow = math.Max(1-float64(b.steady-10)*0.1, 0) * HeadRange
		}
	} else {
		b.lastHead, b.steady = head, 0
	}
	d := mgl64.Clamp(WrapDegrees(head-body), -allow, allow)
	m.SetBodyYaw(head - d)
}
