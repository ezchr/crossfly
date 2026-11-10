package javamap

import (
	"fmt"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
)

// TestCaveVinesJava checks that cave vines map to the Java vine of the same age with or without berries (the
// body to cave_vines_plant; a plain vine is either form, picked from its neighbours) and glow berries to the
// Java item.
func TestCaveVinesJava(t *testing.T) {
	rep := javaBlocks(t)
	table := DefaultBlockStates()
	for age := 0; age <= 25; age++ {
		head := fmt.Sprintf("minecraft:cave_vines[age=%d,berries=true]", age)
		if got := rep.byID[table[world.BlockRuntimeID(block.CaveVines{Age: age, Berries: true})]]; got != head {
			t.Errorf("head with berries %d -> %s, want %s", age, got, head)
		}
		body := "minecraft:cave_vines_plant[berries=true]"
		if got := rep.byID[table[world.BlockRuntimeID(block.CaveVines{Age: age, Berries: true, Body: true})]]; got != body {
			t.Errorf("body with berries %d -> %s, want %s", age, got, body)
		}
		plain := rep.byID[table[world.BlockRuntimeID(block.CaveVines{Age: age})]]
		if plain != fmt.Sprintf("minecraft:cave_vines[age=%d,berries=false]", age) && plain != "minecraft:cave_vines_plant[berries=false]" {
			t.Errorf("plain vine %d -> %s", age, plain)
		}
	}
	if id := Item(block.CaveVines{}); berryJavaItems(t)["minecraft:glow_berries"] != id {
		t.Errorf("glow berries -> Java item %d, want minecraft:glow_berries", id)
	}
}
