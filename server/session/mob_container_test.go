package session

import (
	"context"
	"log/slog"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestEntityWindowItemName(t *testing.T) {
	for in, want := range map[string]string{"saddle": "minecraft:saddle", "minecraft:copper_horse_armor": "minecraft:copper_horse_armor", "": ""} {
		if got := itemName(in); got != want {
			t.Errorf("itemName(%q) = %q, want %q", in, got, want)
		}
	}
}

// windowEntType is an entity type whose behaviour has an inventory window.
type windowEntType struct{}

func (windowEntType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	return entity.Open(tx, h, d)
}
func (windowEntType) EncodeEntity() string                        { return "minecraft:donkey" }
func (windowEntType) BBox(world.Entity) cube.BBox                 { return cube.Box(-0.7, 0, -0.7, 0.7, 1.6, 0.7) }
func (windowEntType) DecodeNBT(map[string]any, *world.EntityData) {}
func (windowEntType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

type windowConf struct{ b *windowBehaviour }

func (c windowConf) Apply(d *world.EntityData) { d.Data = c.b }

type windowBehaviour struct {
	inv    *inventory.Inventory
	closed int
}

func (*windowBehaviour) Tick(*entity.Ent, *world.Tx) *entity.Movement { return nil }
func (b *windowBehaviour) EntityWindow(_ *entity.Ent, _ world.Entity, layout entity.WindowLayout) (entity.EntityWindow, bool) {
	if layout != entity.WindowLayoutBedrock {
		return entity.EntityWindow{}, false
	}
	return entity.EntityWindow{
		Inventory:  b.inv,
		Equip:      []entity.EquipSlot{{Accepted: []string{"saddle"}, Icon: "saddle", Slot: 0}},
		ChestSlots: 15,
		OnClose:    func() { b.closed++ },
	}, true
}

// OpenEntityContainer sends the mount screen as Geyser does (UpdateEquip with
// the horse window type, entity unique ID and accepted items, then the
// window contents); HORSE_EQUIP and LEVEL_ENTITY reach the window and
// closing runs OnClose.
func TestOpenEntityContainer(t *testing.T) {
	w := world.Config{Log: slog.New(slog.DiscardHandler), Synchronous: true}.New()
	defer func() { _ = w.Close() }()
	s := newTestSession()
	b := &windowBehaviour{inv: inventory.New(16, nil)}
	err := w.Do(func(tx *world.Tx) {
		viewer := tx.AddEntity(world.EntitySpawnOpts{}.New(windowEntType{}, windowConf{&windowBehaviour{}}))
		s.ent = viewer.H()
		s.et.ids[viewer.H()] = selfEntityRuntimeID
		donkey := tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{3, 0, 0}}.New(windowEntType{}, windowConf{b}))
		s.et.ids[donkey.H()] = 7
		if !s.OpenEntityContainer(donkey, tx) {
			t.Fatal("window not opened")
		}
		pks := written(s)
		var eq *packet.UpdateEquip
		var content *packet.InventoryContent
		for _, pk := range pks {
			switch pk := pk.(type) {
			case *packet.UpdateEquip:
				eq = pk
			case *packet.InventoryContent:
				content = pk
			}
		}
		if eq == nil || eq.WindowType != protocol.ContainerTypeHorse || eq.EntityUniqueID != 7 {
			t.Fatalf("UpdateEquip %+v", eq)
		}
		var data map[string]any
		if err := nbt.UnmarshalEncoding(eq.SerialisedInventoryData, &data, nbt.NetworkLittleEndian); err != nil {
			t.Fatal(err)
		}
		slots, _ := data["slots"].([]any)
		if len(slots) != 1 {
			t.Fatalf("slots %v", data)
		}
		if content == nil || content.WindowID != uint32(eq.WindowID) || len(content.Content) != 16 {
			t.Fatalf("InventoryContent %+v", content)
		}
		for _, id := range []int32{protocol.ContainerHorseEquip, protocol.ContainerLevelEntity} {
			if inv, ok := s.invByID(id, tx); !ok || inv != b.inv {
				t.Errorf("container %d does not reach the window", id)
			}
		}
		s.closeCurrentContainer(tx, true)
		if b.closed != 1 || s.entityWindowOpen() {
			t.Fatal("closing did not close the entity window")
		}
		if _, ok := s.invByID(protocol.ContainerHorseEquip, tx); ok {
			t.Error("HORSE_EQUIP still open")
		}
	}).Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}
