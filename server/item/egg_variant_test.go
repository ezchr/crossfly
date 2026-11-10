package item_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
)

// TestEggVariants checks the white, brown and blue eggs for both editions.
func TestEggVariants(t *testing.T) {
	for v, name := range map[item.EggVariant]string{
		item.EggTemperate: "minecraft:egg",
		item.EggWarm:      "minecraft:brown_egg",
		item.EggCold:      "minecraft:blue_egg",
	} {
		pbCheckItem(t, item.Egg{Variant: v}, name, name, 16)
	}
	if (item.Egg{}) != (item.Egg{Variant: item.EggTemperate}) {
		t.Error("the zero egg is not the white egg")
	}
	for v, want := range map[item.EggVariant]string{item.EggTemperate: "temperate", item.EggWarm: "warm", item.EggCold: "cold"} {
		if v.String() != want {
			t.Errorf("egg variant %d is %q, want %q", v, v.String(), want)
		}
	}
}
