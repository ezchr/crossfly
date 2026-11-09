package entity

import (
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
)

// EntityWindow is the inventory window of an entity a player opens: the saddle, armour and chest
// of a horse, donkey, mule, llama, camel or nautilus. The sessions show it as the horse screen of
// their edition and move items in Inventory; the entity applies what changed.
type EntityWindow struct {
	// Inventory is the window's contents in the layout the window was asked for (WindowLayout).
	Inventory *inventory.Inventory
	// Equip are the equipment slots at the start of Inventory (Bedrock layout) with the items
	// they accept. Java windows always have a saddle and a body slot.
	Equip []EquipSlot
	// ChestSlots is the number of chest slots after the equipment.
	ChestSlots int
	// OnClose is called once when the viewer closes the window.
	OnClose func()
}

// EquipSlot is an equipment slot of an EntityWindow.
type EquipSlot struct {
	// Accepted are the item names the slot takes ("minecraft:saddle").
	Accepted []string
	// Icon is the item drawn in the empty slot.
	Icon string
	// Slot is the entity's own number of the slot (minecraft:equippable: 0 saddle, 1 armour or
	// carpet), the slotNumber of Bedrock's UpdateEquip.
	Slot int
}

// WindowLayout is the slot order an EntityWindow is built in.
type WindowLayout int

const (
	// WindowLayoutBedrock: the equipment slots of the entity (saddle, armour; a donkey only has a
	// saddle, a llama only a carpet), then the chest. Bedrock HORSE_EQUIP slot n and LEVEL_ENTITY
	// slot n are both index n.
	WindowLayoutBedrock WindowLayout = iota
	// WindowLayoutJava: saddle (0), body armour (1), then the chest (java HorseInventoryMenu).
	WindowLayoutJava
)

// EntityWindowBehaviour may be implemented by the Behaviour of an Ent with an inventory window.
type EntityWindowBehaviour interface {
	EntityWindow(e *Ent, viewer world.Entity, layout WindowLayout) (EntityWindow, bool)
}

// WindowOf returns the inventory window of e for viewer, if e has one viewer may open.
func WindowOf(e world.Entity, viewer world.Entity, layout WindowLayout) (EntityWindow, bool) {
	if ent, ok := e.(interface{ ent() *Ent }); ok {
		if b, ok := ent.ent().Behaviour().(EntityWindowBehaviour); ok {
			return b.EntityWindow(ent.ent(), viewer, layout)
		}
	}
	return EntityWindow{}, false
}

// ClientDrivenVehicle may be implemented by a Rideable whose position comes from the client of
// the rider steering it. A session does not send the vehicle's own movement back to that client.
type ClientDrivenVehicle interface {
	ClientDriven(rider *world.EntityHandle) bool
}

// ClientDrivenBehaviour may be implemented by the Behaviour of an Ent that can be a
// ClientDrivenVehicle: DrivenBy asks it directly instead of making the Rideable first. Sessions ask
// DrivenBy of every movement they are shown.
type ClientDrivenBehaviour interface {
	ClientDrivenBy(e *Ent, rider *world.EntityHandle) bool
}

// DrivenBy reports whether e is a vehicle the client of rider is moving itself.
func DrivenBy(e world.Entity, rider *world.EntityHandle) bool {
	if rider == nil {
		return false
	}
	if _, ok := e.(Rideable); !ok {
		if ent, ok := e.(interface{ ent() *Ent }); ok {
			if d, ok := ent.ent().Behaviour().(ClientDrivenBehaviour); ok {
				return d.ClientDrivenBy(ent.ent(), rider)
			}
		}
	}
	r, ok := RideableOf(e)
	if !ok {
		return false
	}
	d, ok := r.(ClientDrivenVehicle)
	return ok && d.ClientDriven(rider)
}
