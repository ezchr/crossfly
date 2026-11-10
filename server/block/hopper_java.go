//go:build crossfly_full

package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
)

var _ world.RedstonePowerConsumer = Hopper{}

// javaTick is Java's HopperBlockEntity.pushItemsTick: the cooldown counts down every tick; once it is over, an
// unlocked hopper pushes one item into the container it faces, then takes one in from the container above (or
// item entities over it), and waits 8 ticks if either moved.
func (h Hopper) javaTick(currentTick int64, pos cube.Pos, tx *world.Tx) {
	h.TransferCooldown--
	h.tickedAt = currentTick
	if h.TransferCooldown <= 0 {
		h.TransferCooldown = 0
		if !h.Powered && h.inventory != nil {
			changed := false
			if !javaInventoryEmpty(h.inventory) {
				changed = h.javaEject(pos, tx)
			}
			if !javaInventoryFull(h.inventory) {
				// Java's changed |= suckInItems(): sucked in even when an item went out.
				if h.javaSuck(pos, tx) {
					changed = true
				}
			}
			if changed {
				h.TransferCooldown = 8
			}
		}
	}
	// The destination of an item this hopper pushed may be a hopper that changed its own cooldown in the world: only
	// this hopper's fields are written back.
	tx.SetBlockEntity(pos, h)
}

// javaContainer is a container as Java's hoppers and droppers see it (HopperBlockEntity.getContainerAt): a block
// container, or else a container entity (a chest or hopper minecart) in the block space.
type javaContainer struct {
	inv *inventory.Inventory
	// block is the block container, nil for a minecart.
	block world.Block
	pos   cube.Pos
	// self is the ticking hopper taking items in: its cooldown is set when its tick ends.
	self bool
}

// javaContainerAt is HopperBlockEntity.getContainerAt.
func javaContainerAt(tx *world.Tx, pos cube.Pos) (javaContainer, bool) {
	b := tx.Block(pos)
	// WorldlyContainers (furnaces, brewing stands) and container holders (composters) keep their own sided rules.
	_, insertable := b.(HopperInsertable)
	_, extractable := b.(HopperExtractable)
	if insertable || extractable {
		return javaContainer{block: b, pos: pos}, true
	}
	if c, ok := b.(Container); ok {
		if inv := c.Inventory(tx, pos); inv != nil {
			return javaContainer{inv: inv, block: b, pos: pos}, true
		}
	}
	// Container entities whose box overlaps the block space; Java picks one of them at random.
	box := cube.Box(0, 0, 0, 1, 1, 1).Translate(pos.Vec3())
	var carts []*inventory.Inventory
	for e := range tx.EntitiesWithin(box.Grow(1)) {
		c, ok := e.(interface{ MinecartInventory() *inventory.Inventory })
		if !ok || c.MinecartInventory() == nil {
			continue
		}
		if !e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) {
			continue
		}
		carts = append(carts, c.MinecartInventory())
	}
	switch len(carts) {
	case 0:
		return javaContainer{}, false
	case 1:
		return javaContainer{inv: carts[0], pos: pos}, true
	}
	return javaContainer{inv: carts[rand.IntN(len(carts))], pos: pos}, true
}

// canPlace is Container.canPlaceItem: a shulker box takes no shulker boxes.
func (c javaContainer) canPlace(stack item.Stack) bool {
	if _, ok := c.block.(ShulkerBox); ok {
		if _, ok := stack.Item().(ShulkerBox); ok {
			return false
		}
	}
	return true
}

// javaEject is HopperBlockEntity.ejectItems: the first item of the hopper's slots that fits goes into the container
// it faces.
func (h Hopper) javaEject(pos cube.Pos, tx *world.Tx) bool {
	destPos := pos.Side(h.Facing)
	dest, ok := javaContainerAt(tx, destPos)
	if !ok {
		return false
	}
	if dest.inv == nil {
		// Furnaces, brewing stands, composters, jukeboxes and pots keep their own sided rules.
		if into, ok := dest.block.(HopperInsertable); ok {
			return into.InsertItem(h, destPos, tx)
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
		if rest := javaAddItem(&h, dest, stack.Grow(1-stack.Count()), tx); rest.Empty() {
			_ = h.inventory.SetItem(slot, stack.Grow(-1))
			return true
		}
	}
	return false
}

// javaSuck is HopperBlockEntity.suckInItems: one item from the first slot of the container above that has one, or
// else the item entities in the space above the hopper, unless a full block covers it.
func (h *Hopper) javaSuck(pos cube.Pos, tx *world.Tx) bool {
	srcPos := pos.Side(cube.FaceUp)
	src, ok := javaContainerAt(tx, srcPos)
	if ok {
		if src.inv == nil {
			if from, ok := src.block.(HopperExtractable); ok {
				return from.ExtractItem(*h, srcPos, tx)
			}
			return false
		}
		self := javaContainer{inv: h.inventory, block: *h, pos: pos, self: true}
		for slot, stack := range src.inv.Slots() {
			if stack.Empty() {
				continue
			}
			if rest := javaAddItem(nil, self, stack.Grow(1-stack.Count()), tx); rest.Empty() {
				_ = src.inv.SetItem(slot, stack.Grow(-1))
				return true
			}
		}
		return false
	}
	if javaFullCollisionBlock(tx, srcPos) {
		return false
	}
	if hopperItemEntity == nil {
		return false
	}
	// Hopper.getSuckAabb: the hopper's bowl and the block above it.
	suck := cube.Box(0, 11.0/16.0, 0, 1, 2, 1).Translate(pos.Vec3())
	self := javaContainer{inv: h.inventory, block: *h, pos: pos, self: true}
	for e := range tx.EntitiesWithin(suck.Grow(0.5)) {
		stack, set, ok := hopperItemEntity(tx, e)
		if !ok || !e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(suck) {
			continue
		}
		rest := javaAddItem(nil, self, stack, tx)
		if rest.Empty() {
			set(item.Stack{})
			return true
		}
		set(rest)
	}
	return false
}

// javaAddItem is HopperBlockEntity.addItem/tryMoveInItem for plain containers: slot by slot in order, the stack goes
// into the first empty slot or merges into a stack of the same item. A hopper that was empty and receives an item
// waits 8 ticks, one less if it already ticked this tick and the item came from a hopper ticking after it. The
// stack left over is returned.
func javaAddItem(from *Hopper, dest javaContainer, stack item.Stack, tx *world.Tx) item.Stack {
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
		case current.Count() <= current.MaxCount() && current.Comparable(stack):
			n := min(stack.Count(), stack.MaxCount()-current.Count())
			if n > 0 {
				_ = dest.inv.SetItem(slot, current.Grow(n))
				stack, moved = stack.Grow(-n), true
			}
		}
	}
	if moved && wasEmpty && dest.block != nil && !dest.self {
		if hopper, ok := tx.Block(dest.pos).(Hopper); ok && hopper.TransferCooldown <= 8 {
			skip := int64(0)
			if from != nil && hopper.tickedAt >= from.tickedAt {
				skip = 1
			}
			hopper.TransferCooldown = 8 - skip
			tx.SetBlockEntity(dest.pos, hopper)
		}
	}
	return stack
}

// javaInventoryEmpty is Container.isEmpty.
func javaInventoryEmpty(inv *inventory.Inventory) bool {
	for _, s := range inv.Slots() {
		if !s.Empty() {
			return false
		}
	}
	return true
}

// javaInventoryFull is HopperBlockEntity.inventoryFull / isFullContainer: every slot holds a full stack.
func javaInventoryFull(inv *inventory.Inventory) bool {
	for _, s := range inv.Slots() {
		if s.Empty() || s.Count() < s.MaxCount() {
			return false
		}
	}
	return true
}

// javaFullCollisionBlock is BlockState.isCollisionShapeFullBlock: the block's collision is one full cube.
func javaFullCollisionBlock(tx *world.Tx, pos cube.Pos) bool {
	boxes := tx.Block(pos).Model().BBox(pos, tx)
	return len(boxes) == 1 && boxes[0] == cube.Box(0, 0, 0, 1, 1, 1)
}
