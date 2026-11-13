package session

import (
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// AnvilRules are optional rules of the anvil for servers that hand out enchantments above their
// maximum level. The zero value is vanilla.
type AnvilRules struct {
	// OverMax lets a higher level of an enchantment replace one that is already at or above its
	// maximum level, and lets a level above the maximum replace any lower one. A Bedrock client
	// refuses to add an enchantment to an item that has it at its maximum level or higher
	// (ItemEnchants::canEnchant) and then shows no result to take, so while such an item lies in the
	// anvil the client is shown it without that enchantment; the result gets the higher level. The
	// levels the anvil asks for are those of adding the enchantment to an item without it. Two equal
	// levels at or above the maximum still do not combine.
	OverMax bool
	// NoPriorWork leaves the prior work penalty (the cost every earlier anvil use of an item adds)
	// out of the cost, so an item that went through the anvil often does not become too expensive.
	// Items keep counting their anvil uses.
	NoPriorWork bool
}

var anvilRules atomic.Pointer[AnvilRules]

// SetAnvilRules sets the rules of every anvil. It may be called at any time.
func SetAnvilRules(r AnvilRules) { anvilRules.Store(&r) }

// CurrentAnvilRules returns the rules set with SetAnvilRules.
func CurrentAnvilRules() AnvilRules {
	if r := anvilRules.Load(); r != nil {
		return *r
	}
	return AnvilRules{}
}

// AnvilInputs returns the input and material of an anvil as the anvil counts them under the
// AnvilRules, which is also how a Bedrock client is shown them while they lie in the anvil: with
// OverMax the input is without each enchantment the material raises that the client would refuse
// to raise, with NoPriorWork both are without their prior work penalty. Without rules they are
// returned unchanged. The stacks returned are only ever a basis for the cost and the result, never
// put into an inventory.
func AnvilInputs(input, material item.Stack) (item.Stack, item.Stack) {
	r := CurrentAnvilRules()
	if input.Empty() || (!r.OverMax && !r.NoPriorWork) {
		return input, material
	}
	if r.OverMax && !material.Empty() && anvilMerges(input, material) {
		view := input
		for _, e := range material.Enchantments() {
			t := e.Type()
			existing, ok := input.Enchantment(t)
			if !ok || existing.Level() >= e.Level() {
				// Not on the input, or not raised by the material.
				continue
			}
			if existing.Level() < t.MaxLevel() && e.Level() <= t.MaxLevel() {
				continue
			}
			if !anvilEnchantmentFits(t, input) {
				// The anvil will not apply it, so it must stay as it is.
				continue
			}
			view = view.WithoutEnchantments(t)
		}
		// A book that would lose its last enchantment turns into a plain book, which the anvil takes
		// no enchantments for.
		if _, book := input.Item().(item.EnchantedBook); !book || len(view.Enchantments()) > 0 {
			input = view
		}
	}
	if r.NoPriorWork {
		input, material = input.WithAnvilCost(0), material.WithAnvilCost(0)
	}
	return input, material
}

// anvilMerges reports whether the anvil merges the enchantments of material onto input: material is
// an enchanted book or the same kind of durable item, and not something input is repaired with.
func anvilMerges(input, material item.Stack) bool {
	if repairable, ok := input.Item().(item.Repairable); ok && repairable.RepairableBy(material) {
		return false
	}
	if _, book := material.Item().(item.EnchantedBook); book && len(material.Enchantments()) > 0 {
		return true
	}
	_, durable := input.Item().(item.Durable)
	inputName, inputMeta := input.Item().EncodeItem()
	materialName, materialMeta := material.Item().EncodeItem()
	return durable && inputName == materialName && inputMeta == materialMeta
}

// anvilEnchantmentFits reports whether the anvil applies an enchantment of type t to input: the item
// takes it and no other enchantment on it rules it out.
func anvilEnchantmentFits(t item.EnchantmentType, input item.Stack) bool {
	if _, book := input.Item().(item.EnchantedBook); !book && !t.CompatibleWithItem(input.Item()) {
		return false
	}
	for _, other := range input.Enchantments() {
		if otherType := other.Type(); otherType != t && !t.CompatibleWithEnchantment(otherType) {
			return false
		}
	}
	return true
}

// AnvilKeepsLevels reports whether result has every enchantment of input at the same level or a
// higher one. An anvil result that does not is never handed out.
func AnvilKeepsLevels(input, result item.Stack) bool {
	for _, e := range input.Enchantments() {
		if got, ok := result.Enchantment(e.Type()); !ok || got.Level() < e.Level() {
			return false
		}
	}
	return true
}

// anvilSlots puts the anvil's input and material into content, the UI inventory about to be sent,
// as AnvilInputs has them. The stack network IDs stay those of the real items, which the client's
// requests refer to.
func (s *Session) anvilSlots(content []protocol.ItemInstance) {
	if len(content) <= anvilMaterialSlot {
		return
	}
	input, _ := s.ui.Item(anvilInputSlot)
	material, _ := s.ui.Item(anvilMaterialSlot)
	viewInput, viewMaterial := AnvilInputs(input, material)

	shown := false
	defer func() { s.anvilShown.Store(shown) }()
	if !input.Empty() && !viewInput.Equal(input) {
		content[anvilInputSlot].Stack, shown = stackFromItem(s.br, viewInput), true
	}
	if !material.Empty() && !viewMaterial.Equal(material) {
		content[anvilMaterialSlot].Stack, shown = stackFromItem(s.br, viewMaterial), true
	}
	if shown {
		// The requests that follow move these items or the result made of them, of which the client
		// has its own idea: the slots they change are sent again as they really are.
		s.anvilResync.Store(anvilResyncRequests)
	}
}

// anvilResyncRequests is the number of item stack requests after the client was last shown an anvil
// item differently whose changed slots are sent again. The client sends the take of a result and
// the move that follows before it has the answer to the first.
const anvilResyncRequests = 4

// resyncAnvilSlots sends the slots an item stack request changed as the server has them, if the
// client was shown an anvil item differently shortly before: an item taken back out of the anvil
// and the result of the anvil are then what they really are wherever the client moved them.
func (s *Session) resyncAnvilSlots(changed map[byte][]byte, tx *world.Tx) {
	if s.anvilResync.Load() <= 0 {
		return
	}
	if !s.anvilShown.Load() {
		s.anvilResync.Add(-1)
	}
	var ui bool
	for container, slots := range changed {
		inv, ok := s.invByID(int32(container), tx)
		if !ok {
			continue
		}
		switch inv {
		case s.ui:
			ui = true
		case s.inv:
			for _, slot := range slots {
				it, _ := inv.Item(int(slot))
				s.sendItem(it, int(slot), protocol.WindowIDInventory)
			}
		case s.offHand:
			s.sendInv(inv, protocol.WindowIDOffHand)
		default:
			if s.armour != nil && inv == s.armour.Inventory() {
				for _, slot := range slots {
					it, _ := inv.Item(int(slot))
					s.sendItem(it, int(slot), protocol.WindowIDArmour)
				}
			}
		}
	}
	if ui {
		s.sendInv(s.ui, protocol.WindowIDUI)
	}
}
