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

// berryJavaItems returns the protocol ids of the Java items from the Mojang reports.
func berryJavaItems(t *testing.T) map[string]int32 {
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

// TestSweetBerryBushJava checks that every sweet berry bush state is the Java bush of the same age (Bedrock's
// states above 3 are age 3) and sweet berries the Java item.
func TestSweetBerryBushJava(t *testing.T) {
	rep := javaBlocks(t)
	table := DefaultBlockStates()
	for age := 0; age <= 7; age++ {
		b := block.SweetBerryBush{Age: age}
		want := fmt.Sprintf("minecraft:sweet_berry_bush[age=%d]", min(age, 3))
		if got := rep.byID[table[world.BlockRuntimeID(b)]]; got != want {
			t.Errorf("%+v -> %s, want %s", b, got, want)
		}
	}
	if id := Item(block.SweetBerryBush{}); berryJavaItems(t)["minecraft:sweet_berries"] != id {
		t.Errorf("sweet berries -> Java item %d, want minecraft:sweet_berries", id)
	}
}
