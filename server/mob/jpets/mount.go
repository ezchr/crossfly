//go:build crossfly_full

package jpets

import (
	"encoding/json"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Mounts: a Java mob a player can ride (the horse family, llamas, camels,
// striders, happy ghasts, nautiluses) plugs into package mob's riding through
// the mob component crossfly:java_mount, which every such type lists
// (javamob.Type.Components). The component is a mob.Vehicle that asks the
// mob's behaviour (Mount). Riders on both editions steer the mount with
// their own client, as on a Java server the controlling player's client
// moves the vehicle (Entity.isControlledByLocalInstance); the server checks
// and shows the moves (mob.VehicleClientMover).

// MountComponent is the key of the riding component.
const MountComponent = "crossfly:java_mount"

func init() {
	mob.RegisterComponent(MountComponent, func(string, json.RawMessage) (mob.Component, error) {
		return mountComp{}, nil
	})
}

// Mount is implemented by the behaviour of a rideable Java mob.
type Mount interface {
	// Seats are the rider seats for n riders, relative to the mob (the
	// Bedrock seat positions, so both clients agree). Its length is the
	// number of riders the mob takes (canAddPassenger).
	Seats(m *javamob.Mob, n int) []mgl64.Vec3
	// CanAddPassenger is Entity.canAddPassenger plus the rider checks of
	// the type (who may get on at all).
	CanAddPassenger(m *javamob.Mob, rider world.Entity) bool
	// Controls is getControllingPassenger: whether rider, in the first
	// seat, steers (a saddled horse and a player).
	Controls(m *javamob.Mob, rider world.Entity) bool
}

// Optional Mount hooks.
type (
	// MountJumper is PlayerRideableJumping.onPlayerJump (strength is the
	// jump bar 0..1) or a camel's dash.
	MountJumper interface {
		RiderJump(m *javamob.Mob, rider world.Entity, strength float64)
	}
	// MountRiderListener hears riders get on and off (addPassenger,
	// removePassenger).
	MountRiderListener interface {
		RiderAdded(m *javamob.Mob, rider world.Entity)
		RiderRemoved(m *javamob.Mob, rider *world.EntityHandle)
	}
	// MountDismounter is getDismountLocationForPassenger.
	MountDismounter interface {
		DismountPosition(m *javamob.Mob, rider world.Entity) (mgl64.Vec3, bool)
	}
	// MountWindow gives the mount's inventory window (saddle, armour,
	// chest): HasCustomInventoryScreen.openCustomInventoryScreen.
	MountWindow interface {
		Window(m *javamob.Mob, viewer world.Entity, layout entity.WindowLayout) (entity.EntityWindow, bool)
	}
	// MountJumpStrength is the jump strength attribute the clients size
	// the horse jump bar with (0: the mount does not jump).
	MountJumpStrength interface {
		JumpStrength(m *javamob.Mob) float64
	}
)

// mountComp is the crossfly:java_mount component.
type mountComp struct{}

// Attach ...
func (mountComp) Attach(*mob.Mob) {}

// Detach ...
func (mountComp) Detach(*mob.Mob) {}

func mountOf(bm *mob.Mob) (*javamob.Mob, Mount, bool) {
	m, ok := javamob.Of(bm)
	if !ok {
		return nil, nil, false
	}
	b, ok := m.Behaviour().(Mount)
	return m, b, ok
}

// SeatPositions implements mob.Vehicle.
func (mountComp) SeatPositions(bm *mob.Mob, n int) []mgl64.Vec3 {
	m, b, ok := mountOf(bm)
	if !ok {
		return nil
	}
	return b.Seats(m, n)
}

// CanAddRider implements mob.Vehicle.
func (mountComp) CanAddRider(bm *mob.Mob, rider world.Entity) bool {
	m, b, ok := mountOf(bm)
	return ok && !m.Dead() && b.CanAddPassenger(m, rider)
}

// Controls implements mob.Vehicle.
func (mountComp) Controls(bm *mob.Mob, rider world.Entity) bool {
	m, b, ok := mountOf(bm)
	return ok && b.Controls(m, rider)
}

// AcceptsClientMove implements mob.VehicleClientMover: the steering rider's
// client moves the mount.
func (c mountComp) AcceptsClientMove(bm *mob.Mob, rider world.Entity) bool {
	return c.Controls(bm, rider)
}

// RiderJump implements mob.VehicleJump.
func (mountComp) RiderJump(bm *mob.Mob, rider world.Entity, strength float64) {
	m, b, ok := mountOf(bm)
	if !ok {
		return
	}
	if j, ok := b.(MountJumper); ok {
		j.RiderJump(m, rider, strength)
	}
}

// RiderAdded implements mob.VehicleRiderListener.
func (mountComp) RiderAdded(bm *mob.Mob, h *world.EntityHandle) {
	m, b, ok := mountOf(bm)
	if !ok {
		return
	}
	if l, ok := b.(MountRiderListener); ok {
		if e, ok := h.Entity(m.Tx()); ok {
			l.RiderAdded(m, e)
		}
	}
}

// RiderRemoved implements mob.VehicleRiderListener.
func (mountComp) RiderRemoved(bm *mob.Mob, h *world.EntityHandle) {
	m, b, ok := mountOf(bm)
	if !ok {
		return
	}
	if l, ok := b.(MountRiderListener); ok {
		l.RiderRemoved(m, h)
	}
}

// DismountPosition implements mob.VehicleDismounter.
func (mountComp) DismountPosition(bm *mob.Mob, rider world.Entity) (mgl64.Vec3, bool) {
	m, b, ok := mountOf(bm)
	if !ok {
		return mgl64.Vec3{}, false
	}
	if d, ok := b.(MountDismounter); ok {
		return d.DismountPosition(m, rider)
	}
	return mgl64.Vec3{}, false
}

// Window implements mob.WindowProvider.
func (mountComp) Window(bm *mob.Mob, viewer world.Entity, layout entity.WindowLayout) (entity.EntityWindow, bool) {
	m, b, ok := mountOf(bm)
	if !ok {
		return entity.EntityWindow{}, false
	}
	if w, ok := b.(MountWindow); ok {
		return w.Window(m, viewer, layout)
	}
	return entity.EntityWindow{}, false
}

// JumpStrength is read by package mob for the clients' jump bar.
func (mountComp) JumpStrength(bm *mob.Mob) float64 {
	m, b, ok := mountOf(bm)
	if !ok {
		return 0
	}
	if j, ok := b.(MountJumpStrength); ok {
		return j.JumpStrength(m)
	}
	return 0
}
