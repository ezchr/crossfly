package session

import (
	"math"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// Riding mobs. A mob is not itself an entity.Rider or entity.Rideable: its
// behaviour provides them, so the code below asks entity.RiderOf and
// entity.RideableOf.

// riderOf is entity.RiderOf for the metadata code, which passes an entity or
// its behaviour.
func riderOf(e any) (entity.Rider, bool) {
	if we, ok := e.(world.Entity); ok {
		return entity.RiderOf(we)
	}
	return nil, false
}

// rideableOf is entity.RideableOf for the metadata code.
func rideableOf(e any) (entity.Rideable, bool) {
	if we, ok := e.(world.Entity); ok {
		return entity.RideableOf(we)
	}
	return nil, false
}

// vehicleInput passes the vehicle c steers what only mob vehicles use: the
// position of a vehicle the client predicts (horses: PlayerAuthInput then
// carries the vehicle's position and VehicleRotation) and whether jump is
// held, from which the vehicle works out a power jump (Bedrock clients send
// no jump strength).
func (s *Session) vehicleInput(pk *packet.PlayerAuthInput, c Controllable, rideable entity.Rideable) {
	predicted := pk.InputData.Load(packet.InputFlagClientPredictedVehicle)
	if id, ok := pk.ClientPredictedVehicle.Value(); predicted && ok && uint64(id) != s.entityRuntimeID(rideable) {
		// BDS only takes the position of the vehicle the client names.
		predicted = false
	}
	if cv, ok := rideable.(entity.ClientVehicle); ok && predicted {
		if rot, ok := pk.VehicleRotation.Value(); ok {
			pos := vec32To64(pk.Position)
			if cv.ClientVehicleMove(c, pos, float64(rot.Y()), float64(rot.X())) {
				// Position is the vehicle's (BDS takes the rider's from the
				// server). The rider sits on its seat: the seat offset holds the
				// 1.62 riding height of a player, which handleMovement takes off.
				if seat, ok := c.SeatOffset(); ok {
					sin, cos := math.Sincos(float64(rot.Y()) * math.Pi / 180)
					off := mgl64.Vec3{seat[0]*cos - seat[2]*sin, seat[1], seat[2]*cos + seat[0]*sin}
					pk.Position = vec64To32(pos.Add(off))
				}
			}
		}
	}
	if j, ok := rideable.(entity.VehicleJumpHolder); ok {
		j.RiderJumpHeld(c, pk.InputData.Load(packet.InputFlagJumping))
	}
}
