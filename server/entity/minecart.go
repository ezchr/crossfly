//go:build crossfly_full

package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Minecarts move as Java Edition's OldMinecartBehavior does (Java Edition 26.3 without the minecart_improvements
// experiment), see minecart_behaviour.go.

// MinecartKind is the kind of a minecart: what it carries.
type MinecartKind = item.MinecartKind

// The kinds of minecarts.
const (
	MinecartPlain  = item.MinecartPlain
	MinecartChest  = item.MinecartChest
	MinecartHopper = item.MinecartHopper
	MinecartTNT    = item.MinecartTNT
)

// The minecart entity types.
var (
	MinecartType       = minecartType{kind: MinecartPlain}
	ChestMinecartType  = minecartType{kind: MinecartChest}
	HopperMinecartType = minecartType{kind: MinecartHopper}
	TNTMinecartType    = minecartType{kind: MinecartTNT}
)

// Java Edition's minecart size, 0.98 by 0.7 as floats, as EntityDimensions.makeBoundingBox uses them.
var (
	minecartHalfWidth = float64(float32(0.98) / 2)
	minecartHeight    = float64(float32(0.7))
)

// minecartBox is the bounding box of a minecart at pos.
func minecartBox(pos mgl64.Vec3) cube.BBox {
	return cube.Box(pos[0]-minecartHalfWidth, pos[1], pos[2]-minecartHalfWidth, pos[0]+minecartHalfWidth, pos[1]+minecartHeight, pos[2]+minecartHalfWidth)
}

// NewMinecart creates a minecart of the kind passed.
func NewMinecart(opts world.EntitySpawnOpts, kind MinecartKind) *world.EntityHandle {
	return opts.New(minecartTypeOf(kind), MinecartConfig{Kind: kind})
}

// MinecartConfig configures a minecart.
type MinecartConfig struct {
	Kind MinecartKind
}

// Apply ...
func (conf MinecartConfig) Apply(data *world.EntityData) {
	data.Data = newMinecartBehaviour(conf.Kind)
}

func minecartTypeOf(kind MinecartKind) minecartType {
	switch kind {
	case MinecartChest:
		return ChestMinecartType
	case MinecartHopper:
		return HopperMinecartType
	case MinecartTNT:
		return TNTMinecartType
	}
	return MinecartType
}

type minecartType struct{ kind MinecartKind }

func (t minecartType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &Minecart{Ent: Open(tx, handle, data)}
}

// EncodeEntity ...
func (t minecartType) EncodeEntity() string {
	return item.Minecart{Kind: t.kind}.EntityName()
}

// NetworkOffset is half the height of the minecart, where Bedrock Edition has its position.
func (minecartType) NetworkOffset() float64 { return 0.35 }

// BBox ...
func (minecartType) BBox(world.Entity) cube.BBox {
	return cube.Box(-minecartHalfWidth, 0, -minecartHalfWidth, minecartHalfWidth, minecartHeight, minecartHalfWidth)
}

// DecodeNBT ...
func (t minecartType) DecodeNBT(m map[string]any, data *world.EntityData) {
	b := newMinecartBehaviour(t.kind)
	b.flipped = nbtconv.Bool(m, "FlippedRotation")
	b.yRot = float32(data.Rot.Yaw())
	if b.inv != nil {
		nbtconv.InvFromNBT(b.inv, nbtconv.Slice(m, "Items"))
	}
	if _, ok := m["Enabled"]; ok {
		b.hopperEnabled = nbtconv.Bool(m, "Enabled")
	}
	if v, ok := m["fuse"].(int32); ok {
		b.fuse = int(v)
	}
	data.Data = b
}

// EncodeNBT ...
func (t minecartType) EncodeNBT(data *world.EntityData) map[string]any {
	b := data.Data.(*minecartBehaviour)
	m := map[string]any{"FlippedRotation": boolByte(b.flipped)}
	if b.inv != nil {
		m["Items"] = nbtconv.InvToNBT(b.inv)
	}
	switch t.kind {
	case MinecartHopper:
		m["Enabled"] = boolByte(b.hopperEnabled)
	case MinecartTNT:
		m["fuse"] = int32(b.fuse)
	}
	return m
}

// Minecart is a minecart entity. A plain minecart can be ridden.
type Minecart struct {
	*Ent
}

func (c *Minecart) behaviour() *minecartBehaviour {
	return c.Behaviour().(*minecartBehaviour)
}

// Kind returns the kind of the minecart.
func (c *Minecart) Kind() MinecartKind {
	return c.behaviour().kind
}

// MinecartInventory returns the items of a chest or hopper minecart, or nil for other minecarts.
func (c *Minecart) MinecartInventory() *inventory.Inventory {
	return c.behaviour().inv
}

// DetectorBox returns the bounding box a detector rail sees. Java Edition runs scheduled block ticks before entities
// move, so the scheduled check of a detector rail sees a minecart where it was at the start of the tick.
func (c *Minecart) DetectorBox(tx *world.Tx, scheduled bool) cube.BBox {
	b := c.behaviour()
	if scheduled && b.tickedAt == tx.CurrentTick() {
		return minecartBox(b.tickStart)
	}
	return minecartBox(c.Position())
}

// MinecartWobble returns how the minecart shakes after a hit: Java Edition's hurt time, hurt direction and damage.
func (c *Minecart) MinecartWobble() (hurtTime, hurtDir int, damage float32) {
	return c.behaviour().MinecartWobble()
}

// TNTFuse returns the ticks left until a primed TNT minecart explodes, and whether it is primed.
func (c *Minecart) TNTFuse() (int, bool) {
	return c.behaviour().TNTFuse()
}

// SeatPositions ...
func (c *Minecart) SeatPositions() []mgl64.Vec3 {
	if c.behaviour().kind != MinecartPlain {
		return nil
	}
	return []mgl64.Vec3{minecartSeat}
}

// minecartSeat is the seat offset sent to Bedrock clients: the minecart's -0.2 seat plus the vanilla offset of a
// player rider, from the minecart's network position.
var minecartSeat = mgl64.Vec3{0, 1.02001, 0}

// NextFreeSeatIndex ...
func (c *Minecart) NextFreeSeatIndex(mgl64.Vec3) (int, bool) {
	b := c.behaviour()
	return 0, b.kind == MinecartPlain && b.rider == nil && !b.removed
}

// AcceptsRider refuses sneaking players, as Java Edition's Minecart.interact.
func (c *Minecart) AcceptsRider(rider world.Entity) bool {
	s, ok := rider.(interface{ Sneaking() bool })
	return !ok || !s.Sneaking()
}

// ControllingRider returns the rider of the minecart. It does not steer, but Java Edition reads its input.
func (c *Minecart) ControllingRider() *world.EntityHandle {
	return c.behaviour().rider
}

// ControllingSeatIndex ...
func (c *Minecart) ControllingSeatIndex() int {
	if c.behaviour().rider == nil {
		return -1
	}
	return 0
}

// Riders ...
func (c *Minecart) Riders() []RiderSeat {
	if r := c.behaviour().rider; r != nil {
		return []RiderSeat{{Handle: r, SeatIndex: 0}}
	}
	return nil
}

// AddRider ...
func (c *Minecart) AddRider(rider *world.EntityHandle, seatIndex int) bool {
	b := c.behaviour()
	if seatIndex != 0 || b.kind != MinecartPlain || b.rider != nil || b.removed {
		return false
	}
	b.rider = rider
	b.moveLeft, b.moveForward = 0, 0
	return true
}

// RemoveRider ...
func (c *Minecart) RemoveRider(rider *world.EntityHandle) {
	if b := c.behaviour(); b.rider == rider {
		b.rider = nil
		b.moveLeft, b.moveForward = 0, 0
	}
}

// MoveInput keeps the keys the rider holds: Java Edition's old minecart movement lets the rider push a minecart that
// has almost stopped.
func (c *Minecart) MoveInput(vector mgl64.Vec2, yaw, _ float32) {
	b := c.behaviour()
	b.moveLeft, b.moveForward, b.moveYaw = sign(vector[0]), sign(vector[1]), yaw
}

func sign(v float64) float64 {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

// DismountPosition returns where a rider getting off goes: next to the minecart, as Java Edition's
// AbstractMinecart.getDismountLocationForPassenger.
func (c *Minecart) DismountPosition(rider world.Entity) mgl64.Vec3 {
	return minecartDismountPosition(c.Ent, rider)
}

// InteractText ...
func (c *Minecart) InteractText(user world.Entity) string {
	if _, free := c.NextFreeSeatIndex(mgl64.Vec3{}); !free || !c.AcceptsRider(user) {
		return ""
	}
	return "action.interact.ride.minecart"
}

// Pick returns the minecart item.
func (c *Minecart) Pick() item.Stack {
	return item.NewStack(item.Minecart{Kind: c.behaviour().kind}, 1)
}
