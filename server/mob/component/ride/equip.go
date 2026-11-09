//go:build crossfly_full

package ride

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// ---- minecraft:equippable ----

// Equippable is minecraft:equippable (BDS EquippableComponent): numbered
// slots taking accepted_items, saddle in slot 0 and armour or carpet in slot
// 1. The interact component equips into a slot (equip_item_slot) and drops
// from one (drop_item_slot), and the inventory window moves items in and
// out; on_equip/on_unequip fire (the saddled groups) and slot 1 is shown as
// body armour. The items are saved and dropped on death.
type Equippable struct {
	slots []equipSlot
	items map[int]item.Stack
	// applied is what the events and visuals were last applied for: the
	// inventory window changes items outside the mob's transaction, Tick
	// applies the difference.
	applied map[int]item.Stack
}

type equipSlot struct {
	slot      int
	accepted  []core.ItemDescriptor
	names     []string
	icon      string
	onEquip   *core.Trigger
	onUnequip *core.Trigger
}

func newEquippable(key string, raw json.RawMessage) (mob.Component, error) {
	v := struct {
		Slots []struct {
			Slot      int             `json:"slot"`
			Item      string          `json:"item"`
			Accepted  json.RawMessage `json:"accepted_items"`
			OnEquip   *core.Trigger   `json:"on_equip"`
			OnUnequip *core.Trigger   `json:"on_unequip"`
		} `json:"slots"`
	}{}
	if err := decode(raw, &v); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	e := &Equippable{items: map[int]item.Stack{}, applied: map[int]item.Stack{}}
	for _, s := range v.Slots {
		acc, err := core.ParseItemList(s.Accepted)
		if err != nil {
			return nil, fmt.Errorf("%v: accepted_items: %w", key, err)
		}
		var names []string
		_ = json.Unmarshal(s.Accepted, &names)
		e.slots = append(e.slots, equipSlot{slot: s.Slot, accepted: acc, names: names, icon: s.Item, onEquip: s.OnEquip, onUnequip: s.OnUnequip})
	}
	return e, nil
}

// Attach ...
func (e *Equippable) Attach(*mob.Mob) {}

// Detach ...
func (e *Equippable) Detach(*mob.Mob) {}

// Item returns what slot holds.
func (e *Equippable) Item(slot int) item.Stack { return e.items[slot] }

// accepts returns the slot definition taking s, preferring slot.
func (e *Equippable) accepts(slot int, s item.Stack) (equipSlot, bool) {
	for _, d := range e.slots {
		if d.slot == slot && d.takes(s) {
			return d, true
		}
	}
	for _, d := range e.slots {
		if d.takes(s) {
			return d, true
		}
	}
	return equipSlot{}, false
}

// def returns the definition of slot.
func (e *Equippable) def(slot int) (equipSlot, bool) {
	i := slices.IndexFunc(e.slots, func(d equipSlot) bool { return d.slot == slot })
	if i < 0 {
		return equipSlot{}, false
	}
	return e.slots[i], true
}

// EquipSlot implements mob.SlotEquipper.
func (e *Equippable) EquipSlot(m *mob.Mob, slot string, s item.Stack) bool {
	n, err := strconv.Atoi(slot)
	if err != nil {
		if slot != "slot.armor.body" {
			return false
		}
		n = 1
	}
	d, ok := e.accepts(n, s)
	if !ok || !e.items[d.slot].Empty() {
		return false
	}
	e.Set(m, d.slot, s)
	return true
}

// DropSlot implements mob.SlotEquipper.
func (e *Equippable) DropSlot(m *mob.Mob, slot string) (item.Stack, bool) {
	n, err := strconv.Atoi(slot)
	if err != nil {
		if slot != "slot.armor.body" {
			return item.Stack{}, false
		}
		n = 1
	}
	s := e.items[n]
	if s.Empty() {
		return item.Stack{}, false
	}
	e.Set(m, n, item.Stack{})
	return s, true
}

// Set puts s (empty to clear) into slot and applies it at once.
func (e *Equippable) Set(m *mob.Mob, slot int, s item.Stack) {
	e.put(slot, s)
	e.apply(m)
}

// put changes a slot without applying it (window changes, outside the
// mob's transaction).
func (e *Equippable) put(slot int, s item.Stack) {
	if s.Empty() {
		delete(e.items, slot)
	} else {
		e.items[slot] = s
	}
}

// Tick applies changes the inventory window made.
func (e *Equippable) Tick(m *mob.Mob) { e.apply(m) }

// apply fires on_equip/on_unequip and shows slot 1 for every slot whose item
// changed since the last apply. An item a slot does not accept (only a
// cheating client puts one there) is dropped.
func (e *Equippable) apply(m *mob.Mob) {
	for _, d := range e.slots {
		now, old := e.items[d.slot], e.applied[d.slot]
		if !now.Empty() && !d.takes(now) {
			delete(e.items, d.slot)
			core.DropItems(m, []item.Stack{now}, 0.5)
			now = item.Stack{}
		}
		if now.Empty() == old.Empty() && (now.Empty() || now.Comparable(old) && now.Count() == old.Count()) {
			continue
		}
		if now.Empty() {
			delete(e.applied, d.slot)
		} else {
			e.applied[d.slot] = now
		}
		if d.slot == 1 {
			m.SetBodyArmour(now)
		}
		switch {
		case !now.Empty() && old.Empty():
			if d.slot == 0 {
				core.PlayMobSound(m, "saddle")
			} else {
				core.PlayMobSound(m, "armor")
			}
			if d.onEquip != nil {
				d.onEquip.Fire(m, nil)
			}
		case now.Empty() && !old.Empty():
			if d.onUnequip != nil {
				d.onUnequip.Fire(m, nil)
			}
		}
	}
}

// InventoryItems implements mob.InventoryHolder: the equipped items are the
// first slots of the mob's container.
func (e *Equippable) InventoryItems(*mob.Mob) []item.Stack {
	out := make([]item.Stack, 0, len(e.items))
	for _, d := range e.slots {
		out = append(out, e.items[d.slot])
	}
	return out
}

// OnDeath drops the equipment.
func (e *Equippable) OnDeath(m *mob.Mob, _ world.DamageSource) {
	var drops []item.Stack
	for slot, s := range e.items {
		drops = append(drops, s)
		delete(e.items, slot)
	}
	core.DropItems(m, drops, 0.5)
}

// EncodeNBT ...
func (e *Equippable) EncodeNBT() map[string]any {
	if len(e.items) == 0 {
		return nil
	}
	out := map[string]any{}
	for slot, s := range e.items {
		out[strconv.Itoa(slot)] = item.WriteNBT(s, true)
	}
	return map[string]any{"Slots": out}
}

// DecodeNBT restores the items; their events ran before the save (the
// saddled group is saved with the definition), so they count as applied.
func (e *Equippable) DecodeNBT(m *mob.Mob, d map[string]any) {
	slots, _ := d["Slots"].(map[string]any)
	for k, v := range slots {
		n, err := strconv.Atoi(k)
		tag, ok := v.(map[string]any)
		if err != nil || !ok {
			continue
		}
		if s := item.ReadNBT(tag, nil); !s.Empty() {
			e.items[n], e.applied[n] = s, s
		}
	}
}

// ---- minecraft:inventory ----

// Inventory is minecraft:inventory (BDS ContainerComponent): the mob's
// container. Its saddle and armour slots are minecraft:equippable; the
// slots here are the chest of a chested donkey, mule or llama: the
// inventory_size less the equipment slots, or additional_slots_per_strength
// per point of minecraft:strength (llamas), and empty unless the mob is
// chested. The owner of a tamed mob opens it by using the mob while sneaking
// (or when it may not ride it) or with the inventory key while riding
// (Actor::getInteraction, Horse::openContainerComponent). Items are saved
// and dropped on death, with the chest itself.
type Inventory struct {
	containerType string
	size          int
	perStrength   int
	private       bool
	restrictOwner bool
	inv           *inventory.Inventory
	saved         []any
}

func newInventory(key string, raw json.RawMessage) (mob.Component, error) {
	v := struct {
		Type          string `json:"container_type"`
		Size          *int   `json:"inventory_size"`
		PerStrength   int    `json:"additional_slots_per_strength"`
		Private       bool   `json:"private"`
		RestrictOwner bool   `json:"restrict_to_owner"`
	}{}
	if err := decode(raw, &v); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	c := &Inventory{containerType: v.Type, size: 5, perStrength: v.PerStrength, private: v.Private, restrictOwner: v.RestrictOwner}
	if v.Size != nil {
		c.size = *v.Size
	}
	return c, nil
}

// Attach ...
func (c *Inventory) Attach(m *mob.Mob) {}

// Detach keeps the items for a JSON change that rebuilds the component.
func (c *Inventory) Detach(*mob.Mob) {
	if c.inv != nil {
		c.saved = nbtconv.InvToNBT(c.inv)
	}
}

// capacity is the number of chest slots the mob has when chested.
func (c *Inventory) capacity(m *mob.Mob) int {
	if c.containerType != "horse" {
		return c.size
	}
	if c.perStrength > 0 {
		return c.perStrength * strength(m)
	}
	n := 0
	if e, ok := mob.ComponentOf[*Equippable](m); ok {
		n = len(e.slots)
	}
	return max(c.size-n, 0)
}

// ensure makes the container (the size needs the other components).
func (c *Inventory) ensure(m *mob.Mob) *inventory.Inventory {
	if c.inv == nil {
		c.inv = inventory.New(max(c.capacity(m), 1), nil)
		if c.saved != nil {
			nbtconv.InvFromNBT(c.inv, c.saved)
			c.saved = nil
		}
	}
	return c.inv
}

// Inventory returns the container.
func (c *Inventory) Inventory(m *mob.Mob) *inventory.Inventory { return c.ensure(m) }

// ContainerType is container_type ("horse", "container", ...).
func (c *Inventory) ContainerType() string { return c.containerType }

// chestSlots is the number of chest slots shown now.
func (c *Inventory) chestSlots(m *mob.Mob) int {
	if c.containerType == "horse" && !m.Meta().Chested {
		return 0
	}
	return min(c.capacity(m), c.ensure(m).Size())
}

// InventoryItems implements mob.InventoryHolder.
func (c *Inventory) InventoryItems(m *mob.Mob) []item.Stack {
	if c.inv == nil && c.saved == nil {
		return nil
	}
	return c.ensure(m).Items()
}

// mayOpen reports whether viewer may open the window (ContainerComponent::
// canOpenContainer): a horse-type container of a tamed mob, not a private
// one, the owner's with restrict_to_owner.
func (c *Inventory) mayOpen(m *mob.Mob, viewer world.Entity) bool {
	if c.private || c.containerType != "horse" {
		return false
	}
	if !m.Meta().Tamed && !m.HasComponent("minecraft:is_tamed") {
		return false
	}
	if c.restrictOwner {
		id, ok := m.OwnerUUID()
		return ok && id == viewer.H().UUID()
	}
	return true
}

// Window implements mob.WindowProvider: the equipment then the chest. A
// Bedrock window has the mob's own equipment slots; a Java one always a
// saddle (0) and a body (1) slot. Window changes go straight into the
// equippable and the chest; Equippable.Tick applies equipment changes.
func (c *Inventory) Window(m *mob.Mob, viewer world.Entity, layout entity.WindowLayout) (entity.EntityWindow, bool) {
	if !c.mayOpen(m, viewer) {
		return entity.EntityWindow{}, false
	}
	eq, _ := mob.ComponentOf[*Equippable](m)
	// order maps window indexes before the chest to equippable slots (-1:
	// a Java slot the mob does not have).
	var order []int
	var equip []entity.EquipSlot
	switch layout {
	case entity.WindowLayoutJava:
		order = []int{-1, -1}
		if eq != nil {
			for i := range order {
				if _, ok := eq.def(i); ok {
					order[i] = i
				}
			}
		}
	default:
		if eq != nil {
			for _, d := range eq.slots {
				order = append(order, d.slot)
			}
		}
	}
	for _, slot := range order {
		var es entity.EquipSlot
		if eq != nil {
			if d, ok := eq.def(slot); ok {
				es = entity.EquipSlot{Accepted: d.names, Icon: d.icon, Slot: d.slot}
			}
		}
		equip = append(equip, es)
	}
	chest := c.chestSlots(m)
	chestInv := c.ensure(m)
	win := inventory.New(max(len(order)+chest, 1), nil)
	for i, slot := range order {
		if eq != nil && slot >= 0 {
			_ = win.SetItem(i, eq.items[slot])
		}
	}
	for i := 0; i < chest; i++ {
		it, _ := chestInv.Item(i)
		_ = win.SetItem(len(order)+i, it)
	}
	win.SlotFunc(func(i int, _, after item.Stack) {
		switch {
		case i < len(order):
			if eq != nil && order[i] >= 0 {
				eq.put(order[i], after)
			}
		case i-len(order) < chest:
			_ = chestInv.SetItem(i-len(order), after)
		}
	})
	return entity.EntityWindow{Inventory: win, Equip: equip, ChestSlots: chest}, true
}

// InteractLate: the window opens when nothing else takes the interaction.
func (c *Inventory) InteractLate() {}

// Interact opens the window for a player using the mob while sneaking, or
// when it may not get on it.
func (c *Inventory) Interact(m *mob.Mob, user item.User, held item.Stack, _ mgl64.Vec3) (item.Stack, bool) {
	p, ok := user.(*player.Player)
	if !ok || !c.mayOpen(m, p) {
		return held, false
	}
	if r, ok := mob.ComponentOf[*Rideable](m); ok && !p.Sneaking() && r.CanAddRider(m, p) {
		return held, false
	}
	return held, p.OpenEntityContainer(m)
}

// InteractText implements mob.InteractTexter.
func (c *Inventory) InteractText(m *mob.Mob, user world.Entity) string {
	p, ok := user.(*player.Player)
	if !ok || !c.mayOpen(m, p) {
		return ""
	}
	if r, ok := mob.ComponentOf[*Rideable](m); ok && !p.Sneaking() && r.CanAddRider(m, p) {
		return ""
	}
	return "action.interact.opencontainer"
}

// OnDeath drops the container and the chest of a chested mob.
func (c *Inventory) OnDeath(m *mob.Mob, _ world.DamageSource) {
	if c.inv == nil && c.saved == nil {
		return
	}
	drops := c.ensure(m).Clear()
	if m.Meta().Chested {
		drops = append(drops, item.NewStack(block.NewChest(), 1))
	}
	core.DropItems(m, drops, 0.5)
}

// EncodeNBT ...
func (c *Inventory) EncodeNBT() map[string]any {
	if c.inv == nil {
		if len(c.saved) == 0 {
			return nil
		}
		return map[string]any{"Items": c.saved}
	}
	items := nbtconv.InvToNBT(c.inv)
	if len(items) == 0 {
		return nil
	}
	return map[string]any{"Items": items}
}

// DecodeNBT ...
func (c *Inventory) DecodeNBT(_ *mob.Mob, d map[string]any) {
	items := nbtconv.Slice(d, "Items")
	if c.inv != nil {
		nbtconv.InvFromNBT(c.inv, items)
		return
	}
	c.saved = items
}

// strength is the minecraft:strength value (llama).
func strength(m *mob.Mob) int {
	raw, ok := m.ComponentJSON("minecraft:strength")
	if !ok {
		return 0
	}
	var v struct {
		Value int `json:"value"`
	}
	_ = json.Unmarshal(raw, &v)
	return v.Value
}

// takes reports whether the slot accepts s: an accepted_items descriptor, or
// one of the legacy names of the entity files Dragonfly encodes otherwise
// (horsearmoriron, carpet).
func (d equipSlot) takes(s item.Stack) bool {
	if s.Empty() {
		return false
	}
	if core.MatchesAny(d.accepted, s) {
		return true
	}
	name, _ := s.Item().EncodeItem()
	for _, a := range d.names {
		if f, ok := legacyItems[strings.TrimPrefix(a, "minecraft:")]; ok && f(name) {
			return true
		}
	}
	return false
}

// legacyItems maps legacy item names of the entity files to the names
// Dragonfly encodes.
var legacyItems = map[string]func(name string) bool{
	"horsearmorleather": func(n string) bool { return n == "minecraft:leather_horse_armor" },
	"horsearmoriron":    func(n string) bool { return n == "minecraft:iron_horse_armor" },
	"horsearmorgold":    func(n string) bool { return n == "minecraft:golden_horse_armor" },
	"horsearmordiamond": func(n string) bool { return n == "minecraft:diamond_horse_armor" },
	"carpet":            func(n string) bool { return strings.HasSuffix(n, "_carpet") && n != "minecraft:moss_carpet" },
}
