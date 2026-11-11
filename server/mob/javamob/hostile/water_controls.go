//go:build crossfly_full

package hostile

import (
	"math"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// The move controls of the water mobs, run as the base's MoveControl.Custom:
// the base keeps the wanted position (setWantedPosition from the navigation
// and the goals); these never go back to WAIT on their own, as in Java.

// fishMoveTick is AbstractFish.FishMoveControl.tick: buoyant with the eyes in
// water; swims straight at the wanted position, the speed easing in.
func fishMoveTick(m *javamob.Mob, c *javamob.MoveControl) {
	if m.UnderWater() {
		m.SetVelocity(m.Velocity().Add(mgl64.Vec3{0, 0.005}))
	}
	if !c.HasWanted() || m.Navigation().Done() {
		m.SetMoveSpeed(0)
		return
	}
	target := float64(float32(c.SpeedModifier() * m.Attr(javamob.AttrMovementSpeed)))
	m.SetMoveSpeed(float64(float32(lerp(0.125, m.MoveSpeed(), target))))
	d := c.Wanted().Sub(m.Position())
	if d[1] != 0 {
		dd := d.Len()
		m.SetVelocity(m.Velocity().Add(mgl64.Vec3{0, m.MoveSpeed() * (d[1] / dd) * 0.1}))
	}
	if d[0] != 0 || d[2] != 0 {
		yRotD := float64(float32(math.Atan2(d[2], d[0])*180/math.Pi) - 90)
		m.SetYRot(rotlerp(m.YRot(), yRotD, 90))
		m.SetBodyYRot(m.YRot())
	}
}

// guardianMoveTick is Guardian.GuardianMoveControl.tick: swim straight at the
// wanted position, speeding up smoothly, with a little wobble, and look a bit
// ahead; the spikes go in while moving.
func guardianMoveTick(m *javamob.Mob, c *javamob.MoveControl) {
	g := guardianOf(m.Mob)
	if !c.HasWanted() || m.Navigation().Done() {
		m.SetMoveSpeed(0)
		g.setMoving(m.Mob, false)
		return
	}
	pos := m.Position()
	delta := c.Wanted().Sub(pos)
	length := delta.Len()
	if length == 0 { // Java divides by it; a NaN velocity must never reach the world.
		m.SetMoveSpeed(0)
		g.setMoving(m.Mob, false)
		return
	}
	xd, yd, zd := delta[0]/length, delta[1]/length, delta[2]/length
	yRotD := float64(float32(math.Atan2(delta[2], delta[0])*180/math.Pi) - 90)
	m.SetYRot(rotlerp(m.YRot(), yRotD, 90))
	m.SetBodyYRot(m.YRot())
	target := float64(float32(c.SpeedModifier() * m.Attr(javamob.AttrMovementSpeed)))
	newSpeed := float64(float32(lerp(0.125, m.MoveSpeed(), target)))
	m.SetMoveSpeed(newSpeed)
	phase := float64(m.TickCount()) + float64(entityNetID(m))
	push := math.Sin(phase*0.5) * 0.05
	cos := math.Cos(m.YRot() * math.Pi / 180)
	sin := math.Sin(m.YRot() * math.Pi / 180)
	yPush := math.Sin(phase*0.75) * 0.05
	m.SetVelocity(m.Velocity().Add(mgl64.Vec3{push * cos, yPush*(sin+cos)*0.25 + newSpeed*yd*0.1, push * sin}))
	look := mgl64.Vec3{pos[0] + xd*2, m.EyeY() + yd/length, pos[2] + zd*2}
	lc := m.LookControl()
	old := look
	if lc.Looking() {
		old = lc.Wanted()
	}
	lc.SetLookAtWith(mgl64.Vec3{lerp(0.125, old[0], look[0]), lerp(0.125, old[1], look[1]), lerp(0.125, old[2], look[2])}, 10, 40)
	g.setMoving(m.Mob, true)
}

// Navigation and controls of the base for the port functions on *mob.Mob.

func navStop(m *mob.Mob) {
	if jm, ok := javamob.Of(m); ok {
		jm.Navigation().Stop()
		return
	}
	m.Navigation().Stop(m)
}

func navMoveToEntity(m *mob.Mob, e world.Entity, speed float64) bool {
	if jm, ok := javamob.Of(m); ok {
		return jm.Navigation().MoveToEntity(jm, e, speed)
	}
	return m.Navigation().MoveToEntity(m, e, speed)
}

func navMoveTo(m *mob.Mob, pos mgl64.Vec3, speed float64) bool {
	if jm, ok := javamob.Of(m); ok {
		return jm.Navigation().MoveTo(jm, pos, 1, speed)
	}
	return m.Navigation().MoveTo(m, pos, speed)
}

func navDone(m *mob.Mob) bool {
	if jm, ok := javamob.Of(m); ok {
		return jm.Navigation().Done()
	}
	return m.Navigation().Done()
}

func lookControlAt(m *mob.Mob, e world.Entity, yMax, xMax float64) {
	if jm, ok := javamob.Of(m); ok {
		jm.LookControl().SetLookAtEntity(e, yMax, xMax)
		return
	}
	m.LookControl().SetLookAtEntity(e, yMax, xMax)
}

// tickCount is Entity.tickCount.
func tickCount(m *mob.Mob) int64 {
	if jm, ok := javamob.Of(m); ok {
		return int64(jm.TickCount())
	}
	return m.Ticks()
}

// movementSpeedAttr is the movement speed attribute.
func movementSpeedAttr(m *mob.Mob) float64 {
	if jm, ok := javamob.Of(m); ok {
		return jm.Attr(javamob.AttrMovementSpeed)
	}
	return m.Attr(mob.AttrMovement)
}
