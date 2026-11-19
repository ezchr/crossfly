package session

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// The anvil, with AnvilRules.OverMax, for a Bedrock player: an item in the anvil next to a book
// raising an enchantment the client refuses to raise (it has it at its maximum level or above) has
// that enchantment really taken off, kept in the item (item.WithAnvilHeld), so the client offers
// the result. The result is made of the item as it was; the item gets the enchantment back as soon
// as the book is taken away or the item leaves the anvil (item.RestoreAnvilHeld, here, in
// Player.MoveItemsToInventory and when dropped). Showing the client the item without it
// (anvilSlots) is not enough: a client keeps the anvil input it predicted (owner's test,
// 2026-11-19: the tooltip kept Fortune 3), while a real change, under a new stack network ID, it
// takes.

// anvilRefusedTypes returns the enchantments of material that the anvil raises on input but a
// Bedrock client refuses to (ItemEnchants::canEnchant: input has it at its maximum level or above).
func anvilRefusedTypes(input, material item.Stack) []item.EnchantmentType {
	if input.Empty() || material.Empty() || !anvilMerges(input, material) {
		return nil
	}
	var out []item.EnchantmentType
	for _, e := range material.Enchantments() {
		t := e.Type()
		existing, ok := input.Enchantment(t)
		if !ok || existing.Level() >= e.Level() {
			continue
		}
		if existing.Level() < t.MaxLevel() && e.Level() <= t.MaxLevel() {
			continue
		}
		if !anvilEnchantmentFits(t, input) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// anvilKeep puts the anvil input in the state it should be in after an item stack request or a
// close: without the enchantments the material raises past the client's limit while both lie in an
// open anvil, as it was otherwise. Items that left the anvil with enchantments taken off get them
// back.
func (s *Session) anvilKeep(tx *world.Tx) {
	anvilOpen := false
	if s.containerOpened.Load() {
		if pos := s.openedPos.Load(); pos != nil {
			_, anvilOpen = tx.Block(*pos).(block.Anvil)
		}
	}
	if anvilOpen {
		in, _ := s.ui.Item(anvilInputSlot)
		if !in.Empty() {
			want := item.RestoreAnvilHeld(in)
			if CurrentAnvilRules().OverMax {
				mat, _ := s.ui.Item(anvilMaterialSlot)
				if types := anvilRefusedTypes(want, mat); len(types) > 0 {
					want = item.WithAnvilHeld(want, types...)
				}
			}
			if !want.Equal(in) || item.AnvilHeld(want) != item.AnvilHeld(in) {
				want = want.Grow(0) // a new stack network ID: the client takes the new item
				_ = s.ui.SetItem(anvilInputSlot, want)
				s.writePacket(&packet.InventorySlot{
					WindowID:  protocol.WindowIDUI,
					Slot:      anvilInputSlot,
					Container: protocol.Option(protocol.FullContainerName{ContainerID: protocol.ContainerAnvilInput}),
					NewItem:   instanceFromItem(s.br, want),
				})
			}
		}
	}
	restore := func(inv *inventory.Inventory, skip int) {
		if inv == nil {
			return
		}
		for i, it := range inv.Slots() {
			if i == skip || !item.AnvilHeld(it) {
				continue
			}
			_ = inv.SetItem(i, item.RestoreAnvilHeld(it).Grow(0))
		}
	}
	skip := -1
	if anvilOpen {
		skip = anvilInputSlot
	}
	restore(s.ui, skip)
	restore(s.inv, -1)
	restore(s.offHand, -1)
}
