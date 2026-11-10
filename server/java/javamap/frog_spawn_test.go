//go:build crossfly_full

package javamap

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
)

// TestFrogSpawnJava checks that frogspawn is the Java frogspawn, block and item.
func TestFrogSpawnJava(t *testing.T) {
	rep := javaBlocks(t)
	if got := rep.byID[DefaultBlockStates()[world.BlockRuntimeID(block.FrogSpawn{})]]; got != "minecraft:frogspawn" {
		t.Errorf("frogspawn -> %s, want minecraft:frogspawn", got)
	}
	if id := Item(block.FrogSpawn{}); javaItemIDs(t)["minecraft:frogspawn"] != id {
		t.Errorf("frogspawn item -> Java item %d, want minecraft:frogspawn", id)
	}
}
