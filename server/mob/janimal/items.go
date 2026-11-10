//go:build crossfly_full

package janimal

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

type gameModer interface{ GameMode() world.GameMode }
type inventoryHolder interface{ Inventory() *inventory.Inventory }

// Creative is Player.hasInfiniteMaterials.
func Creative(user item.User) bool {
	g, ok := user.(gameModer)
	return ok && g.GameMode().CreativeInventory()
}

// entityUUID is the UUID of the interacting entity (the love cause).
func entityUUID(user item.User) uuid.UUID {
	if e, ok := user.(world.Entity); ok {
		return e.H().UUID()
	}
	return uuid.Nil
}

// usePlayerItem is Mob.usePlayerItem: one item used, unless the player is in creative.
func usePlayerItem(user item.User, held item.Stack) item.Stack {
	if Creative(user) {
		return held
	}
	return held.Grow(-1)
}

// FilledResult is ItemUtils.createFilledResult(held, player, result, limitCreativeStackSize=true): one item of
// held becomes result. In creative the held item stays and result is added to the inventory if it holds
// none; otherwise the last item turns into result, or result goes to the inventory (dropped when full). It
// returns the stack the hand holds afterwards.
func FilledResult(user item.User, held, result item.Stack) item.Stack {
	inv := userInventory(user)
	if Creative(user) {
		if inv != nil && !inventoryHas(inv, result) {
			_, _ = inv.AddItem(result)
		}
		return held
	}
	held = held.Grow(-1)
	if held.Empty() {
		return result
	}
	if inv == nil {
		return held
	}
	if n, _ := inv.AddItem(result); n < result.Count() {
		dropFor(user, result.Grow(n-result.Count()))
	}
	return held
}

func userInventory(user item.User) *inventory.Inventory {
	if h, ok := user.(inventoryHolder); ok {
		return h.Inventory()
	}
	return nil
}

func inventoryHas(inv *inventory.Inventory, s item.Stack) bool {
	for _, it := range inv.Items() {
		if it.Comparable(s) {
			return true
		}
	}
	return false
}

// dropFor drops s at the user (Player.drop).
func dropFor(user item.User, s item.Stack) {
	if d, ok := user.(interface{ Drop(item.Stack) int }); ok {
		d.Drop(s)
	}
}
