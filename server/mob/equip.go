//go:build crossfly_full

package mob

import (
	"github.com/df-mc/dragonfly/server/item"
)

// Equipment slots and inventories of mobs (minecraft:equippable,
// minecraft:inventory) as other packages see them: the interact component
// equips into a slot (equip_item_slot) and drops one (drop_item_slot), the
// has_equipment filter reads the inventory.

// SlotEquipper is implemented by a component that keeps equipment slots
// (minecraft:equippable). slot is the interact component's
// equip_item_slot/drop_item_slot: an equippable slot number ("0") or a slot
// name ("slot.armor.body").
type SlotEquipper interface {
	EquipSlot(m *Mob, slot string, s item.Stack) bool
	DropSlot(m *Mob, slot string) (item.Stack, bool)
}

// GearRoller is implemented by a component that equips the mob with its
// spawn gear once (minecraft:equipment rolls its loot table on its first
// tick). RollGear rolls it now if that has not happened yet: a
// transformation does so before it puts the preserved items on.
type GearRoller interface {
	RollGear(m *Mob)
}

// InventoryHolder is implemented by a component holding items of the mob's
// container (equippable slots, minecraft:inventory).
type InventoryHolder interface {
	InventoryItems(m *Mob) []item.Stack
}

// EquipSlot puts one item of s into slot. A component (equippable) decides
// first; the slot names slot.armor.body and slot.weapon.mainhand fall back
// to the body armour and the main hand when they are empty.
func (m *Mob) EquipSlot(slot string, s item.Stack) bool {
	if s.Empty() {
		return false
	}
	s = s.Grow(1 - s.Count())
	for _, c := range m.s.comps {
		if e, ok := c.comp.(SlotEquipper); ok && e.EquipSlot(m, slot, s) {
			return true
		}
	}
	switch slot {
	case "slot.armor.body":
		if m.s.bodyArmour.Empty() {
			m.SetBodyArmour(s)
			return true
		}
	case "slot.weapon.mainhand":
		if m.s.mainHand.Empty() {
			m.SetHeldItems(s, m.s.offHand)
			return true
		}
	}
	return false
}

// DropSlot empties slot and returns what was in it.
func (m *Mob) DropSlot(slot string) (item.Stack, bool) {
	for _, c := range m.s.comps {
		if e, ok := c.comp.(SlotEquipper); ok {
			if s, ok := e.DropSlot(m, slot); ok {
				return s, true
			}
		}
	}
	switch slot {
	case "slot.armor.body":
		if s := m.s.bodyArmour; !s.Empty() {
			m.SetBodyArmour(item.Stack{})
			return s, true
		}
	case "slot.weapon.mainhand":
		if s := m.s.mainHand; !s.Empty() {
			m.SetHeldItems(item.Stack{}, m.s.offHand)
			return s, true
		}
	}
	return item.Stack{}, false
}

// InventoryItems returns the non-empty items of the mob's container
// (filter has_equipment with domain inventory).
func (m *Mob) InventoryItems() []item.Stack {
	var out []item.Stack
	for _, c := range m.s.comps {
		if h, ok := c.comp.(InventoryHolder); ok {
			for _, s := range h.InventoryItems(m) {
				if !s.Empty() {
					out = append(out, s)
				}
			}
		}
	}
	return out
}
