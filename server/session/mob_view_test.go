package session

import (
	"bytes"
	"fmt"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// fakeMobType is an entity type with properties like the vanilla cow's,
// plus the other kinds of property.
type fakeMobType struct {
	id    string
	props []world.EntityProperty
}

func (t *fakeMobType) Open(*world.Tx, *world.EntityHandle, *world.EntityData) world.Entity {
	return nil
}
func (t *fakeMobType) EncodeEntity() string                        { return t.id }
func (t *fakeMobType) BBox(world.Entity) cube.BBox                 { return cube.Box(-0.45, 0, -0.45, 0.45, 1.4, 0.45) }
func (t *fakeMobType) DecodeNBT(map[string]any, *world.EntityData) {}
func (t *fakeMobType) EncodeNBT(*world.EntityData) map[string]any  { return nil }
func (t *fakeMobType) EntityProperties() []world.EntityProperty    { return t.props }

type fakeMobConfig struct{}

func (fakeMobConfig) Apply(*world.EntityData) {}

var climate = []string{"temperate", "warm", "cold"}

// newFakeMobType returns a type with unsorted, partly unsynced properties
// and a unique identifier, so that the schema cache is not shared.
func newFakeMobType(t *testing.T) *fakeMobType {
	return &fakeMobType{id: "test:" + t.Name(), props: []world.EntityProperty{
		{Name: "minecraft:sound_variant", Type: world.EntityPropertyEnum, Enum: []string{"default", "moody"}, ClientSync: true, Default: "default"},
		{Name: "minecraft:is_waxed", Type: world.EntityPropertyBool, Default: false},
		{Name: "minecraft:climate_variant", Type: world.EntityPropertyEnum, Enum: climate, ClientSync: true, Default: "temperate"},
		{Name: "test:swaying_ticks", Type: world.EntityPropertyInt, Min: 0, Max: 6, ClientSync: true, Default: 0},
		{Name: "test:glow", Type: world.EntityPropertyFloat, Min: 0, Max: 2, ClientSync: true, Default: 0.0},
		{Name: "minecraft:has_nectar", Type: world.EntityPropertyBool, ClientSync: true, Default: "query.had_component_group('has_nectar')"},
	}}
}

// fakeMob implements every optional interface a mob may implement.
type fakeMob struct {
	h                                   *world.EntityHandle
	pos                                 mgl64.Vec3
	rot                                 cube.Rotation
	head, body                          float64
	health, maxHealth, speed            float64
	props                               map[string]any
	target                              *world.EntityHandle
	colour, colour2                     item.Colour
	sheared, saddled, tamed, sitting    bool
	climbing, shaking, chested          bool
	ignited, powered, invisible, isBaby bool
	armour                              *inventory.Armour
	bodyArmour, mainHand                item.Stack
}

func (m *fakeMob) Close() error                      { return nil }
func (m *fakeMob) H() *world.EntityHandle            { return m.h }
func (m *fakeMob) Position() mgl64.Vec3              { return m.pos }
func (m *fakeMob) Rotation() cube.Rotation           { return m.rot }
func (m *fakeMob) HeadYaw() float64                  { return m.head }
func (m *fakeMob) BodyYaw() float64                  { return m.body }
func (m *fakeMob) Health() float64                   { return m.health }
func (m *fakeMob) MaxHealth() float64                { return m.maxHealth }
func (m *fakeMob) Speed() float64                    { return m.speed }
func (m *fakeMob) ActorProperties() map[string]any   { return m.props }
func (m *fakeMob) TargetEntity() *world.EntityHandle { return m.target }
func (m *fakeMob) MobColour() item.Colour            { return m.colour }
func (m *fakeMob) MobColour2() item.Colour           { return m.colour2 }
func (m *fakeMob) Sheared() bool                     { return m.sheared }
func (m *fakeMob) Saddled() bool                     { return m.saddled }
func (m *fakeMob) Tamed() bool                       { return m.tamed }
func (m *fakeMob) Sitting() bool                     { return m.sitting }
func (m *fakeMob) WallClimbing() bool                { return m.climbing }
func (m *fakeMob) Shaking() bool                     { return m.shaking }
func (m *fakeMob) Chested() bool                     { return m.chested }
func (m *fakeMob) Ignited() bool                     { return m.ignited }
func (m *fakeMob) Powered() bool                     { return m.powered }
func (m *fakeMob) FuseLength() time.Duration         { return time.Second }
func (m *fakeMob) Baby() bool                        { return m.isBaby }
func (m *fakeMob) Scale() float64 {
	if m.isBaby {
		return 0.5
	}
	return 1
}
func (m *fakeMob) Invisible() bool                     { return m.invisible }
func (m *fakeMob) NameTag() string                     { return "" }
func (m *fakeMob) Armour() *inventory.Armour           { return m.armour }
func (m *fakeMob) BodyArmour() item.Stack              { return m.bodyArmour }
func (m *fakeMob) HeldItems() (item.Stack, item.Stack) { return m.mainHand, item.Stack{} }
func (m *fakeMob) BedrockMetadata(md protocol.EntityMetadata) {
	md[protocol.EntityDataKeyStrength] = int32(3)
}

func newFakeMob(t *testing.T) *fakeMob {
	return &fakeMob{
		h:         world.EntitySpawnOpts{}.New(newFakeMobType(t), fakeMobConfig{}),
		pos:       mgl64.Vec3{1, 64, 2},
		rot:       cube.Rotation{90, 10},
		head:      120,
		body:      80,
		health:    7,
		maxHealth: 10,
		speed:     0.25,
		props:     map[string]any{"minecraft:climate_variant": "warm", "test:swaying_ticks": 3, "test:glow": 1.5},
	}
}

// newTestSession returns a Session that collects the packets it writes.
func newTestSession() *Session {
	return &Session{
		conf:             Config{Log: slog.New(slog.DiscardHandler)},
		packets:          make(chan packet.Packet, 64),
		closeBackground:  make(chan struct{}),
		entityRuntimeIDs: map[*world.EntityHandle]uint64{},
		entities:         map[uint64]*world.EntityHandle{},
		hiddenEntities:   map[uuid.UUID]struct{}{},
		br:               world.DefaultBlockRegistry,
		// Runtime ID 1 is the player itself.
		currentEntityRuntimeID: selfEntityRuntimeID,
	}
}

// written returns the packets s wrote so far.
func written(s *Session) []packet.Packet {
	var pks []packet.Packet
	for {
		select {
		case pk := <-s.packets:
			pks = append(pks, pk)
		default:
			return pks
		}
	}
}

// only returns the single packet of type T written by s.
func only[T packet.Packet](t *testing.T, s *Session) T {
	t.Helper()
	var found []T
	for _, pk := range written(s) {
		if v, ok := pk.(T); ok {
			found = append(found, v)
		}
	}
	if len(found) != 1 {
		var zero T
		t.Fatalf("want one %T, got %d", zero, len(found))
	}
	return found[0]
}

func TestSyncActorPropertySchema(t *testing.T) {
	s := newTestSession()
	typ := newFakeMobType(t)
	s.sendActorPropertySchemas([]world.EntityType{typ, &fakeMobType{id: "test:none"}})
	pk := only[*packet.SyncActorProperty](t, s)

	want := map[string]any{"type": typ.id, "properties": []map[string]any{
		{"name": "minecraft:climate_variant", "type": int32(3), "enum": climate},
		{"name": "minecraft:has_nectar", "type": int32(2)},
		{"name": "minecraft:sound_variant", "type": int32(3), "enum": []string{"default", "moody"}},
		{"name": "test:glow", "type": int32(1), "min": float32(0), "max": float32(2)},
		{"name": "test:swaying_ticks", "type": int32(0), "min": int32(0), "max": int32(6)},
	}}
	if !reflect.DeepEqual(pk.PropertyData, want) {
		t.Fatalf("property data:\n got %#v\nwant %#v", pk.PropertyData, want)
	}

	// The packet must encode and decode as network NBT.
	buf := new(bytes.Buffer)
	pk.Marshal(protocol.NewWriter(buf, 0))
	var back packet.SyncActorProperty
	back.Marshal(protocol.NewReader(bytes.NewReader(buf.Bytes()), 0, false))
	if props, _ := back.PropertyData["properties"].([]any); len(props) != 5 || back.PropertyData["type"] != typ.id {
		t.Fatalf("decoded %#v", back.PropertyData)
	}
}

func TestTooManyPropertiesHaveNoSchema(t *testing.T) {
	typ := &fakeMobType{id: "test:" + t.Name()}
	for i := range maxEntityProperties + 1 {
		typ.props = append(typ.props, world.EntityProperty{Name: fmt.Sprintf("test:p%02d", i), Type: world.EntityPropertyBool, ClientSync: true})
	}
	if entityPropertySchema(typ) != nil {
		t.Fatal("BDS loads no properties for a type with more than 32")
	}
}

func TestAddActorMob(t *testing.T) {
	s := newTestSession()
	m := newFakeMob(t)
	m.target = world.EntitySpawnOpts{}.New(newFakeMobType(t), fakeMobConfig{})
	s.entityRuntimeIDs[m.target], s.entities[42] = 42, m.target
	m.colour, m.colour2 = item.ColourRed(), item.ColourBlue()
	m.sheared, m.tamed, m.climbing, m.ignited, m.powered, m.isBaby = true, true, true, true, true, true

	s.ViewEntity(m)
	pk := only[*packet.AddActor](t, s)

	if pk.EntityType != m.h.Type().EncodeEntity() {
		t.Errorf("type %q", pk.EntityType)
	}
	if pk.Yaw != 90 || pk.HeadYaw != 120 || pk.BodyYaw != 80 || pk.Pitch != 10 {
		t.Errorf("rotation yaw %v head %v body %v pitch %v", pk.Yaw, pk.HeadYaw, pk.BodyYaw, pk.Pitch)
	}
	wantProps := protocol.EntityProperties{
		// climate_variant warm, has_nectar has no constant default, sound_variant default, swaying_ticks 3.
		IntegerProperties: []protocol.IntegerEntityProperty{{Index: 0, Value: 1}, {Index: 2, Value: 0}, {Index: 4, Value: 3}},
		FloatProperties:   []protocol.FloatEntityProperty{{Index: 3, Value: 1.5}},
	}
	if !reflect.DeepEqual(pk.EntityProperties, wantProps) {
		t.Errorf("properties:\n got %+v\nwant %+v", pk.EntityProperties, wantProps)
	}
	if len(pk.Attributes) != 2 || pk.Attributes[0] != (protocol.AttributeValue{Name: "minecraft:health", Value: 7, Max: 10}) ||
		pk.Attributes[1].Name != "minecraft:movement" || pk.Attributes[1].Value != 0.25 {
		t.Errorf("attributes %+v", pk.Attributes)
	}

	md := pk.EntityMetadata
	for name, flag := range map[string]uint8{
		"sheared": protocol.EntityDataFlagSheared, "tamed": protocol.EntityDataFlagTamed,
		"wall climbing": protocol.EntityDataFlagWallClimbing, "ignited": protocol.EntityDataFlagIgnited,
		"powered": protocol.EntityDataFlagPowered, "baby": protocol.EntityDataFlagBaby,
	} {
		if !md.Flag(protocol.EntityDataKeyFlags, flag) {
			t.Errorf("flag %s not set", name)
		}
	}
	for name, flag := range map[string]uint8{
		"saddled": protocol.EntityDataFlagSaddled, "sitting": protocol.EntityDataFlagSitting,
		"shaking": protocol.EntityDataFlagShaking, "chested": protocol.EntityDataFlagChested,
	} {
		if md.Flag(protocol.EntityDataKeyFlags, flag) {
			t.Errorf("flag %s set", name)
		}
	}
	for key, want := range map[uint32]any{
		protocol.EntityDataKeyTarget:        int64(42),
		protocol.EntityDataKeyColorIndex:    item.ColourRed().Uint8(),
		protocol.EntityDataKeyColorTwoIndex: item.ColourBlue().Uint8(),
		protocol.EntityDataKeyFuseTime:      int32(20),
		protocol.EntityDataKeyScale:         float32(0.5),
		protocol.EntityDataKeyStrength:      int32(3),
	} {
		if md[key] != want {
			t.Errorf("data %d = %#v, want %#v", key, md[key], want)
		}
	}

	// The target the viewer does not know is no target.
	delete(s.entityRuntimeIDs, m.target)
	s.ViewEntityState(m)
	if got := only[*packet.SetActorData](t, s).EntityMetadata[protocol.EntityDataKeyTarget]; got != int64(0) {
		t.Errorf("unknown target sent as %v", got)
	}
}

func TestMobMovementHeadYaw(t *testing.T) {
	s := newTestSession()
	m := newFakeMob(t)
	s.ViewEntity(m)
	written(s)

	s.ViewEntityMovement(m, mgl64.Vec3{1, 65, 2}, cube.Rotation{45, -20}, true)
	pk := only[*packet.MoveActorAbsolute](t, s)
	if pk.Rotation != (mgl32.Vec3{-20, 45, 120}) {
		t.Errorf("rotation %v, want pitch, yaw, head yaw", pk.Rotation)
	}
	if pk.Flags&packet.MoveFlagOnGround == 0 {
		t.Error("not on ground")
	}

	s.ViewEntityTeleport(m, mgl64.Vec3{5, 70, 5})
	for _, p := range written(s) {
		if mv, ok := p.(*packet.MoveActorAbsolute); ok && mv.Rotation != (mgl32.Vec3{10, 90, 120}) {
			t.Errorf("teleport rotation %v", mv.Rotation)
		}
	}
}

func TestSetActorDataProperties(t *testing.T) {
	s := newTestSession()
	m := newFakeMob(t)
	s.ViewEntity(m)
	written(s)

	m.props = map[string]any{"minecraft:climate_variant": 2, "minecraft:sound_variant": "moody", "minecraft:has_nectar": true, "test:glow": float32(0.5)}
	s.ViewEntityState(m)
	pk := only[*packet.SetActorData](t, s)
	want := protocol.EntityProperties{
		IntegerProperties: []protocol.IntegerEntityProperty{{Index: 0, Value: 2}, {Index: 1, Value: 1}, {Index: 2, Value: 1}, {Index: 4, Value: 0}},
		FloatProperties:   []protocol.FloatEntityProperty{{Index: 3, Value: 0.5}},
	}
	if !reflect.DeepEqual(pk.EntityProperties, want) {
		t.Errorf("properties:\n got %+v\nwant %+v", pk.EntityProperties, want)
	}

	// Values out of an enum are left out.
	m.props = map[string]any{"minecraft:climate_variant": "nether", "minecraft:sound_variant": 7}
	if got := entityProperties(m).IntegerProperties; len(got) != 1 || got[0].Index != 4 {
		t.Errorf("bad enum values sent: %+v", got)
	}
}

func TestViewEntityHealth(t *testing.T) {
	s := newTestSession()
	m := newFakeMob(t)
	s.ViewEntity(m)
	written(s)

	m.health = 3.5
	s.ViewEntityHealth(m)
	pk := only[*packet.UpdateAttributes](t, s)
	if pk.EntityRuntimeID != s.entityRuntimeID(m) || len(pk.Attributes) != 2 {
		t.Fatalf("packet %+v", pk)
	}
	if h := pk.Attributes[0]; h.Name != "minecraft:health" || h.Value != 3.5 || h.Max != 10 || h.Default != 10 {
		t.Errorf("health %+v", h)
	}
}

func TestMobEquipment(t *testing.T) {
	s := newTestSession()
	m := newFakeMob(t)
	m.invisible = true
	m.mainHand = item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1)
	m.bodyArmour = item.NewStack(item.Apple{}, 1)
	s.ViewEntity(m)
	written(s)

	// Without an armour inventory only the body slot is filled.
	s.ViewEntityArmour(m)
	pk := only[*packet.MobArmourEquipment](t, s)
	if pk.Body.Stack.NetworkID == 0 || pk.Helmet.Stack.NetworkID != 0 {
		t.Errorf("armour %+v", pk)
	}

	m.armour = inventory.NewArmour(func(int, item.Stack, item.Stack) {})
	m.armour.SetHelmet(item.NewStack(item.Helmet{Tier: item.ArmourTierIron{}}, 1))
	s.ViewEntityArmour(m)
	if pk := only[*packet.MobArmourEquipment](t, s); pk.Helmet.Stack.NetworkID == 0 || pk.Body.Stack.NetworkID == 0 {
		t.Errorf("armour %+v", pk)
	}

	// An invisible mob still shows what it holds.
	s.ViewEntityItems(m)
	var held int
	for _, p := range written(s) {
		if eq, ok := p.(*packet.MobEquipment); ok && eq.WindowID == protocol.WindowIDInventory && eq.NewItem.Stack.NetworkID != 0 {
			held++
		}
	}
	if held != 1 {
		t.Error("main hand item of an invisible mob not shown")
	}
}

// fakeBehaviour is the behaviour of an *entity.Ent mob.
type fakeBehaviour struct{ head float64 }

func (fakeBehaviour) Tick(*entity.Ent, *world.Tx) *entity.Movement { return nil }
func (b fakeBehaviour) HeadYaw() float64                           { return b.head }
func (fakeBehaviour) Saddled() bool                                { return true }

func TestBehaviourInterfaces(t *testing.T) {
	m := &fakeMob{h: world.EntitySpawnOpts{}.New(&fakeMobType{id: "test:" + t.Name()}, fakeMobConfig{})}
	e := struct {
		world.Entity
		behaviour
	}{m, behaviour{fakeBehaviour{head: 33}}}
	if got := entityHeadYaw(e, 1); got != 33 {
		t.Errorf("head yaw from behaviour %v", got)
	}
	s := newTestSession()
	if md := s.parseEntityMetadata(e); !md.Flag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagSaddled) {
		t.Error("saddled flag of behaviour not set")
	}
}

type behaviour struct{ b fakeBehaviour }

func (b behaviour) Behaviour() entity.Behaviour { return b.b }
