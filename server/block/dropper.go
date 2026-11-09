//go:build crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"math/rand/v2"
)

// NewDropper creates the dropper variant of a nine-slot dispenser container. Java shares their activation,
// orientation and timing; only the handling of the selected item differs.
func NewDropper() Dispenser { d := NewDispenser(); d.Dropper = true; return d }

func (d Dispenser) dropperDispense(slot int, stack item.Stack, pos cube.Pos, tx *world.Tx, r *rand.Rand) bool {
	targetPos := pos.Side(d.Facing)
	target := tx.Block(targetPos)
	if into, ok := target.(HopperInsertable); ok {
		// A one-item temporary source shares the destination's sided insertion rules without giving it access to
		// another randomly selected source slot. It cannot consume more than the single selected item.
		source := NewHopper()
		source.Facing = d.Facing
		_ = source.inventory.SetItem(0, stack.Grow(1-stack.Count()))
		if into.InsertItem(source, targetPos, tx) {
			_ = d.inventory.SetItem(slot, stack.Grow(-1))
		}
		return true // Full or rejecting containers retain the item silently; they never cause world ejection.
	}
	if into, ok := target.(Container); ok {
		inv := into.Inventory(tx, targetPos)
		empty := true
		for _, it := range inv.Slots() {
			if !it.Empty() {
				empty = false
				break
			}
		}
		added, _ := inv.AddItem(stack.Grow(1 - stack.Count()))
		if added == 1 {
			_ = d.inventory.SetItem(slot, stack.Grow(-1))
			if h, ok := target.(Hopper); ok && empty {
				h.TransferCooldown = 8
				tx.SetBlockEntity(targetPos, h)
			}
		}
		return true
	}
	return d.dropDispensedItem(slot, stack, pos, tx, r)
}
