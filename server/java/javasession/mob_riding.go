package javasession

import (
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Riding mobs: a Java client drives the vehicle it controls itself (a saddled
// horse, a pig with a carrot on a stick) and sends move_vehicle; the power
// jump arrives as the player command start_riding_jump.

// vehicleMove passes the vehicle c steers the position its client moved it to.
func vehicleMove(tx *world.Tx, c session.Controllable, pos mgl64.Vec3, yaw, pitch float64) {
	rideable, ok := c.RidingEntity(tx)
	if !ok || rideable.ControllingRider() != c.H() {
		return
	}
	if isBoat(rideable) {
		yaw += 90 // Bedrock boats face 90 degrees off Java's (entityYaws)
	}
	if cv, ok := rideable.(entity.ClientVehicle); ok {
		cv.ClientVehicleMove(c, pos, yaw, pitch)
	}
}

// vehicleJump makes the vehicle c steers jump with strength 0..1.
func vehicleJump(tx *world.Tx, c session.Controllable, strength float64) {
	rideable, ok := c.RidingEntity(tx)
	if !ok || rideable.ControllingRider() != c.H() {
		return
	}
	if j, ok := rideable.(entity.VehicleJumper); ok {
		j.RiderJump(c, min(max(strength, 0), 1))
	}
}

// boatPaddle passes the paddles the driver of a boat works (paddle_boat).
func boatPaddle(tx *world.Tx, c session.Controllable, left, right bool) {
	rideable, ok := c.RidingEntity(tx)
	if !ok {
		return
	}
	if p, ok := rideable.(interface {
		PaddleInput(rider world.Entity, left, right bool)
	}); ok {
		p.PaddleInput(c, left, right)
	}
}
