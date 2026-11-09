package javasession

import (
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/java/protocol/version"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
)

// Entity inventory windows (entity.WindowOf): the horse screen (mount_screen_open with the
// entity id and the chest columns; java HorseInventoryMenu: saddle 0, body armour 1, then the
// chest). The window is a virtual menu of kind menuMount on the entity window's inventory; its
// two equipment slots take only what the entity accepts (mountAccepts).

// mountSlots holds what the equipment slots of each open mount window accept.
var mountSlots sync.Map // *inventory.Inventory -> [2][]string

// OpenEntityContainer opens the inventory window of e for the player, if it has one the player
// may open, and reports whether it did (player.OpenEntityContainer).
func (s *Session) OpenEntityContainer(e world.Entity, tx *world.Tx) bool {
	st := s.items()
	_, c := st.current()
	if c == nil || st.inv == nil {
		return false
	}
	eid, ok := s.entityID(e)
	if !ok {
		return false
	}
	w, ok := entity.WindowOf(e, c, entity.WindowLayoutJava)
	if !ok || w.Inventory == nil {
		return false
	}
	s.closeMenu(tx, c, true)
	var accept [2][]string
	for i := 0; i < len(w.Equip) && i < 2; i++ {
		accept[i] = w.Equip[i].Accepted
	}
	mountSlots.Store(w.Inventory, accept)
	inv := w.Inventory
	onClose := w.OnClose
	m := &menu{
		kind: menuMount, size: 2 + w.ChestSlots, inv: inv, virtual: true, sel: -1, w: tx.World(),
		onClose: func() {
			mountSlots.Delete(inv)
			if onClose != nil {
				onClose()
			}
		},
	}
	m.id = st.nextWindowID()
	st.drag = dragState{}
	st.open.Store(m)

	pk := s.packet()
	pk.VarInt(m.id)
	pk.VarInt(int32(w.ChestSlots / 3)) // inventory columns
	pk.Int32(eid)
	s.queue(v777.ClientboundPlayMountScreenOpen, pk)
	s.syncWindow(tx, c, m)
	m.shown = inv.Slots()
	return true
}

// mountAccepts reports whether equipment slot idx (0 saddle, 1 body) of a mount window takes it.
func mountAccepts(inv *inventory.Inventory, idx int, it item.Stack) bool {
	v, ok := mountSlots.Load(inv)
	if !ok || idx < 0 || idx > 1 || it.Count() > 1 {
		return false
	}
	name, _ := it.Item().EncodeItem()
	for _, a := range v.([2][]string)[idx] {
		if !strings.Contains(a, ":") {
			a = "minecraft:" + a
		}
		if a == name || legacyMountItem(a, name) {
			return true
		}
	}
	return false
}

// legacyMountItem matches the legacy names of the entity files (horsearmoriron, carpet) with the
// item names Dragonfly encodes.
func legacyMountItem(legacy, name string) bool {
	switch legacy {
	case "minecraft:horsearmorleather":
		return name == "minecraft:leather_horse_armor"
	case "minecraft:horsearmoriron":
		return name == "minecraft:iron_horse_armor"
	case "minecraft:horsearmorgold":
		return name == "minecraft:golden_horse_armor"
	case "minecraft:horsearmordiamond":
		return name == "minecraft:diamond_horse_armor"
	case "minecraft:carpet":
		return strings.HasSuffix(name, "_carpet") || name == "minecraft:carpet"
	}
	return false
}

// quickRouteMount is HorseInventoryMenu.quickMoveStack: out of the horse into the player's
// inventory; in, to the body slot, then the saddle slot, then the chest.
func (v *view) quickRouteMount(js int, it *item.Stack) bool {
	m := v.m
	if js < m.size {
		return v.moveStack(it, m.size, m.slots(), true)
	}
	for _, a := range [...]int{1, 0} {
		if v.refs[a].mayPlace(*it) && v.slots[a].Empty() {
			return v.moveStack(it, a, a+1, false)
		}
	}
	if m.size > 2 {
		return v.moveStack(it, 2, m.size, false)
	}
	return false
}

// openVehicleInventory opens the window of the vehicle c rides (the inventory key while riding:
// the player command open_inventory).
func (s *Session) openVehicleInventory(tx *world.Tx, c session.Controllable) {
	if rideable, ok := c.RidingEntity(tx); ok {
		s.OpenEntityContainer(rideable, tx)
	}
}

// attributeJumpStrength is minecraft:jump_strength in the minecraft:attribute registry.
var attributeJumpStrength = v777.BuiltinID("minecraft:attribute", "minecraft:jump_strength")

// sendVehicleAttributes sends the movement speed and jump strength of the vehicle the player got
// on, which the client moves itself (a saddled horse), so its prediction runs at the server's
// speed.
func (s *Session) sendVehicleAttributes(vehicle world.Entity) {
	id, ok := s.entityID(vehicle)
	if !ok {
		return
	}
	type attr struct {
		id    int32
		value float64
	}
	var attrs []attr
	if sp, ok := mobAs[interface{ Speed() float64 }](vehicle); ok {
		attrs = append(attrs, attr{attributeMovementSpeed, sp.Speed()})
	}
	if j, ok := mobAs[interface{ JumpStrength() float64 }](vehicle); ok && j.JumpStrength() > 0 {
		attrs = append(attrs, attr{attributeJumpStrength, j.JumpStrength()})
	}
	if l := s.legacy(); l != nil {
		kept := attrs[:0]
		for _, a := range attrs {
			if a.id = version.Map(l.attribute, a.id); a.id >= 0 {
				kept = append(kept, a)
			}
		}
		attrs = kept
	}
	if len(attrs) == 0 {
		return
	}
	w := s.packet()
	w.VarInt(id)
	w.VarInt(int32(len(attrs)))
	for _, a := range attrs {
		w.VarInt(a.id)
		w.Float64(a.value)
		w.VarInt(0) // modifiers
	}
	s.queue(v777.ClientboundPlayUpdateAttributes, w)
}

// drivenBySelf reports whether e is a vehicle this client drives (move_vehicle): its own
// movement is not sent back.
func (s *Session) drivenBySelf(e world.Entity) bool { return entity.DrivenBy(e, s.ent) }
