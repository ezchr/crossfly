package session

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// multiCraftTest runs f with a session that has a crafting table open, whose recipe network ID 7 is the
// multi recipe of the UUID passed.
func multiCraftTest(t *testing.T, id string, f func(tx *world.Tx, s *Session, h *ItemStackRequestHandler)) {
	t.Helper()
	recipe.RegisterDynamic(recipe.FireworkRecipe{})
	recipe.RegisterDynamic(recipe.BookCloningRecipe{})

	w := world.Config{Log: slog.New(slog.DiscardHandler), Synchronous: true}.New()
	defer func() { _ = w.Close() }()
	s := newTestSession()
	s.ui = inventory.New(51, nil)
	s.inv = inventory.New(36, nil)
	s.recipes = map[uint32]recipe.Recipe{7: recipe.NewMulti(uuid.MustParse(id))}
	s.openedContainerID.Store(1)
	h := &ItemStackRequestHandler{changes: map[byte]map[byte]changeInfo{}, responseChanges: map[int32]map[*inventory.Inventory]map[byte]responseChange{}}
	if err := w.Do(func(tx *world.Tx) { f(tx, s, h) }).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// TestCraftFireworkThroughHandler crafts rockets through the CraftRecipe action of the firework multi
// recipe: the grid loses one of each item per craft and the output slot gets the rockets.
func TestCraftFireworkThroughHandler(t *testing.T) {
	multiCraftTest(t, "00000000-0000-0000-0000-000000000002", func(tx *world.Tx, s *Session, h *ItemStackRequestHandler) {
		_ = s.ui.SetItem(craftingGridLargeOffset+0, item.NewStack(item.Paper{}, 5))
		_ = s.ui.SetItem(craftingGridLargeOffset+4, item.NewStack(item.Gunpowder{}, 2))
		_ = s.ui.SetItem(craftingGridLargeOffset+5, item.NewStack(item.Gunpowder{}, 3))
		if err := h.handleCraft(&protocol.CraftRecipeStackRequestAction{RecipeNetworkID: 7, NumberOfCrafts: 2}, s, tx); err != nil {
			t.Fatal(err)
		}
		out, _ := s.ui.Item(craftingResult)
		fw, ok := out.Item().(item.Firework)
		if !ok || out.Count() != 6 || fw.Duration != 1500*time.Millisecond {
			t.Fatalf("result %v, want 6 rockets of flight 2", out)
		}
		for slot, want := range map[int]int{0: 3, 4: 0, 5: 1} {
			if st, _ := s.ui.Item(craftingGridLargeOffset + slot); st.Count() != want {
				t.Errorf("grid slot %d has %d, want %d", slot, st.Count(), want)
			}
		}
		// Each request starts without pending results.
		h.pendingResults = nil
		// The grid now has paper and one gunpowder: still a rocket, through the recipe book path too.
		if err := h.handleAutoCraft(&protocol.AutoCraftRecipeStackRequestAction{RecipeNetworkID: 7, NumberOfCrafts: 1}, s, tx); err != nil {
			t.Fatal(err)
		}
		if out, _ := s.ui.Item(craftingResult); out.Count() != 3 || out.Item().(item.Firework).Duration != time.Second {
			t.Errorf("auto craft result %v, want 3 rockets of flight 1", out)
		}
		h.pendingResults = nil
		// Only paper is left, which makes nothing.
		if err := h.handleCraft(&protocol.CraftRecipeStackRequestAction{RecipeNetworkID: 7, NumberOfCrafts: 1}, s, tx); err == nil {
			t.Error("paper alone crafted something")
		}
	})
}

// TestCraftBookCloneThroughHandler: book cloning has two results, which the client creates one by one.
func TestCraftBookCloneThroughHandler(t *testing.T) {
	multiCraftTest(t, "d1ca6b84-338e-4f2f-9c6b-76cc8b4bd98d", func(tx *world.Tx, s *Session, h *ItemStackRequestHandler) {
		book := item.WrittenBook{Title: "t", Author: "a", Pages: []string{"p"}}
		_ = s.ui.SetItem(craftingGridLargeOffset+1, item.NewStack(book, 1))
		_ = s.ui.SetItem(craftingGridLargeOffset+2, item.NewStack(item.BookAndQuill{}, 1))
		if err := h.handleCraft(&protocol.CraftRecipeStackRequestAction{RecipeNetworkID: 7, NumberOfCrafts: 1}, s, tx); err != nil {
			t.Fatal(err)
		}
		if len(h.pendingResults) != 2 {
			t.Fatalf("pending results %v, want the copy and the original", h.pendingResults)
		}
		if err := h.handleCreate(&protocol.CreateStackRequestAction{ResultsSlot: 0}, s, tx); err != nil {
			t.Fatal(err)
		}
		if out, _ := s.ui.Item(craftingResult); out.Item().(item.WrittenBook).Generation != item.CopyGeneration() {
			t.Errorf("first result %v, want the copy", out)
		}
		if err := h.handleCreate(&protocol.CreateStackRequestAction{ResultsSlot: 1}, s, tx); err != nil {
			t.Fatal(err)
		}
		if out, _ := s.ui.Item(craftingResult); out.Item().(item.WrittenBook).Generation != item.OriginalGeneration() {
			t.Errorf("second result %v, want the original", out)
		}
		for _, slot := range []int{1, 2} {
			if st, _ := s.ui.Item(craftingGridLargeOffset + slot); !st.Empty() {
				t.Errorf("grid slot %d not consumed: %v", slot, st)
			}
		}
	})
}
