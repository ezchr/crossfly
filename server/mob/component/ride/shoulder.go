//go:build crossfly_full

package ride

import (
	"encoding/json"
	"slices"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/mob/data"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Players as vehicles: the minecraft:rideable of player.json gives every
// player two shoulder seats for tamed parrots (family parrot_tame), which a
// parrot's behavior.find_mount gets on. A parrot jumps off when the player
// falls, swims, flies or sleeps (java Player.aiStep removeEntitiesOnShoulder).
// Package mob asks for these seats through SetPlayerVehicles.

var (
	playerSeatsOnce sync.Once
	playerSeats     *Rideable

	// shoulders are the riders of each player, by the player's handle.
	shouldersMu sync.Mutex
	shoulders   = map[*world.EntityHandle][]entity.RiderSeat{}
)

// playerRideable is the rideable component of player.json.
func playerRideable() *Rideable {
	playerSeatsOnce.Do(func() {
		b, ok := data.Entity("player.json")
		if !ok {
			return
		}
		var v struct {
			Entity struct {
				Components map[string]json.RawMessage `json:"components"`
			} `json:"minecraft:entity"`
		}
		if json.Unmarshal(b, &v) != nil {
			return
		}
		if raw, ok := v.Entity.Components["minecraft:rideable"]; ok {
			if c, err := newRideable("minecraft:rideable", raw); err == nil {
				playerSeats = c.(*Rideable)
			}
		}
	})
	return playerSeats
}

// playerVehicle is the hook for mob.SetPlayerVehicles.
func playerVehicle(e world.Entity) (entity.Rideable, bool) {
	p, ok := e.(*player.Player)
	r := playerRideable()
	if !ok || r == nil {
		return nil, false
	}
	return shoulderVehicle{Player: p, r: r}, true
}

// shoulderVehicle is the entity.Rideable of a player's shoulders.
type shoulderVehicle struct {
	*player.Player
	r *Rideable
}

func (v shoulderVehicle) riders() []entity.RiderSeat {
	shouldersMu.Lock()
	defer shouldersMu.Unlock()
	return slices.Clone(shoulders[v.H()])
}

// SeatPositions are the seats of player.json, relative to the player's
// eyes (its Bedrock position).
func (v shoulderVehicle) SeatPositions() []mgl64.Vec3 {
	n := max(len(v.riders()), 1)
	out := make([]mgl64.Vec3, v.r.seatCount)
	for slot := range out {
		if s, ok := v.r.seatFor(n, slot); ok {
			out[slot] = s.pos
		}
	}
	return out
}

// NextFreeSeatIndex ...
func (v shoulderVehicle) NextFreeSeatIndex(mgl64.Vec3) (int, bool) {
	rs := v.riders()
	for slot := 0; slot < v.r.seatCount; slot++ {
		if !slices.ContainsFunc(rs, func(s entity.RiderSeat) bool { return s.SeatIndex == slot }) {
			return slot, true
		}
	}
	return 0, false
}

// AcceptsRider implements entity.RiderFilter: a mob of the family types
// (tamed parrots), on a free shoulder of a player that would not throw it off.
func (v shoulderVehicle) AcceptsRider(rider world.Entity) bool {
	m, ok := mob.Of(rider)
	if !ok || m.Riding() || core.IsBaby(m) || len(v.riders()) >= v.r.seatCount || v.Dead() {
		return false
	}
	if !slices.ContainsFunc(v.r.familyTypes, m.HasFamily) {
		return false
	}
	return !v.EjectsRider(rider)
}

// ControllingRider ...
func (v shoulderVehicle) ControllingRider() *world.EntityHandle { return nil }

// ControllingSeatIndex ...
func (v shoulderVehicle) ControllingSeatIndex() int { return -1 }

// Riders ...
func (v shoulderVehicle) Riders() []entity.RiderSeat { return v.riders() }

// AddRider ...
func (v shoulderVehicle) AddRider(h *world.EntityHandle, slot int) bool {
	shouldersMu.Lock()
	defer shouldersMu.Unlock()
	rs := shoulders[v.H()]
	if slot < 0 || slot >= v.r.seatCount || slices.ContainsFunc(rs, func(s entity.RiderSeat) bool { return s.SeatIndex == slot || s.Handle == h }) {
		return false
	}
	rs = append(rs, entity.RiderSeat{Handle: h, SeatIndex: slot})
	slices.SortFunc(rs, func(a, b entity.RiderSeat) int { return a.SeatIndex - b.SeatIndex })
	shoulders[v.H()] = rs
	return true
}

// RemoveRider ...
func (v shoulderVehicle) RemoveRider(h *world.EntityHandle) {
	shouldersMu.Lock()
	defer shouldersMu.Unlock()
	rs := slices.DeleteFunc(shoulders[v.H()], func(s entity.RiderSeat) bool { return s.Handle == h })
	if len(rs) == 0 {
		delete(shoulders, v.H())
		return
	}
	for i := range rs {
		rs[i].SeatIndex = i
	}
	shoulders[v.H()] = rs
}

// MoveInput does nothing: nobody steers a player.
func (v shoulderVehicle) MoveInput(mgl64.Vec2, float32, float32) {}

// EjectsRider implements mob.RiderEjector: off when the player falls more
// than half a block, is in water, flies or sleeps.
func (v shoulderVehicle) EjectsRider(world.Entity) bool {
	if v.Dead() || v.FallDistance() > 0.5 || v.Flying() || v.Swimming() || v.Gliding() {
		return true
	}
	if _, sleeping := v.Sleeping(); sleeping {
		return true
	}
	if l, ok := v.Tx().Liquid(cube.PosFromVec3(v.Position())); ok && l != nil {
		return true
	}
	return false
}

// DismountPosition implements entity.DismountPositioner: next to the player.
func (v shoulderVehicle) DismountPosition(world.Entity) mgl64.Vec3 {
	return v.Position().Add(mgl64.Vec3{0, 0.5, 0})
}
