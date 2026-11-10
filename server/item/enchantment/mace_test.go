package enchantment

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
)

func TestMaceEnchantments(t *testing.T) {
	for id, want := range map[int]item.EnchantmentType{38: WindBurst, 39: Density, 40: Breach} {
		if got, ok := item.EnchantmentByID(id); !ok || got != want {
			t.Errorf("id %d = %v, want %v", id, got, want)
		}
	}
	if !Density.CompatibleWithItem(item.Mace{}) || Sharpness.CompatibleWithItem(item.Mace{}) || !Smite.CompatibleWithItem(item.Mace{}) {
		t.Error("wrong mace enchantment compatibility")
	}
	if WindBurst.Multiplier(1) != 1.25 || WindBurst.Multiplier(3) != 2.25 || Breach.ArmourReduction(4) != 0.6 {
		t.Error("wrong mace enchantment values")
	}
}
