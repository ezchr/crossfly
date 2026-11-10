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
	// DropperBlock.dispenseFrom: a container in front (a block, or else a chest or hopper minecart) takes the item
	// the way a hopper puts it in; a full or rejecting one keeps it in the dropper.
	if dest, ok := javaContainerAt(tx, targetPos); ok && dest.inv != nil {
		if javaAddItem(nil, dest, stack.Grow(1-stack.Count()), tx).Empty() {
			_ = d.inventory.SetItem(slot, stack.Grow(-1))
		}
		return true
	}
	return d.dropDispensedItem(slot, stack, pos, tx, r)
}
