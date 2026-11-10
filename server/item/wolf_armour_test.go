//go:build crossfly_full

package item_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
)

func init() {
	// Wolf armour is an item of the mobs: registered before the creative inventory is filled.
	item.RegisterMobItems()
}

// TestWolfArmourItem checks the wolf armour item for both editions and its durability, defence and repair item.
func TestWolfArmourItem(t *testing.T) {
	pbCheckItem(t, item.WolfArmour{}, "minecraft:wolf_armor", "minecraft:wolf_armor", 1)
	a := item.WolfArmour{}
	if s := item.NewStack(a, 1); s.MaxDurability() != 64 {
		t.Errorf("wolf armour durability %v, want 64", s.MaxDurability())
	}
	if a.DefencePoints() != 11 || a.Toughness() != 0 {
		t.Errorf("wolf armour defence %v, toughness %v, want 11 and 0", a.DefencePoints(), a.Toughness())
	}
	if !a.RepairableBy(item.NewStack(item.ArmadilloScute{}, 1)) || a.RepairableBy(item.NewStack(item.Scute{}, 1)) {
		t.Error("wolf armour is not repaired by armadillo scutes alone")
	}
}
