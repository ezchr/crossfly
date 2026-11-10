//go:build crossfly_full

package main

import "testing"

func TestFlatBiomeByName(t *testing.T) {
	for name, want := range map[string]string{
		"plains":                 "plains",
		"ice_plains":             "ice_plains",
		"snowy_plains":           "ice_plains",
		"minecraft:snowy_plains": "ice_plains",
		"desert":                 "desert",
	} {
		b, ok := biomeByName(name)
		if !ok || b.String() != want {
			t.Errorf("biomeByName(%q) = %v %v, want %s", name, b, ok, want)
		}
	}
	if _, ok := biomeByName("no_such_biome"); ok {
		t.Error("found a biome that does not exist")
	}
}
