package recipe

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

// The vanilla recipe breeze_rod -> 4 wind_charge in crafting_data.nbt was skipped by registerVanilla while
// the wind charge item did not exist (its output did not resolve). It resolves now.
func TestWindChargeRecipe(t *testing.T) {
	var data struct {
		Shapeless         []shapelessRecipe `nbt:"shapeless"`
		Shaped            []shapedRecipe    `nbt:"shaped"`
		UserDataShapeless []shapelessRecipe `nbt:"shulker_box"`
		Multi             []string          `nbt:"multi"`
	}
	if err := nbt.Unmarshal(vanillaCraftingData, &data); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range data.Shapeless {
		if len(s.Output) != 1 || s.Output[0].Name != "minecraft:wind_charge" {
			continue
		}
		found = true
		out, ok := s.Output.Stacks()
		if !ok {
			t.Fatal("wind charge recipe output does not resolve")
		}
		if _, ok := out[0].Item().(item.WindCharge); !ok || out[0].Count() != 4 {
			t.Fatalf("wind charge recipe makes %v, want 4 wind charges", out[0])
		}
		in, ok := s.Input.Items()
		if !ok || len(in) != 1 {
			t.Fatalf("wind charge recipe input %v (%v), want one breeze rod", in, ok)
		}
		if st, ok := in[0].(item.Stack); !ok {
			t.Fatalf("wind charge recipe input %T", in[0])
		} else if _, ok := st.Item().(item.BreezeRod); !ok {
			t.Fatalf("wind charge recipe input %v, want a breeze rod", st)
		}
	}
	if !found {
		t.Fatal("no shapeless wind charge recipe in crafting_data.nbt")
	}
}
