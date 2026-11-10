//go:build crossfly_full

package javamap

import (
	"fmt"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
)

// TestSnifferEggJava checks that every sniffer egg state is the Java sniffer egg with the same hatch level, and
// the item the Java sniffer egg.
func TestSnifferEggJava(t *testing.T) {
	rep := javaBlocks(t)
	table := DefaultBlockStates()
	for c := 0; c <= 2; c++ {
		b := block.SnifferEgg{Cracks: c}
		want := fmt.Sprintf("minecraft:sniffer_egg[hatch=%d]", c)
		if got := rep.byID[table[world.BlockRuntimeID(b)]]; got != want {
			t.Errorf("%+v -> %s, want %s", b, got, want)
		}
	}
	if id := Item(block.SnifferEgg{}); javaItemIDs(t)["minecraft:sniffer_egg"] != id {
		t.Errorf("sniffer egg item -> Java item %d, want minecraft:sniffer_egg", id)
	}
}
