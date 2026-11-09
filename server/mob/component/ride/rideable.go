//go:build crossfly_full

package ride

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"slices"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// seat is one minecraft:rideable seat (BDS SeatDescription).
type seat struct {
	pos        mgl64.Vec3
	min, max   int
	rotateBy   core.Expr
	lock       float64
	cameraDist float64
}

// Rideable is minecraft:rideable (BDS RideableComponent, RideableDefinition):
// seats, who may get on (family_types, passenger_max_width), getting on by
// interaction, the rider events, and the riders kept with the mob when it is
// saved. The riding itself is in package mob (riding.go).
type Rideable struct {
	seatCount        int
	crouchingSkip    bool
	familyTypes      []string
	pullIn           bool
	riderCanInteract bool
	maxWidth         float64
	dismountMode     string
	interactText     string
	onEnter, onExit  string
	seats            []seat

	// saved are the UUIDs of the mob riders (players are not saved), kept
	// up to date for EncodeNBT; pending are riders to find after loading.
	saved        []uuid.UUID
	pending      []uuid.UUID
	pendingTicks int
}

func newRideable(key string, raw json.RawMessage) (mob.Component, error) {
	var v struct {
		SeatCount         *int            `json:"seat_count"`
		CrouchingSkip     *bool           `json:"crouching_skip_interact"`
		FamilyTypes       []string        `json:"family_types"`
		PullInEntities    bool            `json:"pull_in_entities"`
		RiderCanInteract  bool            `json:"rider_can_interact"`
		PassengerMaxWidth float64         `json:"passenger_max_width"`
		DismountMode      string          `json:"dismount_mode"`
		InteractText      string          `json:"interact_text"`
		OnRiderEnterEvent string          `json:"on_rider_enter_event"`
		OnRiderExitEvent  string          `json:"on_rider_exit_event"`
		Seats             json.RawMessage `json:"seats"`
	}
	if err := decode(raw, &v); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	r := &Rideable{
		seatCount:        1,
		crouchingSkip:    true,
		familyTypes:      v.FamilyTypes,
		pullIn:           v.PullInEntities,
		riderCanInteract: v.RiderCanInteract,
		maxWidth:         v.PassengerMaxWidth,
		dismountMode:     v.DismountMode,
		interactText:     v.InteractText,
		onEnter:          v.OnRiderEnterEvent,
		onExit:           v.OnRiderExitEvent,
	}
	if v.SeatCount != nil {
		r.seatCount = *v.SeatCount
	}
	if v.CrouchingSkip != nil {
		r.crouchingSkip = *v.CrouchingSkip
	}
	var rawSeats []json.RawMessage
	if b := bytes.TrimSpace(v.Seats); len(b) > 0 {
		if b[0] == '[' {
			if err := json.Unmarshal(b, &rawSeats); err != nil {
				return nil, fmt.Errorf("%v: seats: %w", key, err)
			}
		} else {
			rawSeats = []json.RawMessage{b}
		}
	}
	for _, rs := range rawSeats {
		var s struct {
			Position  []float64       `json:"position"`
			Min       int             `json:"min_rider_count"`
			Max       int             `json:"max_rider_count"`
			RotateBy  json.RawMessage `json:"rotate_rider_by"`
			Lock      *float64        `json:"lock_rider_rotation"`
			CameraRad float64         `json:"third_person_camera_radius"`
		}
		if err := json.Unmarshal(rs, &s); err != nil {
			return nil, fmt.Errorf("%v: seat: %w", key, err)
		}
		st := seat{min: s.Min, max: s.Max, lock: 181, cameraDist: s.CameraRad}
		if len(s.Position) == 3 {
			st.pos = mgl64.Vec3{s.Position[0], s.Position[1], s.Position[2]}
		}
		if s.Lock != nil {
			st.lock = *s.Lock
		}
		if len(s.RotateBy) > 0 {
			// Boats ask has_any_family about the rider, which the Molang
			// subset cannot: no rotation then (boats are Dragonfly's own).
			if e, err := core.ParseExpr(s.RotateBy); err == nil {
				st.rotateBy = e
			}
		}
		r.seats = append(r.seats, st)
	}
	return r, nil
}

// Attach ...
func (r *Rideable) Attach(*mob.Mob) {}

// Detach ...
func (r *Rideable) Detach(*mob.Mob) {}

// InteractLate makes getting on the last interaction tried (mob.LateInteractor).
func (r *Rideable) InteractLate() {}

// TickStage: riders are placed after the mob moved.
func (r *Rideable) TickStage() mob.Stage { return mob.StageBody }

// Tick puts the riders on their seats and finds the saved riders after
// loading.
func (r *Rideable) Tick(m *mob.Mob) {
	if len(r.pending) > 0 {
		r.findSaved(m)
	}
	m.PositionRiders()
}

// seatFor is SeatDescriptionUtility::getSeatDescriptionOfPassenger: the
// slot-th seat valid for n riders (a seat without counts always is).
func (r *Rideable) seatFor(n, slot int) (seat, bool) {
	k := 0
	for _, s := range r.seats {
		if (s.min == 0 && s.max == 0) || (n >= s.min && n <= s.max) {
			if k == slot {
				return s, true
			}
			k++
		}
	}
	return seat{}, false
}

// SeatPositions implements mob.Vehicle.
func (r *Rideable) SeatPositions(m *mob.Mob, n int) []mgl64.Vec3 {
	scale := m.Meta().Scale
	out := make([]mgl64.Vec3, r.seatCount)
	for slot := range out {
		if s, ok := r.seatFor(n, slot); ok {
			out[slot] = s.pos.Mul(scale)
		}
	}
	return out
}

// CanAddRider implements mob.Vehicle (RideableComponent::canAddPassenger):
// a free seat, a rider of one of family_types that fits passenger_max_width
// and does not ride already.
func (r *Rideable) CanAddRider(m *mob.Mob, rider world.Entity) bool {
	if rider.H() == m.H() || m.Dead() || m.RiderCount() >= r.seatCount || m.HasPassenger(rider.H()) {
		return false
	}
	if len(r.familyTypes) > 0 && !slices.ContainsFunc(r.familyTypes, func(f string) bool { return hasFamily(rider, f) }) {
		return false
	}
	if r.maxWidth > 0 {
		if w := rider.H().Type().BBox(rider).Width(); w >= r.maxWidth {
			return false
		}
	}
	if rm, ok := mob.Of(rider); ok && (rm.Riding() || rm.HasPassenger(m.H())) {
		return false
	}
	return true
}

// Controls implements mob.Vehicle: a player in the first seat steers a
// mob with input_ground_controlled, item_controllable or
// free_camera_controlled (BDS Actor::getControllingPassenger).
func (r *Rideable) Controls(m *mob.Mob, rider world.Entity) bool {
	if _, ok := rider.(*player.Player); !ok {
		return false
	}
	return m.HasComponent("minecraft:input_ground_controlled") || m.HasComponent("minecraft:item_controllable") ||
		m.HasComponent("minecraft:free_camera_controlled")
}

// AcceptsClientMove implements mob.VehicleClientMover: the client of the
// player steering the mob may place it (Bedrock predicts horses, Java
// drives every vehicle it controls).
func (r *Rideable) AcceptsClientMove(m *mob.Mob, rider world.Entity) bool {
	return r.Controls(m, rider)
}

// RiderAdded fires on_rider_enter_event and remembers mob riders.
func (r *Rideable) RiderAdded(m *mob.Mob, h *world.EntityHandle) {
	if e, ok := h.Entity(m.Tx()); ok {
		if _, isMob := mob.Of(e); isMob && !slices.Contains(r.saved, h.UUID()) {
			r.saved = append(r.saved, h.UUID())
		}
		if r.onEnter != "" {
			ctx := m.Ctx()
			ctx.Other = e
			m.Fire(r.onEnter, ctx)
		}
	}
}

// RiderRemoved fires on_rider_exit_event.
func (r *Rideable) RiderRemoved(m *mob.Mob, h *world.EntityHandle) {
	r.saved = slices.DeleteFunc(r.saved, func(id uuid.UUID) bool { return id == h.UUID() })
	if r.onExit != "" {
		ctx := m.Ctx()
		if e, ok := h.Entity(m.Tx()); ok {
			ctx.Other = e
		}
		m.Fire(r.onExit, ctx)
	}
}

// SeatRotation implements mob.VehicleSeatRotator (rotate_rider_by,
// lock_rider_rotation).
func (r *Rideable) SeatRotation(m *mob.Mob, slot int) (entity.SeatRotation, bool) {
	s, ok := r.seatFor(max(m.RiderCount(), 1), slot)
	if !ok || (s.rotateBy == nil && s.lock >= 181) {
		return entity.SeatRotation{}, false
	}
	rot := entity.SeatRotation{LockRotation: s.lock < 181, LockDegrees: float32(s.lock)}
	if s.rotateBy != nil {
		rot.RotateBy = float32(s.rotateBy.Eval(m))
	}
	return rot, true
}

// DismountPosition implements mob.VehicleDismounter: on_top_center puts
// the rider on top of the mob.
func (r *Rideable) DismountPosition(m *mob.Mob, rider world.Entity) (mgl64.Vec3, bool) {
	switch r.dismountMode {
	case "on_top_center":
		return m.Pos().Add(mgl64.Vec3{0, m.Meta().Height * m.Meta().Scale}), true
	case "look_direction":
		yaw := rider.Rotation().Yaw() * math.Pi / 180
		w := m.Meta().Width*m.Meta().Scale/2 + 0.5
		return m.Pos().Add(mgl64.Vec3{-math.Sin(yaw) * w, 0, math.Cos(yaw) * w}), true
	}
	return mgl64.Vec3{}, false
}

// Interact gets a player on the mob (RideableComponent::getInteraction): not
// while sneaking if crouching_skip_interact, only if it may ride.
func (r *Rideable) Interact(m *mob.Mob, user item.User, held item.Stack, _ mgl64.Vec3) (item.Stack, bool) {
	p, ok := user.(*player.Player)
	if !ok || (r.crouchingSkip && p.Sneaking()) || !r.CanAddRider(m, p) {
		return held, false
	}
	if core.IsBaby(m) {
		return held, false
	}
	rideable, ok := entity.RideableOf(m)
	if !ok {
		return held, false
	}
	slot, ok := rideable.NextFreeSeatIndex(m.Pos())
	if !ok {
		return held, false
	}
	p.MountEntity(m.Tx(), rideable, slot)
	return held, p.RidingEntityHandle() == m.H()
}

// OnDeath takes the riders off (BDS removes all passengers of a dead actor).
func (r *Rideable) OnDeath(m *mob.Mob, _ world.DamageSource) { m.EjectPassengers() }

// EncodeNBT saves the mob riders.
func (r *Rideable) EncodeNBT() map[string]any {
	ids := r.saved
	if len(r.pending) > 0 {
		ids = r.pending
	}
	if len(ids) == 0 {
		return nil
	}
	list := make([]any, len(ids))
	for i, id := range ids {
		list[i] = id.String()
	}
	return map[string]any{"Riders": list}
}

// DecodeNBT remembers the saved riders to find once they are loaded.
func (r *Rideable) DecodeNBT(_ *mob.Mob, d map[string]any) {
	list, _ := d["Riders"].([]any)
	for _, v := range list {
		if s, ok := v.(string); ok {
			if id, err := uuid.Parse(s); err == nil {
				r.pending = append(r.pending, id)
			}
		}
	}
	r.pendingTicks = 40
}

// findSaved puts saved riders loaded near the mob back on it.
func (r *Rideable) findSaved(m *mob.Mob) {
	if r.pendingTicks--; r.pendingTicks <= 0 {
		r.pending = nil
		return
	}
	for e := range m.Tx().EntitiesWithin(m.BBox().Grow(4)) {
		i := slices.Index(r.pending, e.H().UUID())
		if i < 0 {
			continue
		}
		if rm, ok := mob.Of(e); ok && rm.StartRiding(m) {
			r.pending = slices.Delete(r.pending, i, i+1)
		}
	}
}

// hasFamily reports whether e is of family f: a mob's type_family, "player"
// for players.
func hasFamily(e world.Entity, f string) bool {
	if m, ok := mob.Of(e); ok {
		return m.HasFamily(f)
	}
	if _, ok := e.(*player.Player); ok {
		return f == "player"
	}
	return false
}

func decode(raw json.RawMessage, v any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	return json.Unmarshal(raw, v)
}

// InteractText implements mob.InteractTexter: interact_text while the
// player may get on.
func (r *Rideable) InteractText(m *mob.Mob, user world.Entity) string {
	p, ok := user.(*player.Player)
	if !ok || (r.crouchingSkip && p.Sneaking()) || core.IsBaby(m) || !r.CanAddRider(m, p) {
		return ""
	}
	if r.interactText == "" {
		return "action.interact.mount"
	}
	return r.interactText
}
