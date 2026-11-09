//go:build crossfly_full

package javasession

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/world"
)

// The saddle and body slots of a mount window take only what the entity
// accepts, with the legacy names of the entity files.
func TestMountAccepts(t *testing.T) {
	inv := inventory.New(17, nil)
	mountSlots.Store(inv, [2][]string{{"saddle"}, {"horsearmorleather", "horsearmoriron", "carpet"}})
	defer mountSlots.Delete(inv)
	saddle, iron := item.NewStack(item.Saddle{}, 1), item.NewStack(item.HorseArmour{Tier: item.HorseArmourIron}, 1)
	carpet := item.NewStack(block.Carpet{Colour: item.ColourRed()}, 1)
	for _, c := range []struct {
		idx  int
		it   item.Stack
		want bool
	}{
		{0, saddle, true}, {1, saddle, false}, {1, iron, true}, {0, iron, false}, {1, carpet, true},
		{0, item.NewStack(block.Dirt{}, 1), false}, {1, item.NewStack(item.HorseArmour{Tier: item.HorseArmourDiamond}, 1), false},
	} {
		if got := mountAccepts(inv, c.idx, c.it); got != c.want {
			t.Errorf("slot %d %v: %v, want %v", c.idx, c.it, got, c.want)
		}
	}
	r := slotRef{inv, 0, kindMountEquip}
	if r.mayPlace(item.NewStack(block.Dirt{}, 1)) || !r.mayPlace(saddle) {
		t.Error("mount slot ref ignores the accepted items")
	}
	if (&slotRef{inv, 5, kindPlain}).mayPlace(item.NewStack(block.Dirt{}, 1)) == false {
		t.Error("chest slot refuses dirt")
	}
}

// mountEntType is an entity type whose behaviour has a Java mount window.
type mountEntType struct{}

func (mountEntType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	return entity.Open(tx, h, d)
}
func (mountEntType) EncodeEntity() string                        { return "minecraft:horse" }
func (mountEntType) BBox(world.Entity) cube.BBox                 { return cube.Box(-0.7, 0, -0.7, 0.7, 1.6, 0.7) }
func (mountEntType) DecodeNBT(map[string]any, *world.EntityData) {}
func (mountEntType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

type mountConf struct{ b *mountBehaviour }

func (c mountConf) Apply(d *world.EntityData) { d.Data = c.b }

type mountBehaviour struct {
	inv    *inventory.Inventory
	closed int
}

func (*mountBehaviour) Tick(*entity.Ent, *world.Tx) *entity.Movement { return nil }
func (b *mountBehaviour) EntityWindow(_ *entity.Ent, _ world.Entity, layout entity.WindowLayout) (entity.EntityWindow, bool) {
	if layout != entity.WindowLayoutJava {
		return entity.EntityWindow{}, false
	}
	return entity.EntityWindow{Inventory: b.inv, ChestSlots: 3, OnClose: func() { b.closed++ },
		Equip: []entity.EquipSlot{{Accepted: []string{"saddle"}}, {Accepted: []string{"horsearmoriron"}}}}, true
}

// The Java horse screen: mount_screen_open, then the window as a mount menu
// (saddle 0, body 1, chest); shift-clicking puts a saddle in the saddle slot
// and dirt in the chest, never in the equipment slots.
func TestJavaMountScreen(t *testing.T) {
	s, st, c := itemsTestPlayer(world.GameModeSurvival)
	w := itemsTestWorld(t)
	b := &mountBehaviour{inv: inventory.New(5, nil)}
	itemsDo(t, w, func(tx *world.Tx) {
		st.tx = tx
		e := tx.AddEntity(world.EntitySpawnOpts{}.New(mountEntType{}, mountConf{b}))
		if s.entityIDs == nil {
			s.entityIDs = map[*world.EntityHandle]int32{}
		}
		s.entityIDs[e.H()] = 42
		itemsPackets(s)
		if !s.OpenEntityContainer(e, tx) {
			t.Fatal("no window")
		}
		ids := itemsPackets(s)
		if itemsCount(ids, v777.ClientboundPlayMountScreenOpen) != 1 || itemsCount(ids, v777.ClientboundPlayContainerSetContent) != 1 {
			t.Fatalf("packets %v", ids)
		}
		m := st.open.Load()
		if m == nil || m.kind != menuMount || m.size != 5 || m.slots() != 5+36 {
			t.Fatalf("menu %+v", m)
		}
		// Player inventory slot 9 is window slot size+0.
		_ = st.inv.SetItem(9, item.NewStack(item.Saddle{}, 1))
		_ = st.inv.SetItem(10, item.NewStack(block.Dirt{}, 1))
		itemsClick(s, tx, c, m.size, 0, clickQuickMove)
		itemsClick(s, tx, c, m.size+1, 0, clickQuickMove)
		if it, _ := b.inv.Item(0); it.Empty() {
			t.Error("saddle not in the saddle slot")
		}
		if it, _ := b.inv.Item(1); !it.Empty() {
			t.Errorf("body slot holds %v", it)
		}
		if it, _ := b.inv.Item(2); it.Empty() {
			t.Error("dirt not in the chest")
		}
		s.closeMenu(tx, c, true)
		if b.closed != 1 {
			t.Error("closing did not run OnClose")
		}
		if _, ok := mountSlots.Load(b.inv); ok {
			t.Error("mount slots kept after closing")
		}
	})
}
