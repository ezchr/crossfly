//go:build crossfly_full

package mob

import (
	"math"
	"slices"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// Riding (BDS ActorRiding, RideableComponent, VehicleServerPositionPassengerSystem).
//
// A mob rides through the entity.Rider/entity.Rideable model Dragonfly uses
// for players: the mob's behaviour gives entity.RiderOf / entity.RideableOf an
// adapter around the Ent, so players get on mobs with Player.MountEntity and
// the sessions show the links as for cushions. A mob is a Rideable while a
// component implementing Vehicle (minecraft:rideable) is attached; every mob
// can be a Rider. Riders are positioned on their seat in StageTravel instead
// of travelling; their AI keeps running.

// Vehicle is implemented by the component that makes a mob rideable
// (minecraft:rideable).
type Vehicle interface {
	Component
	// SeatPositions returns the seat of each rider slot for n riders,
	// relative to the mob (BDS seat position x scale, unrotated). Its length
	// is the number of slots (seat_count).
	SeatPositions(m *Mob, n int) []mgl64.Vec3
	// CanAddRider reports whether rider may get on now (family_types, seats
	// left, passenger_max_width).
	CanAddRider(m *Mob, rider world.Entity) bool
	// Controls reports whether rider, in the first slot, steers the mob.
	Controls(m *Mob, rider world.Entity) bool
}

// Optional hooks of a Vehicle component.
type (
	// VehicleRiderListener hears riders get on and off (on_rider_enter_event,
	// on_rider_exit_event).
	VehicleRiderListener interface {
		RiderAdded(m *Mob, rider *world.EntityHandle)
		RiderRemoved(m *Mob, rider *world.EntityHandle)
	}
	// VehicleSeatRotator turns and locks the riders of a seat
	// (rotate_rider_by, lock_rider_rotation).
	VehicleSeatRotator interface {
		SeatRotation(m *Mob, slot int) (entity.SeatRotation, bool)
	}
	// VehicleDismounter places riders getting off (dismount_mode).
	VehicleDismounter interface {
		DismountPosition(m *Mob, rider world.Entity) (mgl64.Vec3, bool)
	}
	// VehicleJump handles the controlling rider's jump (can_power_jump,
	// dash_action).
	VehicleJump interface {
		RiderJump(m *Mob, rider world.Entity, strength float64)
	}
	// VehicleClientMover reports whether the client of the controlling
	// rider may move the mob itself (a horse the Bedrock client predicts, a
	// vehicle a Java client drives with move_vehicle).
	VehicleClientMover interface {
		AcceptsClientMove(m *Mob, rider world.Entity) bool
	}
)

// RiderInput is the latest steering input of the controlling rider:
// Move[0] is strafe (left positive), Move[1] forward, both -1..1; Yaw and
// Pitch are the rider's rotation in degrees.
type RiderInput struct {
	Move       mgl64.Vec2
	Yaw, Pitch float64
	// Tick is the mob tick (Ticks) the input arrived.
	Tick int64
}

type rideKey struct{}

// rideState is a mob's riding state, kept in the scratch map.
type rideState struct {
	// vehicle is what the mob rides, slot its rider slot there.
	vehicle *world.EntityHandle
	slot    int
	// riders are the mob's own riders by slot (compact: slots 0..n-1).
	riders []entity.RiderSeat
	input  RiderInput
	// hasInput: input came from the current controller.
	hasInput bool
	// clientMoved is the tick a rider's client last placed the mob.
	clientMoved int64
	// jumpTicks counts the input ticks jump is held (RiderJumpHeld).
	jumpTicks int
}

func (s *state) ride() *rideState {
	if v, ok := s.scratch[rideKey{}].(*rideState); ok {
		return v
	}
	r := &rideState{slot: -1, clientMoved: -1}
	if s.scratch == nil {
		s.scratch = map[any]any{}
	}
	s.scratch[rideKey{}] = r
	return r
}

func (s *state) rideIfAny() *rideState {
	r, _ := s.scratch[rideKey{}].(*rideState)
	return r
}

// vehicleComp returns the attached Vehicle component, if any.
func (s *state) vehicleComp() Vehicle { return s.vehicle }

// mobOf opens the mob for the adapters (same as Of).
func (s *state) mobOf(e *entity.Ent) *Mob {
	m := &Mob{Ent: e, data: s.data, s: s}
	s.cur = m
	return m
}

// Rideable implements entity.RideableBehaviour: the mob is a Rideable while
// a Vehicle component is attached.
func (s *state) Rideable(e *entity.Ent) (entity.Rideable, bool) {
	if s.vehicleComp() == nil {
		return nil, false
	}
	return rideableEnt{Ent: e, s: s}, true
}

// ClientDrivenBy implements entity.ClientDrivenBehaviour: whether the mob is
// a Rideable the client of rider placed within the last tick
// (rideableEnt.ClientDriven), without making the Rideable.
func (s *state) ClientDrivenBy(e *entity.Ent, rider *world.EntityHandle) bool {
	r := s.rideIfAny()
	if r == nil || r.clientMoved < 0 || s.ticks-r.clientMoved > 1 || s.vehicle == nil {
		return false
	}
	return rideableEnt{Ent: e, s: s}.ClientDriven(rider)
}

// Rider implements entity.RiderBehaviour: every mob may ride.
func (s *state) Rider(e *entity.Ent) (entity.Rider, bool) {
	return riderEnt{Ent: e, s: s}, true
}

// ---- Mob API ----

// Vehicle returns the entity the mob rides.
func (m *Mob) Vehicle() (world.Entity, bool) {
	r := m.s.rideIfAny()
	if r == nil || r.vehicle == nil || m.Tx() == nil {
		return nil, false
	}
	return r.vehicle.Entity(m.Tx())
}

// VehicleHandle returns the handle of the entity the mob rides, or nil.
func (m *Mob) VehicleHandle() *world.EntityHandle {
	if r := m.s.rideIfAny(); r != nil {
		return r.vehicle
	}
	return nil
}

// Riding reports whether the mob rides something.
func (m *Mob) Riding() bool { return m.VehicleHandle() != nil }

// Passengers returns the mob's riders, in slot order.
func (m *Mob) Passengers() []world.Entity {
	r := m.s.rideIfAny()
	if r == nil || len(r.riders) == 0 || m.Tx() == nil {
		return nil
	}
	out := make([]world.Entity, 0, len(r.riders))
	for _, seat := range r.riders {
		if e, ok := seat.Handle.Entity(m.Tx()); ok {
			out = append(out, e)
		}
	}
	return out
}

// PassengerHandles returns the handles of the mob's riders, in slot order.
func (m *Mob) PassengerHandles() []*world.EntityHandle {
	r := m.s.rideIfAny()
	if r == nil {
		return nil
	}
	out := make([]*world.EntityHandle, len(r.riders))
	for i, seat := range r.riders {
		out[i] = seat.Handle
	}
	return out
}

// RiderCount is the number of riders (filter rider_count).
func (m *Mob) RiderCount() int {
	if r := m.s.rideIfAny(); r != nil {
		return len(r.riders)
	}
	return 0
}

// HasPassenger reports whether h rides the mob.
func (m *Mob) HasPassenger(h *world.EntityHandle) bool {
	r := m.s.rideIfAny()
	return r != nil && slices.ContainsFunc(r.riders, func(s entity.RiderSeat) bool { return s.Handle == h })
}

// ControllingPassenger returns the rider steering the mob (BDS
// Actor::getControllingPassenger): the rider in the first slot, if the
// Vehicle component says it controls the mob.
func (m *Mob) ControllingPassenger() (world.Entity, bool) {
	r := m.s.rideIfAny()
	v := m.s.vehicleComp()
	if r == nil || v == nil || len(r.riders) == 0 || r.riders[0].SeatIndex != 0 || m.Tx() == nil {
		return nil, false
	}
	e, ok := r.riders[0].Handle.Entity(m.Tx())
	if !ok || !v.Controls(m, e) {
		return nil, false
	}
	return e, true
}

// RiderInput returns the controlling rider's latest steering input, if the
// current controller sent any.
func (m *Mob) RiderInput() (RiderInput, bool) {
	r := m.s.rideIfAny()
	if r == nil || !r.hasInput {
		return RiderInput{}, false
	}
	if _, ok := m.ControllingPassenger(); !ok {
		return RiderInput{}, false
	}
	return r.input, true
}

// ClientMoved reports whether the controlling rider's client placed the mob
// this tick or the last one (the mob then does not travel by itself).
func (m *Mob) ClientMoved() bool {
	r := m.s.rideIfAny()
	return r != nil && r.clientMoved >= 0 && m.s.ticks-r.clientMoved <= 1
}

// SeatPositions returns the seat of every rider slot for the current number
// of riders, or nil if the mob is not rideable.
func (m *Mob) SeatPositions() []mgl64.Vec3 {
	v := m.s.vehicleComp()
	if v == nil {
		return nil
	}
	return v.SeatPositions(m, max(m.RiderCount(), 1))
}

// StartRiding puts the mob on v in its first free slot (BDS
// Actor::startRiding). It reports whether the mob got on.
func (m *Mob) StartRiding(v world.Entity) bool {
	if v == nil || v.H() == m.H() || m.Dead() {
		return false
	}
	rideable, ok := VehicleOf(v)
	if !ok {
		return false
	}
	if f, ok := rideable.(entity.RiderFilter); ok && !f.AcceptsRider(m) {
		return false
	}
	slot, ok := rideable.NextFreeSeatIndex(v.Position())
	if !ok {
		return false
	}
	return m.mount(rideable, slot)
}

func (m *Mob) mount(rideable entity.Rideable, slot int) bool {
	if m.Riding() {
		m.StopRiding()
	}
	if !rideable.AddRider(m.H(), slot) {
		return false
	}
	r := m.s.ride()
	r.vehicle, r.slot = rideable.H(), slot
	m.StopMovement()
	m.positionRider()
	m.showMount(rideable)
	return true
}

// showMount shows viewers the mob getting on rideable.
func (m *Mob) showMount(rideable entity.Rideable) {
	tx := m.Tx()
	driver := rideable.ControllingRider() == m.H()
	for _, v := range tx.Viewers(m.Position()) {
		v.ViewEntityMount(m, rideable, driver)
	}
	m.RefreshState()
}

// StopRiding takes the mob off what it rides (BDS Actor::stopRiding).
func (m *Mob) StopRiding() {
	r := m.s.rideIfAny()
	if r == nil || r.vehicle == nil {
		return
	}
	h := r.vehicle
	r.vehicle, r.slot = nil, -1
	tx := m.Tx()
	e, ok := h.Entity(tx)
	if !ok {
		m.RefreshState()
		return
	}
	rideable, ok := VehicleOf(e)
	if !ok {
		m.RefreshState()
		return
	}
	rideable.RemoveRider(m.H())
	for _, v := range tx.Viewers(m.Position()) {
		v.ViewEntityDismount(m, rideable)
	}
	if d, ok := rideable.(entity.DismountPositioner); ok {
		m.SetPosition(d.DismountPosition(m))
	} else {
		m.SetPosition(e.Position().Add(mgl64.Vec3{0, m.s.meta.Height * m.s.meta.Scale / 2}))
	}
	m.ResetFallDistance()
	m.RefreshState()
}

// EjectPassengers takes every rider off the mob (BDS Actor::removeAllPassengers).
func (m *Mob) EjectPassengers() {
	for _, h := range m.PassengerHandles() {
		m.RemovePassenger(h)
	}
}

// RemovePassenger takes the rider h off the mob.
func (m *Mob) RemovePassenger(h *world.EntityHandle) {
	tx := m.Tx()
	if e, ok := h.Entity(tx); ok {
		if rider, ok := entity.RiderOf(e); ok && rider.RidingEntityHandle() == m.H() {
			rider.DismountEntity(tx)
			if !m.HasPassenger(h) {
				return
			}
		}
	}
	rideableEnt{Ent: m.Ent, s: m.s}.RemoveRider(h)
}

// rideTick runs instead of travel for a riding mob or one placed by its
// rider's client. It reports whether travel must be skipped.
func (m *Mob) rideTick() bool {
	r := m.s.rideIfAny()
	if r == nil {
		return false
	}
	m.pruneRiders()
	if len(r.riders) > 0 && m.s.vehicleComp() == nil {
		// No rideable any more (RemovePassengersWithoutSeatSystem).
		tx := m.Tx()
		for _, seat := range slices.Clone(r.riders) {
			if e, ok := seat.Handle.Entity(tx); ok {
				for _, v := range tx.Viewers(m.Position()) {
					v.ViewEntityDismount(e, m)
				}
				if rider, ok := entity.RiderOf(e); ok && rider.RidingEntityHandle() == m.H() {
					rider.DismountEntity(tx)
				}
			}
		}
		r.riders = nil
	}
	if r.vehicle != nil {
		e, ok := r.vehicle.Entity(m.Tx())
		if !ok || !m.ridingOn(e) {
			r.vehicle, r.slot = nil, -1
			m.RefreshState()
			return false
		}
		if v, ok := VehicleOf(e); ok {
			if ej, ok := v.(RiderEjector); ok && ej.EjectsRider(m) {
				m.StopRiding()
				return false
			}
		}
		m.positionRider()
		return true
	}
	return m.ClientMoved()
}

// ridingOn reports whether the vehicle e still has the mob as a rider.
func (m *Mob) ridingOn(e world.Entity) bool {
	rideable, ok := VehicleOf(e)
	if !ok {
		return false
	}
	for _, seat := range rideable.Riders() {
		if seat.Handle == m.H() {
			m.s.ride().slot = seat.SeatIndex
			return true
		}
	}
	return false
}

// pruneRiders drops riders that are gone, dead or ride something else.
func (m *Mob) pruneRiders() {
	r := m.s.rideIfAny()
	if r == nil || len(r.riders) == 0 {
		return
	}
	tx := m.Tx()
	for _, seat := range slices.Clone(r.riders) {
		e, ok := seat.Handle.Entity(tx)
		gone := !ok
		if ok {
			// A player registers its vehicle right after AddRider returns,
			// so by the next tick every rider must name the mob.
			if rider, ok := entity.RiderOf(e); !ok || rider.RidingEntityHandle() != m.H() {
				gone = true
			}
			if l, ok := e.(interface{ Dead() bool }); ok && l.Dead() {
				gone = true
			}
		}
		if gone {
			rideableEnt{Ent: m.Ent, s: m.s}.RemoveRider(seat.Handle)
		}
	}
}

// positionRider puts the riding mob on its seat (BDS
// VehicleServerPositionPassengerSystem, PositionPassengerUtility): the seat
// rotated by the vehicle's yaw, on top of its feet.
func (m *Mob) positionRider() {
	r := m.s.rideIfAny()
	if r == nil || r.vehicle == nil {
		return
	}
	e, ok := r.vehicle.Entity(m.Tx())
	if !ok {
		return
	}
	rideable, ok := VehicleOf(e)
	if !ok {
		return
	}
	m.SetPosition(seatWorldPosition(e, rideable, r.slot))
	m.data.Vel = mgl64.Vec3{}
	m.s.physics.FallDistance = 0
	m.s.physics.OnGround = false
	if m.HasComponent("minecraft:rotation_locked_to_vehicle") {
		yaw := e.Rotation().Yaw()
		m.SetBodyYaw(yaw)
		if m.TargetHandle() == nil {
			m.SetYaw(yaw)
		}
	}
}

// seatWorldPosition is where the rider of slot sits on e: the seat rotated
// by e's yaw, on top of e's feet (PositionPassengerUtility::getNewPassengerPosition).
// A player's seats (shoulders) are relative to its eyes, as its Bedrock
// position is.
func seatWorldPosition(e world.Entity, rideable entity.Rideable, slot int) mgl64.Vec3 {
	var seat mgl64.Vec3
	if positions := rideable.SeatPositions(); slot >= 0 && slot < len(positions) {
		seat = positions[slot]
	}
	if e.H().Type() == player.Type {
		seat[1] += playerRiderOffset
	}
	sin, cos := math.Sincos(e.Rotation().Yaw() * math.Pi / 180)
	return e.Position().Add(mgl64.Vec3{seat[0]*cos - seat[2]*sin, seat[1], seat[2]*cos + seat[0]*sin})
}

// playerVehicles is the hook that makes players vehicles of mobs (parrots on
// shoulders), set by package mob/component/ride.
var playerVehicles func(p world.Entity) (entity.Rideable, bool)

// SetPlayerVehicles installs the hook that makes a player a vehicle of mobs
// (from init only).
func SetPlayerVehicles(f func(p world.Entity) (entity.Rideable, bool)) { playerVehicles = f }

// VehicleOf is entity.RideableOf, and for players the shoulder seats of
// SetPlayerVehicles.
func VehicleOf(e world.Entity) (entity.Rideable, bool) {
	if r, ok := entity.RideableOf(e); ok {
		return r, true
	}
	if playerVehicles != nil && e != nil && e.H().Type() == player.Type {
		return playerVehicles(e)
	}
	return nil, false
}

// RiderEjector may be implemented by a vehicle (a player's shoulders) that
// throws a rider off by itself; riding mobs ask it every tick.
type RiderEjector interface {
	EjectsRider(rider world.Entity) bool
}

// SeatWorldPosition returns where the rider of slot sits in the world.
func (m *Mob) SeatWorldPosition(slot int) mgl64.Vec3 {
	return seatWorldPosition(m, rideableEnt{Ent: m.Ent, s: m.s}, slot)
}

// PositionRiders puts the mob's riders on their seats after it moved
// (VehicleServerPositionPassengerSystem): mobs directly, players through
// Move, which shows them to the others (their own client seats them itself).
func (m *Mob) PositionRiders() {
	r := m.s.rideIfAny()
	if r == nil {
		return
	}
	tx := m.Tx()
	for _, seat := range slices.Clone(r.riders) {
		e, ok := seat.Handle.Entity(tx)
		if !ok {
			continue
		}
		if rm, ok := Of(e); ok {
			rm.positionRider()
			continue
		}
		if p, ok := e.(interface {
			Move(deltaPos mgl64.Vec3, deltaYaw, deltaPitch float64)
			ResetFallDistance()
		}); ok {
			if delta := m.SeatWorldPosition(seat.SeatIndex).Sub(mgl64.Vec3{0, playerRiderOffset}).Sub(e.Position()); delta.Len() > 0.01 {
				p.Move(delta, 0, 0)
			}
			p.ResetFallDistance()
		}
	}
}

// ---- entity.Rideable adapter ----

// rideableEnt is the entity.Rideable of a rideable mob.
type rideableEnt struct {
	*entity.Ent
	s *state
}

func (r rideableEnt) mob() *Mob { return r.s.mobOf(r.Ent) }

// SeatPositions ...
// SeatPositions returns the seats sent to the clients: a player's seat is
// raised by playerRiderOffset, the offset of a player rider Bedrock
// clients add (as entity.Cushion does); mobs sit on the seat itself.
func (r rideableEnt) SeatPositions() []mgl64.Vec3 {
	v := r.s.vehicleComp()
	if v == nil {
		return nil
	}
	st := r.s.ride()
	out := v.SeatPositions(r.mob(), max(len(st.riders), 1))
	for _, seat := range st.riders {
		if seat.SeatIndex < len(out) && seat.Handle.Type() == player.Type {
			out[seat.SeatIndex][1] += playerRiderOffset
		}
	}
	return out
}

// playerRiderOffset is the riding height of a player rider: BDS sends the
// seat offset as seat x scale plus the rider's riding height
// (SeatDescriptionUtility::getSeatRidingHeightOffset), the 1.62001 height
// offset of a player (Player::getHeightOffset) and 0 for mobs; a Bedrock
// player's position is its eyes.
const playerRiderOffset = 1.62001

// NextFreeSeatIndex returns the first free slot.
func (r rideableEnt) NextFreeSeatIndex(mgl64.Vec3) (int, bool) {
	v := r.s.vehicleComp()
	if v == nil {
		return 0, false
	}
	m := r.mob()
	n := len(v.SeatPositions(m, len(r.s.ride().riders)+1))
	for slot := 0; slot < n; slot++ {
		if !slices.ContainsFunc(r.s.ride().riders, func(s entity.RiderSeat) bool { return s.SeatIndex == slot }) {
			return slot, true
		}
	}
	return 0, false
}

// AcceptsRider implements entity.RiderFilter.
func (r rideableEnt) AcceptsRider(rider world.Entity) bool {
	v := r.s.vehicleComp()
	return v != nil && !r.s.dead() && v.CanAddRider(r.mob(), rider)
}

// ControllingRider ...
func (r rideableEnt) ControllingRider() *world.EntityHandle {
	if e, ok := r.mob().ControllingPassenger(); ok {
		return e.H()
	}
	return nil
}

// ControllingSeatIndex ...
func (r rideableEnt) ControllingSeatIndex() int {
	if r.ControllingRider() != nil {
		return 0
	}
	return -1
}

// Riders ...
func (r rideableEnt) Riders() []entity.RiderSeat { return slices.Clone(r.s.ride().riders) }

// AddRider ...
func (r rideableEnt) AddRider(h *world.EntityHandle, slot int) bool {
	st := r.s.ride()
	if h == nil || slot < 0 {
		return false
	}
	if i := slices.IndexFunc(st.riders, func(s entity.RiderSeat) bool { return s.Handle == h }); i >= 0 {
		if st.riders[i].SeatIndex == slot {
			return true
		}
		st.riders = slices.Delete(st.riders, i, i+1)
	}
	if slices.ContainsFunc(st.riders, func(s entity.RiderSeat) bool { return s.SeatIndex == slot }) {
		return false
	}
	v := r.s.vehicleComp()
	if v == nil {
		return false
	}
	m := r.mob()
	if slot >= len(v.SeatPositions(m, len(st.riders)+1)) {
		return false
	}
	before := r.ControllingRider()
	st.riders = append(st.riders, entity.RiderSeat{Handle: h, SeatIndex: slot})
	slices.SortFunc(st.riders, func(a, b entity.RiderSeat) int { return a.SeatIndex - b.SeatIndex })
	if r.ControllingRider() != before {
		st.hasInput = false
	}
	for _, c := range r.s.comps {
		if l, ok := c.comp.(VehicleRiderListener); ok {
			l.RiderAdded(m, h)
		}
	}
	m.RefreshState()
	return true
}

// RemoveRider removes h and moves the riders behind it up a slot, as BDS
// keeps its passengers in a vector.
func (r rideableEnt) RemoveRider(h *world.EntityHandle) {
	st := r.s.ride()
	i := slices.IndexFunc(st.riders, func(s entity.RiderSeat) bool { return s.Handle == h })
	if i < 0 {
		return
	}
	before := r.ControllingRider()
	st.riders = slices.Delete(st.riders, i, i+1)
	for j := range st.riders {
		st.riders[j].SeatIndex = j
	}
	if r.ControllingRider() != before || before == h {
		st.hasInput = false
	}
	m := r.mob()
	for _, c := range r.s.comps {
		if l, ok := c.comp.(VehicleRiderListener); ok {
			l.RiderRemoved(m, h)
		}
	}
	m.RefreshState()
}

// MoveInput keeps the controlling rider's input for the mob's next tick.
func (r rideableEnt) MoveInput(vector mgl64.Vec2, yaw, pitch float32) {
	st := r.s.ride()
	st.input = RiderInput{Move: vector, Yaw: float64(yaw), Pitch: float64(pitch), Tick: r.s.ticks}
	st.hasInput = true
}

// SeatRotation implements entity.SeatRotator.
func (r rideableEnt) SeatRotation(slot int) entity.SeatRotation {
	if v, ok := r.s.vehicleComp().(VehicleSeatRotator); ok {
		if rot, ok := v.SeatRotation(r.mob(), slot); ok {
			return rot
		}
	}
	return entity.SeatRotation{}
}

// DismountPosition implements entity.DismountPositioner.
func (r rideableEnt) DismountPosition(rider world.Entity) mgl64.Vec3 {
	if v, ok := r.s.vehicleComp().(VehicleDismounter); ok {
		if pos, ok := v.DismountPosition(r.mob(), rider); ok {
			return pos
		}
	}
	// BDS default: next to the vehicle, on the rider's side.
	m := r.mob()
	w := m.s.meta.Width * m.s.meta.Scale
	yaw := (m.Yaw() + 90) * math.Pi / 180
	return m.Position().Add(mgl64.Vec3{-math.Sin(yaw) * (w/2 + 0.5), 0, math.Cos(yaw) * (w/2 + 0.5)})
}

// ClientDriven implements entity.ClientDrivenVehicle: the client of rider
// steers the mob and placed it within the last tick.
func (r rideableEnt) ClientDriven(rider *world.EntityHandle) bool {
	m := r.mob()
	if !m.ClientMoved() {
		return false
	}
	c, ok := m.ControllingPassenger()
	return ok && c.H() == rider
}

// RiderJump implements entity.VehicleJumper.
func (r rideableEnt) RiderJump(rider world.Entity, strength float64) {
	m := r.mob()
	if c, ok := m.ControllingPassenger(); !ok || c.H() != rider.H() {
		return
	}
	for _, c := range r.s.comps {
		if v, ok := c.comp.(VehicleJump); ok {
			v.RiderJump(m, rider, strength)
			return
		}
	}
}

// RiderJumpHeld implements entity.VehicleJumpHolder: a Bedrock rider holds
// jump to charge the jump bar and lets go to jump. The strength is the Java
// horse jump bar (as Geyser computes it for Bedrock): ticks x 0.1 for the
// first 10 ticks, then 0.8 + 0.2/(ticks-9).
func (r rideableEnt) RiderJumpHeld(rider world.Entity, held bool) {
	st := r.s.ride()
	if held {
		st.jumpTicks++
		return
	}
	ticks := st.jumpTicks
	st.jumpTicks = 0
	if ticks == 0 {
		return
	}
	strength := float64(ticks) * 0.1
	if ticks >= 10 {
		strength = 0.8 + 2/float64(ticks-9)*0.1
	}
	r.RiderJump(rider, min(strength, 1))
}

// ClientVehicleMove implements entity.ClientVehicle: the controlling
// rider's client placed the mob. Moves further than 8 blocks are refused.
func (r rideableEnt) ClientVehicleMove(rider world.Entity, pos mgl64.Vec3, yaw, pitch float64) bool {
	m := r.mob()
	c, ok := m.ControllingPassenger()
	if !ok || c.H() != rider.H() {
		return false
	}
	v, ok := r.s.vehicleComp().(VehicleClientMover)
	if !ok || !v.AcceptsClientMove(m, rider) {
		return false
	}
	if pos.Sub(m.Pos()).Len() > 8 || math.IsNaN(pos.Len()) {
		return false
	}
	m.data.Vel = pos.Sub(m.Pos())
	m.SetPosition(pos)
	m.SetYaw(yaw)
	m.SetBodyYaw(yaw)
	m.SetHeadYaw(yaw)
	m.clientFall(m.data.Vel[1])
	r.s.ride().clientMoved = r.s.ticks
	return true
}

// ---- entity.Rider adapter ----

// riderEnt is the entity.Rider of a mob.
type riderEnt struct {
	*entity.Ent
	s *state
}

// RidingEntity ...
func (r riderEnt) RidingEntity(tx *world.Tx) (entity.Rideable, bool) {
	st := r.s.rideIfAny()
	if st == nil || st.vehicle == nil || tx == nil {
		return nil, false
	}
	e, ok := st.vehicle.Entity(tx)
	if !ok {
		return nil, false
	}
	return VehicleOf(e)
}

// SeatIndex ...
func (r riderEnt) SeatIndex() int {
	if st := r.s.rideIfAny(); st != nil && st.vehicle != nil {
		return st.slot
	}
	return -1
}

// MountEntity ...
func (r riderEnt) MountEntity(_ *world.Tx, rideable entity.Rideable, slot int) {
	m := r.s.mobOf(r.Ent)
	if f, ok := rideable.(entity.RiderFilter); ok && !f.AcceptsRider(m) {
		return
	}
	m.mount(rideable, slot)
}

// DismountEntity ...
func (r riderEnt) DismountEntity(*world.Tx) { r.s.mobOf(r.Ent).StopRiding() }

// RidingEntityHandle ...
func (r riderEnt) RidingEntityHandle() *world.EntityHandle {
	if st := r.s.rideIfAny(); st != nil {
		return st.vehicle
	}
	return nil
}

// RidingEntityController ...
func (r riderEnt) RidingEntityController() bool {
	rideable, ok := r.RidingEntity(r.Tx())
	return ok && rideable.ControllingRider() == r.H()
}

// SeatOffset ...
func (r riderEnt) SeatOffset() (mgl64.Vec3, bool) {
	rideable, ok := r.RidingEntity(r.Tx())
	if !ok {
		return mgl64.Vec3{}, false
	}
	slot := r.SeatIndex()
	positions := rideable.SeatPositions()
	if slot < 0 || slot >= len(positions) {
		return mgl64.Vec3{}, true
	}
	return positions[slot], true
}

func (s *state) dead() bool { return s.dying > 0 || s.health.Health() <= 0 }

// LateInteractor marks an Interactor that only runs after every other
// component declined the interaction: getting on (rideable), sitting down
// (sittable), opening the inventory. BDS tries these after feeding, taming,
// healing and the interact component.
type LateInteractor interface {
	Interactor
	InteractLate()
}

// VehicleBooster is implemented by the component that lets the controlling
// rider speed the mob up with an item (minecraft:boostable). It reports
// whether the boost was taken and the damage the item takes.
type VehicleBooster interface {
	Boost(m *Mob, rider world.Entity, held item.Stack) (damage int, ok bool)
}

// BoostWith lets entity.Ent.BoostWith reach the VehicleBooster component.
func (s *state) BoostWith(e *entity.Ent, rider world.Entity, held item.Stack) (int, bool) {
	m := s.mobOf(e)
	if c, ok := m.ControllingPassenger(); !ok || c.H() != rider.H() {
		return 0, false
	}
	for _, c := range s.comps {
		if b, ok := c.comp.(VehicleBooster); ok {
			return b.Boost(m, rider, held)
		}
	}
	return 0, false
}

// BedrockAttributes adds the horse jump strength for a mob with
// can_power_jump: the Bedrock client sizes the jump bar and predicts the
// jump with it (session.BedrockAttributesEntity).
func (s *state) BedrockAttributes() []protocol.AttributeValue {
	for _, c := range s.comps {
		if c.key == "minecraft:can_power_jump" {
			v := s.attrs.Value(AttrJumpStrength)
			return []protocol.AttributeValue{{Name: AttrJumpStrength, Value: float32(v), Max: math.MaxFloat32}}
		}
	}
	return nil
}

// InteractTexter is a component that names the button a player looking at
// the mob sees ("action.interact.mount", "action.interact.sit"), or "".
type InteractTexter interface {
	InteractText(m *Mob, user world.Entity) string
}

// InteractText lets entity.Ent.InteractText reach the components: the first
// non-empty text, LateInteractor components last, as Interact tries them.
func (s *state) InteractText(e *entity.Ent, user world.Entity) string {
	m := s.mobOf(e)
	if m.Dead() {
		return ""
	}
	for _, late := range [2]bool{false, true} {
		for _, c := range s.comps {
			t, ok := c.comp.(InteractTexter)
			if !ok {
				continue
			}
			if _, isLate := c.comp.(LateInteractor); isLate != late {
				continue
			}
			if text := t.InteractText(m, user); text != "" {
				return text
			}
		}
	}
	return ""
}

// JumpStrength is the horse jump strength of a mob with can_power_jump, 0
// otherwise (the Java session sends it as minecraft:jump_strength).
func (s *state) JumpStrength() float64 {
	for _, c := range s.comps {
		if c.key == "minecraft:can_power_jump" {
			return s.attrs.Value(AttrJumpStrength)
		}
	}
	return 0
}

// WindowProvider is implemented by the component holding the mob's
// container (minecraft:inventory): the window a player opens.
type WindowProvider interface {
	Window(m *Mob, viewer world.Entity, layout entity.WindowLayout) (entity.EntityWindow, bool)
}

// EntityWindow implements entity.EntityWindowBehaviour.
func (s *state) EntityWindow(e *entity.Ent, viewer world.Entity, layout entity.WindowLayout) (entity.EntityWindow, bool) {
	m := s.mobOf(e)
	if m.Dead() {
		return entity.EntityWindow{}, false
	}
	for _, c := range s.comps {
		if w, ok := c.comp.(WindowProvider); ok {
			return w.Window(m, viewer, layout)
		}
	}
	return entity.EntityWindow{}, false
}

// Health, MaxHealth and Speed let the sessions read a mob's attributes from
// its behaviour (they probe the entity and its behaviour): the AddActor
// attributes of a Bedrock client and the vehicle attributes of a Java one.
// A Bedrock client steering a horse moves it at minecraft:movement.
func (s *state) Health() float64    { return s.health.Health() }
func (s *state) MaxHealth() float64 { return s.health.MaxHealth() }
func (s *state) Speed() float64     { return s.attrs.Value(AttrMovement) }
