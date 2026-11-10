//go:build crossfly_full

package item_test

import (
	"testing"
	_ "unsafe"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/creative"
	"github.com/df-mc/dragonfly/server/java/javamap"
	"github.com/df-mc/dragonfly/server/world"
)

//go:linkname harnessCreativeRegister github.com/df-mc/dragonfly/server/item/creative.registerCreativeItems
func harnessCreativeRegister()

// TestHarnessesRegistered checks that the sixteen harnesses register with Crossfly Full's mob items under
// their vanilla names, stack to one, show in the creative inventory and are their own items for Java clients.
func TestHarnessesRegistered(t *testing.T) {
	item.RegisterMobItems()
	hs := item.Harnesses()
	if len(hs) != 16 {
		t.Fatalf("%d harnesses, want 16", len(hs))
	}
	javaIDs := map[int32]string{}
	for _, h := range hs {
		name, meta := h.EncodeItem()
		it, ok := world.ItemByName(name, meta)
		if !ok || it != world.Item(h) {
			t.Errorf("%v not registered (got %v)", name, it)
		}
		if h.MaxCount() != 1 {
			t.Errorf("%v stacks to %d", name, h.MaxCount())
		}
		id, ok := javamap.ItemID(name, meta)
		if !ok {
			t.Errorf("%v has no Java item", name)
		} else if other, dup := javaIDs[id]; dup {
			t.Errorf("%v and %v share Java item %d", name, other, id)
		}
		javaIDs[id] = name
	}
	if n, _ := (item.Harness{Colour: item.ColourLightGrey()}).EncodeItem(); n != "minecraft:light_gray_harness" {
		t.Errorf("light grey harness is %v", n)
	}

	world.DefaultBlockRegistry.Finalize()
	harnessCreativeRegister()
	inCreative := map[string]bool{}
	for _, c := range creative.Items() {
		if h, ok := c.Stack.Item().(item.Harness); ok {
			name, _ := h.EncodeItem()
			inCreative[name] = true
		}
	}
	for _, h := range hs {
		if name, _ := h.EncodeItem(); !inCreative[name] {
			t.Errorf("%v not in the creative inventory", name)
		}
	}
}
