package entity

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Rider is an entity riding another entity.
type Rider interface {
	world.Entity

	// RidingEntity returns the entity being ridden.
	RidingEntity(tx *world.Tx) (Rideable, bool)
	// SeatIndex returns the current seat, or -1 if not riding.
	SeatIndex() int
	// MountEntity puts the rider in a seat.
	MountEntity(tx *world.Tx, rideable Rideable, seatIndex int)
	// DismountEntity removes the rider from its seat.
	DismountEntity(tx *world.Tx)
	// RidingEntityHandle returns the handle of the entity being ridden.
	RidingEntityHandle() *world.EntityHandle
	// RidingEntityController reports whether this rider controls the rideable.
	RidingEntityController() bool
	// SeatOffset returns the rider's position relative to the rideable.
	SeatOffset() (mgl64.Vec3, bool)
}

// RiderSeat holds a rider and its seat number.
type RiderSeat struct {
	Handle    *world.EntityHandle
	SeatIndex int
}

// Rideable is an entity that can have one or more riders.
type Rideable interface {
	world.Entity

	// SeatPositions returns every seat position relative to the rideable.
	SeatPositions() []mgl64.Vec3
	// NextFreeSeatIndex finds a free seat for the given click position.
	NextFreeSeatIndex(clickPos mgl64.Vec3) (int, bool)
	// ControllingRider returns the rider controlling the rideable.
	ControllingRider() *world.EntityHandle
	// ControllingSeatIndex returns the controlling seat, or -1 if there is none.
	ControllingSeatIndex() int
	// Riders returns all occupied seats.
	Riders() []RiderSeat
	// AddRider puts a rider in a seat and reports whether it succeeded.
	AddRider(rider *world.EntityHandle, seatIndex int) bool
	// RemoveRider removes a rider.
	RemoveRider(rider *world.EntityHandle)
	// MoveInput handles movement from the controlling rider.
	MoveInput(vector mgl64.Vec2, yaw, pitch float32)
}

// RiderFilter may be implemented by a Rideable that refuses some riders.
type RiderFilter interface {
	AcceptsRider(rider world.Entity) bool
}

// DismountPositioner may be implemented by a Rideable that moves riders getting off to a specific position.
type DismountPositioner interface {
	DismountPosition(rider world.Entity) mgl64.Vec3
}

// SeatRotation describes how a seat turns and restricts the rotation of its rider.
type SeatRotation struct {
	// RotateBy is the number of degrees the rider is turned by relative to the rideable.
	RotateBy float32
	// LockRotation limits the rider to looking at most LockDegrees away from the seat direction.
	LockRotation bool
	LockDegrees  float32
}

// RiderLocker may be implemented by a Rideable whose riders cannot get off by themselves: only the
// server takes them off.
type RiderLocker interface {
	LocksRiders() bool
}

// LocksRiders reports whether the entity r rides keeps it from getting off by itself.
func LocksRiders(tx *world.Tx, r Rider) bool {
	h := r.RidingEntityHandle()
	if h == nil {
		return false
	}
	e, ok := h.Entity(tx)
	if !ok {
		return false
	}
	if l, ok := e.(RiderLocker); ok {
		return l.LocksRiders()
	}
	if rb, ok := RideableOf(e); ok {
		if l, ok := rb.(RiderLocker); ok {
			return l.LocksRiders()
		}
	}
	return false
}

// SeatRotator may be implemented by a Rideable with seats that turn their riders.
type SeatRotator interface {
	SeatRotation(seatIndex int) SeatRotation
}

// RiderHolder may be implemented by a Rideable whose riders are not moved by knock back, pistons or portals.
type RiderHolder interface {
	HoldsRiders() bool
}

// InteractTexter may be implemented by an entity that shows a player looking at it a button, such as to sit down.
// An empty string means no button is shown.
type InteractTexter interface {
	InteractText(user world.Entity) string
}

// RideableBehaviour may be implemented by the Behaviour of an Ent that can be ridden (a mob with
// minecraft:rideable). The Ent itself is not a Rideable: RideableOf asks the behaviour.
type RideableBehaviour interface {
	Rideable(e *Ent) (Rideable, bool)
}

// RiderBehaviour may be implemented by the Behaviour of an Ent that can ride another entity
// (any mob).
type RiderBehaviour interface {
	Rider(e *Ent) (Rider, bool)
}

// RideableOf returns e as a Rideable: e itself, or for an Ent the Rideable its behaviour provides.
// Use it instead of a type assertion wherever an entity looked up by handle may be ridden.
func RideableOf(e world.Entity) (Rideable, bool) {
	if r, ok := e.(Rideable); ok {
		return r, true
	}
	if ent, ok := e.(interface{ ent() *Ent }); ok {
		if rb, ok := ent.ent().Behaviour().(RideableBehaviour); ok {
			return rb.Rideable(ent.ent())
		}
	}
	return nil, false
}

// RiderOf returns e as a Rider: e itself, or for an Ent the Rider its behaviour provides.
func RiderOf(e world.Entity) (Rider, bool) {
	if r, ok := e.(Rider); ok {
		return r, true
	}
	if ent, ok := e.(interface{ ent() *Ent }); ok {
		if rb, ok := ent.ent().Behaviour().(RiderBehaviour); ok {
			return rb.Rider(ent.ent())
		}
	}
	return nil, false
}

// ClientVehicle may be implemented by a Rideable that takes its position from the client of the
// rider steering it (a horse the Bedrock client predicts, a vehicle a Java client drives with
// move_vehicle). It reports whether the position was taken.
type ClientVehicle interface {
	ClientVehicleMove(rider world.Entity, pos mgl64.Vec3, yaw, pitch float64) bool
}

// VehicleJumper may be implemented by a Rideable whose controlling rider can make it jump: the
// power jump of a horse, the dash of a camel. strength is 0..1.
type VehicleJumper interface {
	RiderJump(rider world.Entity, strength float64)
}

// VehicleJumpHolder may be implemented by a Rideable that works out the jump strength itself from
// how long its controlling rider holds jump (Bedrock clients send no strength for a power jump).
// It is told every input tick whether jump is held.
type VehicleJumpHolder interface {
	RiderJumpHeld(rider world.Entity, held bool)
}
