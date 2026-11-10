//go:build crossfly_full

package filter

import "testing"

// TestHasEquipmentAliases: has_equipment matches legacy names as BDS aliases do: a colour-split name without data
// is every colour of it (not moss carpet), with data the colour of that value; renamed items match their new name.
func TestHasEquipmentAliases(t *testing.T) {
	cases := []struct {
		value, item string
		aux         int
		want        bool
	}{
		{"carpet", "minecraft:red_carpet", 0, true},
		{"carpet", "minecraft:white_carpet", 0, true},
		{"carpet", "minecraft:light_gray_carpet", 0, true},
		{"carpet", "minecraft:moss_carpet", 0, false},
		{"minecraft:carpet", "minecraft:pale_moss_carpet", 0, false},
		{"carpet:14", "minecraft:red_carpet", 0, true},
		{"carpet:14", "minecraft:white_carpet", 0, false},
		{"wool", "minecraft:black_wool", 0, true},
		{"stained_hardened_clay", "minecraft:cyan_terracotta", 0, true},
		{"fireball:0", "minecraft:fire_charge", 0, true},
		{"shears", "minecraft:shears", 0, true},
		{"shears", "minecraft:carpet", 0, false},
		{"bucket:0", "minecraft:bucket", 0, true},
		{"saddle", "minecraft:red_carpet", 0, false},
	}
	for _, c := range cases {
		if got := parseItem(c.value).match(Item{Name: c.item, Aux: c.aux}); got != c.want {
			t.Errorf("%q matching %s: %v, want %v", c.value, c.item, got, c.want)
		}
	}
}
