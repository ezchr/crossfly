//go:build crossfly_full

package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// Boats and chest boats (rafts for bamboo), as Bedrock Edition's minecraft:boat and minecraft:chest_boat: the
// behaviour pack entity files (data/entities/boat.json, chest_boat.json) give the box, seats and the inventory,
// the 1.21.3 dump (Boat.cpp, ChestBoat.cpp, BoatItem.cpp) the hurt, drop and placing rules. A boat a player steers
// is moved by that player's client (Bedrock: PlayerAuthInput with ClientPredictedVehicle; Java: move_vehicle), as
// BDS takes it. The server moves boats nobody steers: see boat_behaviour.go.

// BoatWood is the wood of a boat: its Bedrock Variant.
type BoatWood = item.BoatWood

// The boat entity types.
var (
	BoatType      = boatType{}
	ChestBoatType = boatType{chest: true}
)

// The size of a boat, from minecraft:collision_box.
const (
	boatWidth  = 1.4
	boatHeight = 0.455
)

// boatNetworkOffset is how far above its feet Bedrock Edition has a boat: Boat::reloadHardcoded's
// _setHeightOffset(0.375).
const boatNetworkOffset = 0.375

// boatRiderOffset is the vanilla offset of a player rider added to the seat positions of minecraft:rideable, as the
// other seats of Crossfly send it (cushionSeat, minecartSeat).
const boatRiderOffset = 1.22001

// boatSeatY returns the height of the seats of minecraft:rideable for a wood: -0.2, or 0.1 on a bamboo raft
// (minecraft:can_ride_bamboo).
func boatSeatY(wood BoatWood) float64 {
	if wood == item.BoatBamboo {
		return 0.1 + boatRiderOffset
	}
	return -0.2 + boatRiderOffset
}

// NewBoat creates a boat or chest boat of a wood.
func NewBoat(opts world.EntitySpawnOpts, wood BoatWood, chest bool) *world.EntityHandle {
	t := BoatType
	if chest {
		t = ChestBoatType
	}
	return opts.New(t, BoatConfig{Wood: wood, Chest: chest})
}

// BoatConfig configures a boat.
type BoatConfig struct {
	Wood BoatWood
	// Chest makes a chest boat. It must match the type the boat is created with (ChestBoatType).
	Chest bool
}

// Apply ...
func (conf BoatConfig) Apply(data *world.EntityData) {
	data.Data = newBoatBehaviour(conf.Wood, conf.Chest)
}

type boatType struct{ chest bool }

func (t boatType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &Boat{Ent: Open(tx, handle, data)}
}

// EncodeEntity ...
func (t boatType) EncodeEntity() string {
	if t.chest {
		return "minecraft:chest_boat"
	}
	return "minecraft:boat"
}

// NetworkOffset ...
func (boatType) NetworkOffset() float64 { return boatNetworkOffset }

// BBox ...
func (boatType) BBox(world.Entity) cube.BBox {
	return cube.Box(-boatWidth/2, 0, -boatWidth/2, boatWidth/2, boatHeight, boatWidth/2)
}

// DecodeNBT ...
func (t boatType) DecodeNBT(m map[string]any, data *world.EntityData) {
	v, _ := m["Variant"].(int32)
	b := newBoatBehaviour(BoatWood(v), t.chest)
	if b.inv != nil {
		nbtconv.InvFromNBT(b.inv, nbtconv.Slice(m, "ChestItems"))
	}
	data.Data = b
}

// EncodeNBT ...
func (t boatType) EncodeNBT(data *world.EntityData) map[string]any {
	b := data.Data.(*boatBehaviour)
	m := map[string]any{"Variant": int32(b.wood)}
	if b.inv != nil {
		m["ChestItems"] = nbtconv.InvToNBT(b.inv)
	}
	return m
}

// Boat is a boat or chest boat entity.
type Boat struct {
	*Ent
}

func (c *Boat) behaviour() *boatBehaviour {
	return c.Behaviour().(*boatBehaviour)
}

// Wood returns the wood of the boat.
func (c *Boat) Wood() BoatWood { return c.behaviour().wood }

// Chest reports whether the boat is a chest boat.
func (c *Boat) Chest() bool { return c.behaviour().inv != nil }

// Inventory returns the 27 slots of a chest boat, or nil.
func (c *Boat) Inventory() *inventory.Inventory { return c.behaviour().inv }

// Variant is the Bedrock Variant of the boat (its wood), which Java viewers pick the boat type by.
func (c *Boat) Variant() int32 { return int32(c.behaviour().wood) }

// MinecartWobble returns how the boat shakes after a hit: the hurt time, direction and damage both editions'
// VehicleEntity data carry.
func (c *Boat) MinecartWobble() (hurtTime, hurtDir int, damage float32) {
	b := c.behaviour()
	return b.hurtTime, b.hurtDir, float32(boatIntegrity - b.integrity)
}

// BoatPaddles reports which paddles the boat's driver works (Java Edition's DATA_ID_PADDLE_LEFT and _RIGHT).
func (c *Boat) BoatPaddles() (left, right bool) {
	b := c.behaviour()
	return b.paddleLeft, b.paddleRight
}

// PaddleInput sets the paddles the driver works: the PaddlingLeft/PaddlingRight flags of a Bedrock client's
// PlayerAuthInput, or a Java client's paddle_boat.
func (c *Boat) PaddleInput(rider world.Entity, left, right bool) {
	b := c.behaviour()
	if b.controller() != rider.H() {
		return
	}
	b.paddleLeft, b.paddleRight = left, right
}

// BedrockMetadata sets the actor data Boat::Boat defines: the hurt time (11), hurt direction (12), the row times
// of the paddles (13, 14) and the wood, plus the structural integrity (40 at the start) and the buoyancy the
// client floats the boat it steers with, as Geyser sends it.
func (c *Boat) BedrockMetadata(m protocol.EntityMetadata) {
	b := c.behaviour()
	m[protocol.EntityDataKeyVariant] = int32(b.wood)
	m[protocol.EntityDataKeyHurt] = int32(b.hurtTime)
	m[protocol.EntityDataKeyHurtDirection] = int32(b.hurtDir)
	m[protocol.EntityDataKeyStructuralIntegrity] = int32(max(b.integrity, 0))
	m[protocol.EntityDataKeyRowTimeLeft] = b.rowLeft
	m[protocol.EntityDataKeyRowTimeRight] = b.rowRight
	m[protocol.EntityDataKeyIsBuoyant] = byte(1)
	m[protocol.EntityDataKeyBuoyancyData] = boatBuoyancyData
	m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagStackable)
	if b.inv != nil {
		m[protocol.EntityDataKeyContainerType] = byte(protocol.ContainerTypeChestBoat)
		m[protocol.EntityDataKeyContainerSize] = int32(27)
	}
}

// boatBuoyancyData is minecraft:buoyant of boat.json as the client reads it from the actor data. Waves are off:
// the server floats the boat without them.
const boatBuoyancyData = `{"apply_gravity":true,"base_buoyancy":1.0,"big_wave_probability":0.03,"big_wave_speed":10.0,"drag_down_on_buoyancy_removed":0.0,"liquid_blocks":["minecraft:water","minecraft:flowing_water"],"simulate_waves":false}`

// SeatPositions returns the seats of minecraft:rideable: a lone rider sits in the middle, two sit front and back. A
// chest boat has the front seat only.
func (c *Boat) SeatPositions() []mgl64.Vec3 {
	b := c.behaviour()
	y := boatSeatY(b.wood)
	if b.inv != nil {
		return []mgl64.Vec3{{0.2, y, 0}}
	}
	if b.riders[0] != nil && b.riders[1] != nil {
		return []mgl64.Vec3{{0.2, y, 0}, {-0.6, y, 0}}
	}
	return []mgl64.Vec3{{0, y, 0}, {-0.6, y, 0}}
}

// NextFreeSeatIndex ...
func (c *Boat) NextFreeSeatIndex(mgl64.Vec3) (int, bool) {
	b := c.behaviour()
	if b.removed {
		return 0, false
	}
	for i := range len(c.SeatPositions()) {
		if b.riders[i] == nil {
			return i, true
		}
	}
	return 0, false
}

// AcceptsRider refuses sneaking players and, as Boat::canAddPassenger, anyone while the block above the boat is
// water.
func (c *Boat) AcceptsRider(rider world.Entity) bool {
	if s, ok := rider.(interface{ Sneaking() bool }); ok && s.Sneaking() {
		return false
	}
	above := cube.PosFromVec3(c.Position()).Side(cube.FaceUp)
	if l, ok := c.tx.Liquid(above); ok && isWaterLiquid(l) {
		return false
	}
	return true
}

// ControllingRider returns the rider in the first seat, as Boat::getControllingPlayer.
func (c *Boat) ControllingRider() *world.EntityHandle { return c.behaviour().controller() }

// ControllingSeatIndex ...
func (c *Boat) ControllingSeatIndex() int {
	if c.behaviour().controller() == nil {
		return -1
	}
	return 0
}

// ClientDriven reports whether rider's client moves the boat: the client of its driver.
func (c *Boat) ClientDriven(rider *world.EntityHandle) bool {
	return rider != nil && c.behaviour().controller() == rider
}

// Riders ...
func (c *Boat) Riders() []RiderSeat {
	var seats []RiderSeat
	for i, r := range c.behaviour().riders {
		if r != nil {
			seats = append(seats, RiderSeat{Handle: r, SeatIndex: i})
		}
	}
	return seats
}

// AddRider ...
func (c *Boat) AddRider(rider *world.EntityHandle, seatIndex int) bool {
	b := c.behaviour()
	if b.removed || seatIndex < 0 || seatIndex >= len(c.SeatPositions()) {
		return false
	}
	for i, r := range b.riders {
		if r == rider {
			b.riders[i] = nil
		}
	}
	if b.riders[seatIndex] != nil {
		return false
	}
	b.riders[seatIndex] = rider
	if seatIndex == 1 && b.riders[0] == nil {
		// The first one aboard drives.
		b.riders[0], b.riders[1] = rider, nil
	}
	return true
}

// RemoveRider ...
func (c *Boat) RemoveRider(rider *world.EntityHandle) {
	b := c.behaviour()
	for i, r := range b.riders {
		if r == rider {
			b.riders[i] = nil
		}
	}
	if b.riders[0] == nil && b.riders[1] != nil {
		// The passenger moves up to drive, as Bedrock's passenger list shifts.
		b.riders[0], b.riders[1] = b.riders[1], nil
	}
	if b.riders[0] == nil {
		b.paddleLeft, b.paddleRight = false, false
	}
}

// MoveInput does nothing: the driver's client moves the boat.
func (c *Boat) MoveInput(mgl64.Vec2, float32, float32) {}

// ClientVehicleMove takes the position the driver's client moved the boat to (its feet). Moves of more than 8
// blocks are refused, as for the other client-driven vehicles.
func (c *Boat) ClientVehicleMove(rider world.Entity, pos mgl64.Vec3, yaw, _ float64) bool {
	return c.behaviour().clientMove(c.Ent, rider, pos, yaw)
}

// SeatRotation turns riders 90 degrees, as the client turns them, and limits them to looking 90 degrees away
// (rotate_rider_by, lock_rider_rotation).
func (c *Boat) SeatRotation(int) SeatRotation {
	return SeatRotation{RotateBy: -90, LockRotation: true, LockDegrees: 90}
}

// Interact opens the chest of a chest boat for a sneaking player, as the inventory component of chest_boat.json.
func (c *Boat) Interact(user item.User, held item.Stack, _ mgl64.Vec3) (item.Stack, bool) {
	b := c.behaviour()
	if b.inv == nil || b.removed {
		return held, false
	}
	if s, ok := user.(interface{ Sneaking() bool }); !ok || !s.Sneaking() {
		return held, false
	}
	if o, ok := user.(interface{ OpenEntityContainer(e world.Entity) bool }); ok && o.OpenEntityContainer(c) {
		return held, true
	}
	return held, false
}

// InteractText ...
func (c *Boat) InteractText(user world.Entity) string {
	if _, free := c.NextFreeSeatIndex(mgl64.Vec3{}); !free || !c.AcceptsRider(user) {
		return ""
	}
	return "action.interact.ride.boat"
}

// Pick returns the boat item.
func (c *Boat) Pick() item.Stack {
	return item.NewStack(item.Boat{Wood: c.Wood(), Chest: c.Chest()}, 1)
}
