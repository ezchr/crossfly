//go:build crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// Hoppers with redstone=dragonfly follow Bedrock Edition's rules (Hopper and HopperBlockActor in BDS):
//
//   - A hopper works on redstone ticks only (every other tick). Its cooldown counts down there; once it is over, an
//     unlocked hopper first pushes one item into the container it faces (the first of its slots whose item fits),
//     then, unless it is full, pulls one item from the container above it, or, when no container block is above it,
//     picks up the item entities in its own space and the space above. If anything moved it waits 8 ticks.
//   - A hopper that receives its first item from another hopper waits 8 ticks from then before it passes it on.
//   - A locked (powered) hopper moves nothing; its cooldown still counts down.

// dfHopperCooldown is the wait after a hopper moved items: 4 redstone ticks.
const dfHopperCooldown = 8

// dfTick is HopperBlockActor::tick with redstone=dragonfly. It reports true: the hopper ticked by these rules.
func (h Hopper) dfTick(currentTick int64, pos cube.Pos, tx *world.Tx) bool {
	if currentTick%2 != 0 || h.inventory == nil {
		return true
	}
	h.tickedAt = currentTick
	if h.TransferCooldown > 0 {
		h.TransferCooldown -= 2
	}
	if h.TransferCooldown <= 0 {
		h.TransferCooldown = 0
		if !h.Powered && h.dfMove(pos, tx) {
			h.TransferCooldown = dfHopperCooldown
		}
	}
	// Only the hopper's own fields are written back: a hopper it pushed into keeps the cooldown it got.
	tx.SetBlockEntity(pos, h)
	return true
}

// dfMove is Hopper::_tryMoveItems: push, then pull. It reports whether an item moved.
func (h *Hopper) dfMove(pos cube.Pos, tx *world.Tx) bool {
	pushed := false
	if !javaInventoryEmpty(h.inventory) {
		pushed = h.dfPush(pos, tx)
	}
	if javaInventoryFull(h.inventory) {
		return pushed
	}
	return h.dfPull(pos, tx) || pushed
}

// dfPush is Hopper::_pushOutItems: the first item of the hopper's slots that fits goes into the container it faces.
func (h *Hopper) dfPush(pos cube.Pos, tx *world.Tx) bool {
	destPos := pos.Side(h.Facing)
	dest, ok := javaContainerAt(tx, destPos)
	if !ok {
		return false
	}
	if dest.inv == nil {
		// Furnaces, brewing stands, composters, jukeboxes and pots keep their own sided rules.
		if into, ok := dest.block.(HopperInsertable); ok {
			return into.InsertItem(*h, destPos, tx)
		}
		return false
	}
	if javaInventoryFull(dest.inv) {
		return false
	}
	for slot, stack := range h.inventory.Slots() {
		if stack.Empty() {
			continue
		}
		if dfAddItem(dest, stack.Grow(1-stack.Count()), tx).Empty() {
			_ = h.inventory.SetItem(slot, stack.Grow(-1))
			return true
		}
	}
	return false
}

// dfPull is Hopper::_tryPullInItemsFromAboveContainer, and the pick-up of item entities when no container block is
// above the hopper (Hopper::_tryAddItemsFromPos).
func (h *Hopper) dfPull(pos cube.Pos, tx *world.Tx) bool {
	srcPos := pos.Side(cube.FaceUp)
	self := javaContainer{inv: h.inventory, block: *h, pos: pos, self: true}
	if src, ok := javaContainerAt(tx, srcPos); ok {
		if src.inv == nil {
			if from, ok := src.block.(HopperExtractable); ok {
				return from.ExtractItem(*h, srcPos, tx)
			}
			return false
		}
		for slot, stack := range src.inv.Slots() {
			if stack.Empty() {
				continue
			}
			if dfAddItem(self, stack.Grow(1-stack.Count()), tx).Empty() {
				_ = src.inv.SetItem(slot, stack.Grow(-1))
				return true
			}
		}
		if src.block != nil {
			return false
		}
	}
	if _, container := tx.Block(srcPos).(Container); container {
		return false
	}
	if hopperItemEntity == nil {
		return false
	}
	// The hopper's own space first, then the space above it. An item entity goes in as a whole stack, as much as fits.
	for _, at := range [2]cube.Pos{pos, srcPos} {
		box := cube.Box(0, 0, 0, 1, 1, 1).Translate(at.Vec3())
		for e := range tx.EntitiesWithin(box.Grow(0.5)) {
			stack, set, ok := hopperItemEntity(tx, e)
			if !ok || !entityIntersects(e, box) {
				continue
			}
			rest := dfAddItem(self, stack, tx)
			if rest.Count() == stack.Count() {
				continue
			}
			set(rest)
			return true
		}
	}
	return false
}

// dfAddItem is Hopper::_addItem: slot by slot in order, the stack goes into the first empty slot or merges into a
// stack of the same item. A hopper that receives its first item waits a full cooldown from now before passing it on.
// The stack left over is returned.
func dfAddItem(dest javaContainer, stack item.Stack, tx *world.Tx) item.Stack {
	if !dest.canPlace(stack) {
		return stack
	}
	wasEmpty := javaInventoryEmpty(dest.inv)
	moved := false
	for slot := 0; slot < dest.inv.Size() && !stack.Empty(); slot++ {
		current, _ := dest.inv.Item(slot)
		switch {
		case current.Empty():
			_ = dest.inv.SetItem(slot, stack)
			stack, moved = item.Stack{}, true
		case current.Comparable(stack) && current.Count() < current.MaxCount():
			n := min(stack.Count(), current.MaxCount()-current.Count())
			_ = dest.inv.SetItem(slot, current.Grow(n))
			stack, moved = stack.Grow(-n), true
		}
	}
	if moved && wasEmpty && dest.block != nil && !dest.self {
		if hopper, ok := tx.Block(dest.pos).(Hopper); ok {
			// HopperBlockActor::updateCooldownAfterMove: one redstone tick more if the hopper still ticks this tick.
			hopper.TransferCooldown = dfHopperCooldown
			if hopper.tickedAt < tx.CurrentTick() {
				hopper.TransferCooldown += 2
			}
			tx.SetBlockEntity(dest.pos, hopper)
		}
	}
	return stack
}
