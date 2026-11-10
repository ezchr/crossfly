//go:build crossfly_full

package javamap

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
)

// javaItemNames returns the Java 26.3 item names by protocol id, from the Mojang reports.
func harnessJavaItemNames(t *testing.T) map[int32]string {
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
	names := map[int32]string{}
	for k, v := range reg["minecraft:item"].Entries {
		names[v.ID] = k
	}
	return names
}

// TestHarnessJavaItems checks that every harness is the Java harness of its colour.
func TestHarnessJavaItems(t *testing.T) {
	names := harnessJavaItemNames(t)
	for _, h := range item.Harnesses() {
		want := "minecraft:" + h.Colour.String() + "_harness"
		if got := names[Item(h)]; got != want {
			t.Errorf("%v -> Java %v, want %v", h.Colour, got, want)
		}
	}
}
