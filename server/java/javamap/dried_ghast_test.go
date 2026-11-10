package javamap

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// TestDriedGhastJava checks that every dried ghast state is the Java dried ghast with the same facing and
// hydration (dry, not waterlogged: the water is in the liquid layer), and the item the Java dried ghast.
func TestDriedGhastJava(t *testing.T) {
	rep := javaBlocks(t)
	table := DefaultBlockStates()
	for _, f := range cube.Directions() {
		for h := 0; h <= 3; h++ {
			b := block.DriedGhast{Facing: f, Hydration: h}
			want := fmt.Sprintf("minecraft:dried_ghast[facing=%v,hydration=%d,waterlogged=false]", f, h)
			if got := rep.byID[table[world.BlockRuntimeID(b)]]; got != want {
				t.Errorf("%+v -> %s, want %s", b, got, want)
			}
		}
	}
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
	if id := Item(block.DriedGhast{}); reg["minecraft:item"].Entries["minecraft:dried_ghast"].ID != id {
		t.Errorf("dried ghast item -> Java item %d, want minecraft:dried_ghast", id)
	}
}
