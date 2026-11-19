package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// The owner's pickaxe (Efficiency 10, Unbreaking 3, Fortune 3) and a Fortune 4 book: in the anvil the
// pickaxe really loses Fortune, so the client offers the result; the result is Fortune 4 with the
// rest kept, for 8 levels.
func TestAnvilStripCraft(t *testing.T) {
	SetAnvilRules(AnvilRules{OverMax: true, NoPriorWork: true})
	defer SetAnvilRules(AnvilRules{})
	fortune := enchantment.Fortune
	anvilSessionTest(t, func(tx *world.Tx, s *Session, h *ItemStackRequestHandler) {
		u := &anvilUser{level: 10}
		pick := anvilPick(ench(enchantment.Efficiency, 10), ench(enchantment.Unbreaking, 3), ench(fortune, 3))
		book := anvilBook(ench(fortune, 4))
		_ = s.inv.SetItem(0, pick)
		_ = s.inv.SetItem(1, book)
		pick, _ = s.inv.Item(0)
		book, _ = s.inv.Item(1)
		request := func(id int32, actions ...protocol.StackRequestAction) {
			t.Helper()
			if err := h.handleRequest(protocol.ItemStackRequest{RequestID: id, Actions: actions}, s, tx, u); err != nil {
				t.Fatalf("request %d: %v", id, err)
			}
			s.anvilKeep(tx) // as Handle does after the requests of a packet
		}
		place := func(from, to protocol.StackRequestSlotInfo) *protocol.PlaceStackRequestAction {
			a := &protocol.PlaceStackRequestAction{}
			a.Count, a.Source, a.Destination = 1, from, to
			return a
		}
		request(-1, place(slotInfo(protocol.ContainerInventory, 0, item_id(pick)), slotInfo(protocol.ContainerAnvilInput, anvilInputSlot, 0)))
		if in, _ := s.ui.Item(anvilInputSlot); level(in, fortune) != 3 || item.AnvilHeld(in) {
			t.Fatalf("pickaxe alone changed: %v", in.Enchantments())
		}
		request(-3, place(slotInfo(protocol.ContainerInventory, 1, item_id(book)), slotInfo(protocol.ContainerAnvilMaterial, anvilMaterialSlot, 0)))
		in, _ := s.ui.Item(anvilInputSlot)
		if level(in, fortune) != 0 || level(in, enchantment.Efficiency) != 10 || !item.AnvilHeld(in) {
			t.Fatalf("pickaxe next to the book: %v held %v", in.Enchantments(), item.AnvilHeld(in))
		}
		mat, _ := s.ui.Item(anvilMaterialSlot)
		if _, cost, ok := bedrockClientAnvil(in, mat, u.level); !ok || cost != 8 {
			t.Fatalf("client offers a result: %v at cost %d", ok, cost)
		}
		request(-5,
			&protocol.CraftRecipeOptionalStackRequestAction{},
			&protocol.ConsumeStackRequestAction{},
			&protocol.ConsumeStackRequestAction{},
			place(slotInfo(protocol.ContainerCreatedOutput, craftingResult, -5), slotInfo(protocol.ContainerInventory, 5, 0)),
		)
		res, _ := s.inv.Item(5)
		if level(res, fortune) != 4 || level(res, enchantment.Efficiency) != 10 || level(res, enchantment.Unbreaking) != 3 || item.AnvilHeld(res) {
			t.Fatalf("result %v held %v", res.Enchantments(), item.AnvilHeld(res))
		}
		if u.level != 2 {
			t.Errorf("%d levels left, want 2", u.level)
		}
	})
}

// Taking the book away again gives the pickaxe its Fortune back, and so does taking the pickaxe out.
func TestAnvilStripRestore(t *testing.T) {
	SetAnvilRules(AnvilRules{OverMax: true, NoPriorWork: true})
	defer SetAnvilRules(AnvilRules{})
	fortune := enchantment.Fortune
	anvilSessionTest(t, func(tx *world.Tx, s *Session, h *ItemStackRequestHandler) {
		u := &anvilUser{level: 0}
		pick := anvilPick(ench(enchantment.Efficiency, 10), ench(fortune, 3))
		book := anvilBook(ench(fortune, 4))
		_ = s.inv.SetItem(0, pick)
		_ = s.inv.SetItem(1, book)
		pick, _ = s.inv.Item(0)
		book, _ = s.inv.Item(1)
		request := func(id int32, actions ...protocol.StackRequestAction) {
			t.Helper()
			if err := h.handleRequest(protocol.ItemStackRequest{RequestID: id, Actions: actions}, s, tx, u); err != nil {
				t.Fatalf("request %d: %v", id, err)
			}
			s.anvilKeep(tx)
		}
		place := func(from, to protocol.StackRequestSlotInfo) *protocol.PlaceStackRequestAction {
			a := &protocol.PlaceStackRequestAction{}
			a.Count, a.Source, a.Destination = 1, from, to
			return a
		}
		request(-1, place(slotInfo(protocol.ContainerInventory, 0, item_id(pick)), slotInfo(protocol.ContainerAnvilInput, anvilInputSlot, 0)))
		request(-3, place(slotInfo(protocol.ContainerInventory, 1, item_id(book)), slotInfo(protocol.ContainerAnvilMaterial, anvilMaterialSlot, 0)))
		// The book back into the inventory: the pickaxe in the anvil has Fortune 3 again.
		mat, _ := s.ui.Item(anvilMaterialSlot)
		request(-7, place(slotInfo(protocol.ContainerAnvilMaterial, anvilMaterialSlot, item_id(mat)), slotInfo(protocol.ContainerInventory, 1, 0)))
		in, _ := s.ui.Item(anvilInputSlot)
		if level(in, fortune) != 3 || item.AnvilHeld(in) {
			t.Fatalf("pickaxe after the book left: %v held %v", in.Enchantments(), item.AnvilHeld(in))
		}
		// Book in again, then the pickaxe out: it comes out with Fortune 3.
		book, _ = s.inv.Item(1)
		request(-9, place(slotInfo(protocol.ContainerInventory, 1, item_id(book)), slotInfo(protocol.ContainerAnvilMaterial, anvilMaterialSlot, 0)))
		in, _ = s.ui.Item(anvilInputSlot)
		if level(in, fortune) != 0 {
			t.Fatalf("pickaxe next to the book again: %v", in.Enchantments())
		}
		request(-11, place(slotInfo(protocol.ContainerAnvilInput, anvilInputSlot, item_id(in)), slotInfo(protocol.ContainerInventory, 0, 0)))
		out, _ := s.inv.Item(0)
		if level(out, fortune) != 3 || level(out, enchantment.Efficiency) != 10 || item.AnvilHeld(out) {
			t.Fatalf("pickaxe taken out: %v held %v", out.Enchantments(), item.AnvilHeld(out))
		}
		// Nothing duplicated: one pickaxe, one book.
		var picks, books int
		for _, inv := range []interface{ Slots() []item.Stack }{s.inv, s.ui} {
			for _, it := range inv.Slots() {
				switch it.Item().(type) {
				case item.Pickaxe:
					picks += it.Count()
				case item.EnchantedBook:
					books += it.Count()
				}
			}
		}
		if picks != 1 || books != 1 {
			t.Errorf("%d pickaxes and %d books, want 1 and 1", picks, books)
		}
	})
}

func TestRestoreAnvilHeld(t *testing.T) {
	pick := anvilPick(ench(enchantment.Efficiency, 10), ench(enchantment.Fortune, 3))
	held := item.WithAnvilHeld(pick, enchantment.Fortune)
	if level(held, enchantment.Fortune) != 0 || !item.AnvilHeld(held) {
		t.Fatal("not taken off")
	}
	// Held twice keeps the first original.
	held = item.WithAnvilHeld(held, enchantment.Efficiency)
	back := item.RestoreAnvilHeld(held)
	if level(back, enchantment.Fortune) != 3 || level(back, enchantment.Efficiency) != 10 || item.AnvilHeld(back) {
		t.Fatalf("restored %v", back.Enchantments())
	}
}
