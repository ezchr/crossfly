//go:build crossfly_full

package javamap

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
)

// The egg blocks only register with the mobs.
func init() { block.RegisterMobBlocks() }

// javaItemIDs returns the protocol ids of the Java items from the Mojang reports.
func javaItemIDs(t *testing.T) map[string]int32 {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(refsDir(), "mojang-26.3/generated/reports/registries.json"))
	if err != nil {
		t.Skipf("Mojang reports not available: %v", err)
	}
	var reg map[string]struct {
		Entries map[string]struct {
			ID int32 `json:"protocol_id"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(raw, &reg); err != nil {
		t.Fatal(err)
	}
	ids := map[string]int32{}
	for name, e := range reg["minecraft:item"].Entries {
		ids[name] = e.ID
	}
	return ids
}

// TestTurtleEggJava checks that every turtle egg state is the Java turtle egg with the same egg count and hatch
// level, and the item the Java turtle egg.
func TestTurtleEggJava(t *testing.T) {
	rep := javaBlocks(t)
	table := DefaultBlockStates()
	for c := 0; c <= 3; c++ {
		for cr := 0; cr <= 2; cr++ {
			b := block.TurtleEgg{AdditionalCount: c, Cracks: cr}
			want := fmt.Sprintf("minecraft:turtle_egg[eggs=%d,hatch=%d]", c+1, cr)
			if got := rep.byID[table[world.BlockRuntimeID(b)]]; got != want {
				t.Errorf("%+v -> %s, want %s", b, got, want)
			}
		}
	}
	if id := Item(block.TurtleEgg{}); javaItemIDs(t)["minecraft:turtle_egg"] != id {
		t.Errorf("turtle egg item -> Java item %d, want minecraft:turtle_egg", id)
	}
}
