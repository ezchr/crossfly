//go:build crossfly_full

package ride

import (
	"encoding/json"
	"testing"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/data"
	"github.com/go-gl/mathgl/mgl64"
)

func build[T any](t *testing.T, f mob.ComponentFactory, raw string) T {
	t.Helper()
	c, err := f("test", json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	v, ok := c.(T)
	if !ok {
		t.Fatalf("factory built %T", c)
	}
	return v
}

// BDS RideableDefinition defaults: one seat, crouching_skip_interact on,
// lock_rider_rotation 181 (none), seats as an object or a list.
func TestRideableDefaults(t *testing.T) {
	r := build[*Rideable](t, newRideable, `{}`)
	if r.seatCount != 1 || !r.crouchingSkip || r.maxWidth != 0 || len(r.seats) != 0 {
		t.Fatalf("defaults %+v", r)
	}
	r = build[*Rideable](t, newRideable, `{"seat_count":1,"seats":{"position":[0,1.1,-0.2]}}`)
	if len(r.seats) != 1 || r.seats[0].pos != (mgl64.Vec3{0, 1.1, -0.2}) || r.seats[0].lock != 181 {
		t.Fatalf("object seat %+v", r.seats)
	}
}

// SeatDescriptionUtility::getSeatDescriptionOfPassenger: the slot-th seat
// whose min..max rider count holds the current count (a seat without counts
// always does), as for a boat's one- and two-rider seats.
func TestSeatForRiderCount(t *testing.T) {
	r := build[*Rideable](t, newRideable, `{"seat_count":2,"seats":[
		{"position":[0,0,0],"min_rider_count":0,"max_rider_count":1},
		{"position":[0.2,0,0],"min_rider_count":2,"max_rider_count":2},
		{"position":[-0.6,0,0],"min_rider_count":2,"max_rider_count":2}]}`)
	if s, ok := r.seatFor(1, 0); !ok || s.pos[0] != 0 {
		t.Fatalf("one rider: %v", s.pos)
	}
	if s, ok := r.seatFor(2, 0); !ok || s.pos[0] != 0.2 {
		t.Fatalf("two riders, first: %v", s.pos)
	}
	if s, ok := r.seatFor(2, 1); !ok || s.pos[0] != -0.6 {
		t.Fatalf("two riders, second: %v", s.pos)
	}
	if _, ok := r.seatFor(1, 1); ok {
		t.Fatal("second slot valid with one rider")
	}
}

// Defaults of the steering components (BDS DashDefinition,
// BoostableDefinition, free_camera_controlled).
func TestControlDefaults(t *testing.T) {
	d := build[*Dash](t, newDash, `{}`)
	if d.cooldown != 20 || d.horizontal != 1 || d.vertical != 1 || d.underwater || d.passenger {
		t.Fatalf("dash defaults %+v", d)
	}
	d = build[*Dash](t, newDash, `{"cooldown_time":2.75,"horizontal_momentum":20,"vertical_momentum":0.6}`)
	if d.cooldown != 55 {
		t.Fatalf("camel dash cooldown %d ticks, want 55", d.cooldown)
	}
	b := build[*Boostable](t, newBoostable, `{}`)
	if b.duration != 60 || b.multiplier != 1 {
		t.Fatalf("boost defaults %+v", b)
	}
	b = build[*Boostable](t, newBoostable, `{"boost_items":[{"damage":2,"item":"carrotOnAStick","replace_item":"fishing_rod"}],"duration":3.0,"speed_multiplier":1.35}`)
	if b.duration != 60 || len(b.items) != 1 || b.items[0].damage != 2 {
		t.Fatalf("pig boost %+v", b)
	}
	f := build[*FreeCamera](t, newFreeCamera, `{}`)
	if f.back != 0.5 || f.strafe != 1 {
		t.Fatalf("free camera defaults %+v", f)
	}
	inv := build[*Inventory](t, newInventory, `{}`)
	if inv.size != 5 || inv.containerType != "" {
		t.Fatalf("inventory defaults %+v", inv)
	}
}

// Every vanilla use of the riding components builds.
func TestVanillaComponentsBuild(t *testing.T) {
	factories := map[string]mob.ComponentFactory{
		"minecraft:rideable": newRideable, "minecraft:input_ground_controlled": newGroundControlled,
		"minecraft:free_camera_controlled": newFreeCamera, "minecraft:item_controllable": newItemControllable,
		"minecraft:boostable": newBoostable, "minecraft:can_power_jump": newPowerJump, "minecraft:dash_action": newDash,
		"minecraft:vertical_movement_action": newVerticalMovement, "minecraft:addrider": newAddRider,
		"minecraft:equippable": newEquippable, "minecraft:inventory": newInventory,
	}
	n := 0
	for _, f := range data.Entities() {
		var v struct {
			Entity struct {
				Components json.RawMessage            `json:"components"`
				Groups     map[string]json.RawMessage `json:"component_groups"`
			} `json:"minecraft:entity"`
		}
		if err := json.Unmarshal(f.Data, &v); err != nil {
			t.Fatal(err)
		}
		sets := []json.RawMessage{v.Entity.Components}
		for _, g := range v.Entity.Groups {
			sets = append(sets, g)
		}
		for _, raw := range sets {
			cs, err := mob.ParseOrderedObject(raw)
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range cs {
				if fac, ok := factories[c.Key]; ok {
					if _, err := fac(c.Key, c.Raw); err != nil {
						t.Errorf("%v %v: %v", f.Name, c.Key, err)
					}
					n++
				}
			}
		}
	}
	if n < 80 {
		t.Fatalf("only %d uses built", n)
	}
}
