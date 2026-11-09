package block_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	_ "github.com/df-mc/dragonfly/server/world"
)

// TestShulkerBoxKeepsContents saves a box with items to NBT and reads it back, as a dropped box
// is saved in a player's inventory and a placed one in the world.
func TestShulkerBoxKeepsContents(t *testing.T) {
	box := block.NewShulkerBox()
	box.CustomName = "loot"
	inv := box.Inventory(nil, cube.Pos{})
	if err := inv.SetItem(3, item.NewStack(item.Diamond{}, 12)); err != nil {
		t.Fatal(err)
	}
	if err := inv.SetItem(26, item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1).WithEnchantments()); err != nil {
		t.Fatal(err)
	}
	got, ok := block.ShulkerBox{}.DecodeNBT(box.EncodeNBT()).(block.ShulkerBox)
	if !ok {
		t.Fatal("decoded to another type")
	}
	if got.CustomName != "loot" {
		t.Fatalf("name lost: %q", got.CustomName)
	}
	back := got.Inventory(nil, cube.Pos{})
	s, _ := back.Item(3)
	if s.Count() != 12 {
		t.Fatalf("slot 3 lost: %v", s)
	}
	if _, ok := s.Item().(item.Diamond); !ok {
		t.Fatalf("slot 3 changed: %v", s)
	}
	if s, _ := back.Item(26); s.Empty() {
		t.Fatal("slot 26 lost")
	}
}
