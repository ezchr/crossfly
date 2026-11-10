package javamap

import (
	"strconv"
	"testing"
	_ "unsafe" // go:linkname

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item/creative"
	"github.com/df-mc/dragonfly/server/world"
)

// noinspection ALL
//
//go:linkname beehiveRegisterCreativeItems github.com/df-mc/dragonfly/server/item/creative.registerCreativeItems
func beehiveRegisterCreativeItems()

// Every beehive and bee nest state maps to the Java state with the same facing and honey level, both items to
// their Java items, and both are in the creative inventory.
func TestBeehiveJavaStates(t *testing.T) {
	rep := javaBlocks(t)
	table := DefaultBlockStates()
	facing := map[cube.Direction]string{cube.North: "north", cube.South: "south", cube.West: "west", cube.East: "east"}
	for _, nest := range []bool{false, true} {
		java := "minecraft:beehive"
		if nest {
			java = "minecraft:bee_nest"
		}
		for d, f := range facing {
			for level := 0; level <= 5; level++ {
				b := block.Beehive{Nest: nest, Facing: d, HoneyLevel: level}
				want := java + "[facing=" + f + ",honey_level=" + strconv.Itoa(level) + "]"
				if got := rep.byID[table[world.BlockRuntimeID(b)]]; got != want {
					t.Errorf("%+v -> %s, want %s", b, got, want)
				}
			}
		}
		name, meta := block.Beehive{Nest: nest}.EncodeItem()
		if id, ok := ItemID(name, meta); !ok || id == JavaItemBarrier {
			t.Errorf("item %v -> %d %v", name, id, ok)
		}
	}
	if len(creative.Items()) == 0 {
		beehiveRegisterCreativeItems()
	}
	found := map[string]bool{}
	for _, s := range creative.Items() {
		if b, ok := s.Stack.Item().(block.Beehive); ok {
			name, _ := b.EncodeItem()
			found[name] = true
		}
	}
	if !found["minecraft:beehive"] || !found["minecraft:bee_nest"] {
		t.Errorf("creative inventory: %v", found)
	}
}
