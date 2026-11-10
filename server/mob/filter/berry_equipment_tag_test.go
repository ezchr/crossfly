//go:build crossfly_full

package filter

import "testing"

// TestHasEquipmentTagItemTags: has_equipment_tag knows the item tags of the Bedrock item registry, such as the
// sulfur cube archetypes, besides the tool and armour tags derived from names.
func TestHasEquipmentTagItemTags(t *testing.T) {
	cases := []struct {
		item, tag string
		want      bool
	}{
		{"minecraft:oak_planks", "minecraft:sulfur_cube_archetype_bouncy", true},
		{"minecraft:tnt", "minecraft:sulfur_cube_archetype_explosive", true},
		{"minecraft:red_wool", "minecraft:sulfur_cube_archetype_light", true},
		{"minecraft:magma", "minecraft:sulfur_cube_archetype_hot", true},
		{"minecraft:tnt", "minecraft:sulfur_cube_archetype_bouncy", false},
		{"minecraft:stick", "minecraft:sulfur_cube_archetype_regular", false},
		{"minecraft:apple", "minecraft:is_food", true},
		{"minecraft:iron_axe", "minecraft:is_axe", true},
	}
	for _, c := range cases {
		it := Item{Name: c.item, Tags: itemTags(c.item)}
		if got := hasTag(it, c.tag); got != c.want {
			t.Errorf("%s has %s: %v, want %v", c.item, c.tag, got, c.want)
		}
	}
}
