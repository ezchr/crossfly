package session

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func ench(t item.EnchantmentType, lvl int) item.Enchantment { return item.NewEnchantment(t, lvl) }

func anvilSword(e ...item.Enchantment) item.Stack {
	return item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1).WithEnchantments(e...)
}

func anvilPick(e ...item.Enchantment) item.Stack {
	return item.NewStack(item.Pickaxe{Tier: item.ToolTierDiamond}, 1).WithEnchantments(e...)
}

func anvilBook(e ...item.Enchantment) item.Stack {
	return item.NewStack(item.EnchantedBook{}, 1).WithEnchantments(e...)
}

func level(s item.Stack, t item.EnchantmentType) int {
	e, _ := s.Enchantment(t)
	return e.Level()
}

// bedrockClientAnvil is what the anvil of a Bedrock client makes of the two items it is shown, after
// BDS 1.21.3: EnchantUtils::applyEnchant adds the material's enchantments one by one through
// ItemEnchants::canEnchant, which refuses an enchantment the item already has at its maximum level or
// above, raises two equal levels by one and otherwise keeps the higher level; the cost is the
// difference of ItemEnchants::getTotalValue (EnchantUtils::combineEnchantedItems) plus the prior work
// penalties. ok is whether the client shows a result that can be taken with the levels passed.
func bedrockClientAnvil(input, material item.Stack, playerLevel int) (result map[item.EnchantmentType]int, cost int, ok bool) {
	_, book := material.Item().(item.EnchantedBook)
	_, inputBook := input.Item().(item.EnchantedBook)
	value := func(m map[item.EnchantmentType]int) (v int) {
		for t, lvl := range m {
			c := t.Rarity().Cost()
			if book {
				c = max(1, c/2)
			}
			v += c * lvl
		}
		return v
	}
	before := map[item.EnchantmentType]int{}
	result = map[item.EnchantmentType]int{}
	for _, e := range input.Enchantments() {
		before[e.Type()], result[e.Type()] = e.Level(), e.Level()
	}
	changed := false
next:
	for _, e := range material.Enchantments() {
		t, lvl := e.Type(), e.Level()
		if !inputBook && !t.CompatibleWithItem(input.Item()) {
			continue
		}
		for other := range result {
			if other != t && !t.CompatibleWithEnchantment(other) {
				continue next
			}
		}
		if existing, has := result[t]; has {
			if existing >= t.MaxLevel() {
				continue // EnchantResult 1: refused.
			}
			if existing == lvl {
				lvl++
			}
			lvl = max(lvl, existing)
		}
		changed = changed || result[t] != lvl
		result[t] = lvl
	}
	cost = value(result) - value(before)
	if cost > 0 && input.Count() > 1 {
		cost = 40
	}
	cost += input.AnvilCost() + material.AnvilCost()
	return result, cost, changed && cost < 40 && cost <= playerLevel
}

// anvilCase is one combination of an item and a material in the anvil.
type anvilCase struct {
	name            string
	input, material item.Stack
	t               item.EnchantmentType
	// vanillaClient is whether a Bedrock client shown the real items offers a result.
	vanillaClient bool
	// want is the level of t in the result with AnvilRules.OverMax, 0 if the anvil makes nothing; cost
	// is what it costs then.
	want, cost int
}

func anvilCases() []anvilCase {
	sharp, eff := enchantment.Sharpness, enchantment.Efficiency
	return []anvilCase{
		{"item without the enchantment + above-max book", anvilSword(), anvilBook(ench(sharp, 10)), sharp, true, 10, 10},
		{"lower level below max + above-max book", anvilSword(ench(sharp, 3)), anvilBook(ench(sharp, 10)), sharp, true, 10, 10},
		{"lower level below max + book at max", anvilSword(ench(sharp, 3)), anvilBook(ench(sharp, 5)), sharp, true, 5, 2},
		{"equal levels below max", anvilSword(ench(sharp, 4)), anvilBook(ench(sharp, 4)), sharp, true, 5, 1},
		{"item at max + above-max book", anvilSword(ench(sharp, 5)), anvilBook(ench(sharp, 10)), sharp, false, 10, 10},
		{"item above max + higher book", anvilSword(ench(sharp, 7)), anvilBook(ench(sharp, 10)), sharp, false, 10, 10},
		{"item above max + lower book", anvilSword(ench(sharp, 10)), anvilBook(ench(sharp, 7)), sharp, false, 0, 0},
		{"item at max + book at max", anvilSword(ench(sharp, 5)), anvilBook(ench(sharp, 5)), sharp, false, 0, 0},
		{"two equal levels above max", anvilSword(ench(sharp, 10)), anvilBook(ench(sharp, 10)), sharp, false, 0, 0},
		{"two items, both above max", anvilSword(ench(sharp, 7)), anvilSword(ench(sharp, 10)), sharp, false, 10, 10},
		{"two items, equal above max", anvilSword(ench(sharp, 10)), anvilSword(ench(sharp, 10)), sharp, false, 0, 0},
		{"two items, at max + above max", anvilSword(ench(sharp, 5)), anvilSword(ench(sharp, 6)), sharp, false, 6, 6},
		{"book at max + higher book", anvilBook(ench(sharp, 5), ench(enchantment.Unbreaking, 3)), anvilBook(ench(sharp, 10)), sharp, false, 10, 10},
		// The pickaxe of the player report: Efficiency 5, Unbreaking 3, Fortune 3, three earlier anvil
		// uses, and the trader's Efficiency 10 book.
		{"reported pickaxe: Efficiency 5 + Efficiency 10 book", anvilPick(ench(eff, 5), ench(enchantment.Unbreaking, 3), ench(enchantment.Fortune, 3)).WithAnvilCost(3), anvilBook(ench(eff, 10)), eff, false, 10, 10},
		{"reported pickaxe with Efficiency 4", anvilPick(ench(eff, 4), ench(enchantment.Unbreaking, 3), ench(enchantment.Fortune, 3)).WithAnvilCost(3), anvilBook(ench(eff, 10)), eff, true, 10, 10},
		{"reported pickaxe without Efficiency", anvilPick(ench(enchantment.Unbreaking, 3), ench(enchantment.Fortune, 3)).WithAnvilCost(3), anvilBook(ench(eff, 10)), eff, true, 10, 10},
	}
}

// TestAnvilVanillaClientRefusals pins down which combinations a Bedrock client shown the real items
// offers no result for: those where the item has the enchantment at its maximum level or above.
func TestAnvilVanillaClientRefusals(t *testing.T) {
	SetAnvilRules(AnvilRules{})
	for _, c := range anvilCases() {
		in, mat := AnvilInputs(c.input, c.material)
		if !in.Equal(c.input) || !mat.Equal(c.material) {
			t.Errorf("%s: items changed without rules", c.name)
		}
		if _, _, ok := bedrockClientAnvil(c.input, c.material, 30); ok != c.vanillaClient {
			t.Errorf("%s: vanilla client offers a result: %v, want %v", c.name, ok, c.vanillaClient)
		}
	}
}

// TestAnvilOverMax: with the rules on, every combination in which the larger level wins gives that
// level, the client shown AnvilInputs offers exactly that result at exactly that cost, and nothing
// else of the item changes.
func TestAnvilOverMax(t *testing.T) {
	SetAnvilRules(AnvilRules{OverMax: true, NoPriorWork: true})
	defer SetAnvilRules(AnvilRules{})
	for _, c := range anvilCases() {
		res, cost, _, err := anvilCombine(c.input, c.material, false, "", false)
		viewIn, viewMat := AnvilInputs(c.input, c.material)
		clientRes, clientCost, clientOK := bedrockClientAnvil(viewIn, viewMat, 30)
		if c.want == 0 {
			if err == nil {
				t.Errorf("%s: got %v for %d levels, want nothing", c.name, res, cost)
			}
			if clientOK {
				t.Errorf("%s: the client offers a result the server refuses", c.name)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got := level(res, c.t); got != c.want || cost != c.cost {
			t.Errorf("%s: level %d for %d levels, want %d for %d", c.name, got, cost, c.want, c.cost)
		}
		if !clientOK || clientCost != cost {
			t.Errorf("%s: client offers a result: %v at cost %d, server cost %d", c.name, clientOK, clientCost, cost)
		}
		if len(clientRes) != len(res.Enchantments()) {
			t.Errorf("%s: client result has %d enchantments, server result %d", c.name, len(clientRes), len(res.Enchantments()))
		}
		for _, e := range res.Enchantments() {
			if clientRes[e.Type()] != e.Level() {
				t.Errorf("%s: client predicts %s %d, server gives %d", c.name, e.Type().Name(), clientRes[e.Type()], e.Level())
			}
		}
		for _, e := range c.input.Enchantments() {
			if e.Type() != c.t && level(res, e.Type()) != e.Level() {
				t.Errorf("%s: %s went from %d to %d", c.name, e.Type().Name(), e.Level(), level(res, e.Type()))
			}
		}
		if want := max(c.input.AnvilCost(), c.material.AnvilCost())*2 + 1; res.AnvilCost() != want {
			t.Errorf("%s: result counts %d anvil uses, want %d", c.name, res.AnvilCost(), want)
		}
		if res.Count() != 1 || res.Durability() != c.input.Durability() {
			t.Errorf("%s: result %v", c.name, res)
		}
	}
}

// TestAnvilRulesOffIsUnchanged: without the rules the anvil charges and gives what it always did.
func TestAnvilRulesOffIsUnchanged(t *testing.T) {
	SetAnvilRules(AnvilRules{})
	pick := anvilPick(ench(enchantment.Efficiency, 4)).WithAnvilCost(3)
	res, cost, _, err := anvilCombine(pick, anvilBook(ench(enchantment.Efficiency, 10)), false, "", false)
	if err != nil || level(res, enchantment.Efficiency) != 10 || cost != 6+3 || res.AnvilCost() != 7 {
		t.Errorf("Efficiency 4 + book 10: %v cost %d err %v, want level 10 for 9 levels", res, cost, err)
	}
	// 31 from earlier uses and 10 for the book: too expensive.
	worn := anvilPick().WithAnvilCost(31)
	if _, _, _, err := anvilCombine(worn, anvilBook(ench(enchantment.Efficiency, 10)), false, "", false); err == nil {
		t.Error("a cost of 41 levels was accepted")
	}
	SetAnvilRules(AnvilRules{NoPriorWork: true})
	defer SetAnvilRules(AnvilRules{})
	res, cost, _, err = anvilCombine(worn, anvilBook(ench(enchantment.Efficiency, 10)), false, "", false)
	if err != nil || cost != 10 || res.AnvilCost() != 63 {
		t.Errorf("without prior work: %v cost %d err %v, want 10 levels and 63 uses counted", res, cost, err)
	}
	if in, _ := AnvilInputs(anvilSword(ench(enchantment.Sharpness, 5)), anvilBook(ench(enchantment.Sharpness, 10))); level(in, enchantment.Sharpness) != 5 {
		t.Error("NoPriorWork alone hid an enchantment")
	}
}

// TestAnvilOverMaxKeepsWhatIsNotRaised: an enchantment is only left out of what the client is shown if
// the anvil puts it back at a higher level.
func TestAnvilOverMaxKeepsWhatIsNotRaised(t *testing.T) {
	SetAnvilRules(AnvilRules{OverMax: true})
	defer SetAnvilRules(AnvilRules{})
	sharp, smite := enchantment.Sharpness, enchantment.Smite

	// Smite does not go with the Sharpness the sword keeps.
	res, _, _, err := anvilCombine(anvilSword(ench(sharp, 5)), anvilBook(ench(sharp, 10), ench(smite, 5)), false, "", false)
	if err != nil || level(res, sharp) != 10 || level(res, smite) != 0 {
		t.Errorf("Sharpness 5 + book Sharpness 10, Smite 5: %v, %v", res, err)
	}
	// A sword that has both (a kit item): the book's Smite does not go with its Sharpness, so
	// nothing is hidden and nothing is made.
	odd := anvilSword(ench(sharp, 5), ench(smite, 5))
	if in, _ := AnvilInputs(odd, anvilBook(ench(smite, 10))); !in.Equal(odd) {
		t.Errorf("an enchantment the anvil will not apply was hidden: %v", in.Enchantments())
	}
	// A repair material and a lower book hide nothing.
	worn := anvilSword(ench(sharp, 10)).Damage(100)
	if in, _ := AnvilInputs(worn, item.NewStack(item.Diamond{}, 2)); !in.Equal(worn) {
		t.Error("a repair hid an enchantment")
	}
	if in, _ := AnvilInputs(worn, anvilBook(ench(sharp, 9))); !in.Equal(worn) {
		t.Error("a lower book hid an enchantment")
	}
	// A book with one enchantment stays an enchanted book.
	only := anvilBook(ench(sharp, 5))
	if in, _ := AnvilInputs(only, anvilBook(ench(sharp, 10))); !in.Equal(only) {
		t.Errorf("the only enchantment of a book was hidden: %v", in)
	}
}

// TestAnvilNeverLowersALevel tries random items, materials and rules: whatever the anvil makes has
// every enchantment of the item at the same level or one the material gave.
func TestAnvilNeverLowersALevel(t *testing.T) {
	defer SetAnvilRules(AnvilRules{})
	types := []item.EnchantmentType{enchantment.Sharpness, enchantment.Smite, enchantment.Unbreaking, enchantment.Knockback, enchantment.FireAspect, enchantment.Mending, enchantment.Efficiency}
	r := rand.New(rand.NewPCG(1, 2))
	random := func(s item.Stack) item.Stack {
		for _, et := range types {
			if r.IntN(3) == 0 {
				s = s.WithEnchantments(ench(et, 1+r.IntN(12)))
			}
		}
		return s.WithAnvilCost(r.IntN(8))
	}
	for i := 0; i < 20000; i++ {
		SetAnvilRules(AnvilRules{OverMax: r.IntN(4) != 0, NoPriorWork: r.IntN(2) == 0})
		in := random(item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1))
		var mat item.Stack
		if r.IntN(2) == 0 {
			mat = random(item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1))
		} else if mat = random(item.NewStack(item.EnchantedBook{}, 1)); len(mat.Enchantments()) == 0 {
			continue
		}
		res, cost, _, err := anvilCombine(in, mat, false, "", false)
		if err != nil {
			continue
		}
		if cost <= 0 || cost >= 40 || res.Count() != 1 {
			t.Fatalf("%v + %v: cost %d, result %v", in.Enchantments(), mat.Enchantments(), cost, res)
		}
		for _, e := range in.Enchantments() {
			if got := level(res, e.Type()); got < e.Level() {
				t.Fatalf("%v + %v: %s went from %d to %d", in.Enchantments(), mat.Enchantments(), e.Type().Name(), e.Level(), got)
			}
		}
		for _, e := range res.Enchantments() {
			top := max(level(in, e.Type()), level(mat, e.Type()))
			if level(in, e.Type()) == level(mat, e.Type()) && top < e.Type().MaxLevel() {
				top++
			}
			if e.Level() > top {
				t.Fatalf("%v + %v: %s %d out of nothing", in.Enchantments(), mat.Enchantments(), e.Type().Name(), e.Level())
			}
		}
	}
}

// anvilUser is a Controllable with experience levels and nothing else.
type anvilUser struct {
	Controllable
	level int
}

func (u *anvilUser) GameMode() world.GameMode { return world.GameModeSurvival }
func (u *anvilUser) ExperienceLevel() int     { return u.level }
func (u *anvilUser) SetExperienceLevel(l int) { u.level = l }

// anvilSessionTest runs f with a session that has an anvil open.
func anvilSessionTest(t *testing.T, f func(tx *world.Tx, s *Session, h *ItemStackRequestHandler)) {
	t.Helper()
	w := world.Config{Log: slog.New(slog.DiscardHandler), Synchronous: true}.New()
	defer func() { _ = w.Close() }()
	s := newTestSession()
	s.packets = make(chan packet.Packet, 1024)
	s.ui = inventory.New(51, nil)
	s.inv = inventory.New(36, nil)
	pos := cube.Pos{0, 10, 0}
	s.openedPos.Store(&pos)
	s.containerOpened.Store(true)
	s.openedContainerID.Store(1)
	h := &ItemStackRequestHandler{changes: map[byte]map[byte]changeInfo{}, responseChanges: map[int32]map[*inventory.Inventory]map[byte]responseChange{}}
	if err := w.Do(func(tx *world.Tx) {
		tx.SetBlock(pos, block.Anvil{}, nil)
		s.ui.SlotFunc(func(int, item.Stack, item.Stack) { s.sendInv(s.ui, protocol.WindowIDUI) })
		f(tx, s, h)
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// slotInfo is a request's reference to the item in a slot, as the client knows it.
func slotInfo(container byte, slot int, id int32) protocol.StackRequestSlotInfo {
	return protocol.StackRequestSlotInfo{Container: protocol.FullContainerName{ContainerID: container}, Slot: byte(slot), StackNetworkID: id}
}

// lastUI is the last UI inventory sent to the client, and sentSlots the single inventory slots sent
// (the last item per slot).
func lastUI(pks []packet.Packet) (ui []protocol.ItemInstance, sentSlots map[uint32]protocol.ItemInstance, responses []protocol.ItemStackResponse) {
	sentSlots = map[uint32]protocol.ItemInstance{}
	for _, pk := range pks {
		switch pk := pk.(type) {
		case *packet.InventoryContent:
			if pk.WindowID == protocol.WindowIDUI {
				ui = pk.Content
			}
		case *packet.InventorySlot:
			if pk.WindowID == protocol.WindowIDInventory {
				sentSlots[pk.Slot] = pk.NewItem
			}
		case *packet.ItemStackResponse:
			responses = append(responses, pk.Responses...)
		}
	}
	return
}

// shownLevel is the level of an enchantment on an item as sent to the client.
func shownLevel(s *Session, it protocol.ItemInstance, t item.EnchantmentType) int {
	return level(stackToItem(s.br, it.Stack), t)
}

// TestAnvilClientView walks a Bedrock player through the anvil with the rules on: the sword at the
// maximum level is shown without Sharpness once the book lies next to it, under its own stack network
// ID; the server makes the result of the real items, takes both, charges the levels and sends the
// result as it really is to where the client moved it.
func TestAnvilClientView(t *testing.T) {
	SetAnvilRules(AnvilRules{OverMax: true, NoPriorWork: true})
	defer SetAnvilRules(AnvilRules{})
	sharp := enchantment.Sharpness
	anvilSessionTest(t, func(tx *world.Tx, s *Session, h *ItemStackRequestHandler) {
		u := &anvilUser{level: 15}
		sword := anvilSword(ench(sharp, 5), ench(enchantment.Unbreaking, 3)).WithAnvilCost(3)
		book := anvilBook(ench(sharp, 10))
		_ = s.inv.SetItem(0, sword)
		_ = s.inv.SetItem(1, book)
		sword, _ = s.inv.Item(0)
		book, _ = s.inv.Item(1)

		request := func(id int32, actions ...protocol.StackRequestAction) {
			t.Helper()
			if err := h.handleRequest(protocol.ItemStackRequest{RequestID: id, Actions: actions}, s, tx, u); err != nil {
				t.Fatalf("request %d: %v", id, err)
			}
		}
		place := func(from, to protocol.StackRequestSlotInfo) *protocol.PlaceStackRequestAction {
			a := &protocol.PlaceStackRequestAction{}
			a.Count, a.Source, a.Destination = 1, from, to
			return a
		}

		// The sword alone is shown with its enchantments, only its prior work is left out.
		request(-1, place(slotInfo(protocol.ContainerInventory, 0, item_id(sword)), slotInfo(protocol.ContainerAnvilInput, anvilInputSlot, 0)))
		ui, _, _ := lastUI(written(s))
		sword, _ = s.ui.Item(anvilInputSlot)
		if shownLevel(s, ui[anvilInputSlot], sharp) != 5 || ui[anvilInputSlot].StackNetworkID != item_id(sword) {
			t.Fatalf("sword alone shown as %+v", ui[anvilInputSlot])
		}
		if stackToItem(s.br, ui[anvilInputSlot].Stack).AnvilCost() != 0 {
			t.Error("prior work shown to the client")
		}

		// With the book next to it the client is shown a sword it will add Sharpness 10 to.
		request(-3, place(slotInfo(protocol.ContainerInventory, 1, item_id(book)), slotInfo(protocol.ContainerAnvilMaterial, anvilMaterialSlot, 0)))
		ui, _, _ = lastUI(written(s))
		shown := stackToItem(s.br, ui[anvilInputSlot].Stack)
		if level(shown, sharp) != 0 || level(shown, enchantment.Unbreaking) != 3 || ui[anvilInputSlot].StackNetworkID != item_id(sword) {
			t.Fatalf("sword next to the book shown as %v (id %d, want %d)", shown.Enchantments(), ui[anvilInputSlot].StackNetworkID, item_id(sword))
		}
		if _, cost, ok := bedrockClientAnvil(shown, stackToItem(s.br, ui[anvilMaterialSlot].Stack), u.level); !ok || cost != 10 {
			t.Fatalf("client offers a result: %v at cost %d", ok, cost)
		}
		// The real sword is untouched.
		if real, _ := s.ui.Item(anvilInputSlot); level(real, sharp) != 5 || real.AnvilCost() != 3 {
			t.Fatalf("the real sword changed: %v", real)
		}

		// The client takes the result straight into inventory slot 5.
		request(-5,
			&protocol.CraftRecipeOptionalStackRequestAction{},
			&protocol.ConsumeStackRequestAction{},
			&protocol.ConsumeStackRequestAction{},
			place(slotInfo(protocol.ContainerCreatedOutput, craftingResult, -5), slotInfo(protocol.ContainerInventory, 5, 0)),
		)
		res, _ := s.inv.Item(5)
		if level(res, sharp) != 10 || level(res, enchantment.Unbreaking) != 3 || res.AnvilCost() != 7 || res.Count() != 1 {
			t.Fatalf("result %v %v", res, res.Enchantments())
		}
		if u.level != 5 {
			t.Errorf("player has %d levels left, want 5", u.level)
		}
		// Nothing is left in the anvil or made twice.
		var swords, books int
		for _, inv := range []*inventory.Inventory{s.inv, s.ui} {
			for _, it := range inv.Slots() {
				switch it.Item().(type) {
				case item.Sword:
					swords += it.Count()
				case item.EnchantedBook:
					books += it.Count()
				}
			}
		}
		if swords != 1 || books != 0 {
			t.Errorf("%d swords and %d books after the anvil, want 1 and 0", swords, books)
		}
		pks := written(s)
		ui, slots, responses := lastUI(pks)
		if len(responses) != 1 || responses[0].Status != protocol.ItemStackResponseStatusOK {
			t.Fatalf("responses %+v", responses)
		}
		if _, isResponse := pks[len(pks)-1].(*packet.ItemStackResponse); isResponse {
			t.Error("nothing sent after the response")
		}
		if got, ok := slots[5]; !ok || shownLevel(s, got, sharp) != 10 || got.StackNetworkID != item_id(res) {
			t.Errorf("result sent to the client as %+v (sent: %v)", got, ok)
		}
		if ui[anvilInputSlot].Stack.NetworkID != 0 || ui[anvilMaterialSlot].Stack.NetworkID != 0 {
			t.Error("anvil slots not shown empty after the craft")
		}

		// A second craft is refused: the inputs are gone.
		if err := h.handleRequest(protocol.ItemStackRequest{RequestID: -7, Actions: []protocol.StackRequestAction{&protocol.CraftRecipeOptionalStackRequestAction{}}}, s, tx, u); err == nil {
			t.Error("crafted again from an empty anvil")
		}
		if u.level != 5 {
			t.Error("levels charged for a refused craft")
		}
	})
}

// TestAnvilClientViewRestore: an item taken back out of the anvil, the anvil closed or the book taken
// away again, and the client has the item as it really is.
func TestAnvilClientViewRestore(t *testing.T) {
	SetAnvilRules(AnvilRules{OverMax: true})
	defer SetAnvilRules(AnvilRules{})
	sharp := enchantment.Sharpness
	anvilSessionTest(t, func(tx *world.Tx, s *Session, h *ItemStackRequestHandler) {
		u := &anvilUser{level: 30}
		_ = s.ui.SetItem(anvilInputSlot, anvilSword(ench(sharp, 7)))
		_ = s.ui.SetItem(anvilMaterialSlot, anvilBook(ench(sharp, 10)))
		sword, _ := s.ui.Item(anvilInputSlot)
		book, _ := s.ui.Item(anvilMaterialSlot)
		ui, _, _ := lastUI(written(s))
		if shownLevel(s, ui[anvilInputSlot], sharp) != 0 || !s.anvilShown.Load() {
			t.Fatal("sword not shown without Sharpness")
		}
		take := func(id int32, from, to protocol.StackRequestSlotInfo) {
			t.Helper()
			a := &protocol.TakeStackRequestAction{}
			a.Count, a.Source, a.Destination = 1, from, to
			if err := h.handleRequest(protocol.ItemStackRequest{RequestID: id, Actions: []protocol.StackRequestAction{a}}, s, tx, u); err != nil {
				t.Fatalf("request %d: %v", id, err)
			}
		}

		// The book is taken away to the cursor: the sword is shown as it is again.
		take(-1, slotInfo(protocol.ContainerAnvilMaterial, anvilMaterialSlot, item_id(book)), slotInfo(protocol.ContainerCursor, 0, 0))
		ui, _, _ = lastUI(written(s))
		if shownLevel(s, ui[anvilInputSlot], sharp) != 7 || s.anvilShown.Load() {
			t.Fatal("sword not shown as it is once the book is gone")
		}
		// And put back.
		book, _ = s.ui.Item(0)
		take(-3, slotInfo(protocol.ContainerCursor, 0, item_id(book)), slotInfo(protocol.ContainerAnvilMaterial, anvilMaterialSlot, 0))
		ui, _, _ = lastUI(written(s))
		if shownLevel(s, ui[anvilInputSlot], sharp) != 0 {
			t.Fatal("sword not shown without Sharpness with the book back")
		}

		// The sword goes straight back into the inventory: that slot is sent as it really is.
		take(-5, slotInfo(protocol.ContainerAnvilInput, anvilInputSlot, item_id(sword)), slotInfo(protocol.ContainerInventory, 3, 0))
		_, slots, _ := lastUI(written(s))
		real, _ := s.inv.Item(3)
		if got, ok := slots[3]; !ok || shownLevel(s, got, sharp) != 7 || level(real, sharp) != 7 || got.StackNetworkID != item_id(real) {
			t.Fatalf("sword taken back sent as %+v (sent: %v), really %v", got, ok, real.Enchantments())
		}
		// The move the client sent before it had that answer is covered as well.
		take(-7, slotInfo(protocol.ContainerInventory, 3, item_id(real)), slotInfo(protocol.ContainerInventory, 8, 0))
		_, slots, _ = lastUI(written(s))
		if got, ok := slots[8]; !ok || shownLevel(s, got, sharp) != 7 {
			t.Fatalf("sword moved on sent as %+v (sent: %v)", got, ok)
		}

		// Closing the anvil with a hidden enchantment (Player.MoveItemsToInventory): the items go to
		// the inventory as they are, outside a request, which sends them.
		_ = s.inv.SetItem(8, item.Stack{})
		_ = s.ui.SetItem(anvilInputSlot, anvilSword(ench(sharp, 7)))
		if ui, _, _ = lastUI(written(s)); shownLevel(s, ui[anvilInputSlot], sharp) != 0 {
			t.Fatal("sword not shown without Sharpness")
		}
		s.inv.SlotFunc(func(slot int, _, after item.Stack) { s.sendItem(after, slot, protocol.WindowIDInventory) })
		for _, it := range s.ui.Clear() {
			if _, err := s.inv.AddItem(it); err != nil {
				t.Fatal(err)
			}
		}
		_, slots, _ = lastUI(written(s))
		var found bool
		for slot, it := range s.inv.Slots() {
			if _, ok := it.Item().(item.Sword); ok {
				found = true
				if level(it, sharp) != 7 || shownLevel(s, slots[uint32(slot)], sharp) != 7 {
					t.Errorf("sword after closing: really %v, sent %+v", it.Enchantments(), slots[uint32(slot)])
				}
			}
		}
		if !found {
			t.Error("sword lost on closing")
		}
	})
}

// TestAnvilViewOffByDefault: without rules the UI inventory is sent as it is.
func TestAnvilViewOffByDefault(t *testing.T) {
	SetAnvilRules(AnvilRules{})
	anvilSessionTest(t, func(tx *world.Tx, s *Session, h *ItemStackRequestHandler) {
		_ = s.ui.SetItem(anvilInputSlot, anvilSword(ench(enchantment.Sharpness, 5)).WithAnvilCost(3))
		_ = s.ui.SetItem(anvilMaterialSlot, anvilBook(ench(enchantment.Sharpness, 10)))
		ui, _, _ := lastUI(written(s))
		shown := stackToItem(s.br, ui[anvilInputSlot].Stack)
		if level(shown, enchantment.Sharpness) != 5 || shown.AnvilCost() != 3 || s.anvilShown.Load() || s.anvilResync.Load() != 0 {
			t.Errorf("sword shown as %v without rules", shown)
		}
	})
}
