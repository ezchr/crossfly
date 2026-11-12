//go:build crossfly_full

package javasession

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

// TestSpawnerJavaBlockEntity: Java clients get the spawner's mob as SpawnData.entity.id, by its Java name.
func TestSpawnerJavaBlockEntity(t *testing.T) {
	typ, data := blockEntityExtra(block.NewMobSpawner("minecraft:villager_v2"))
	if typ != "minecraft:mob_spawner" || len(data) < 2 {
		t.Fatalf("type %q, %d bytes", typ, len(data))
	}
	// Put the empty name back to decode it as a named root compound.
	full := append([]byte{data[0], 0, 0}, data[1:]...)
	var m map[string]any
	if err := nbt.UnmarshalEncoding(full, &m, nbt.BigEndian); err != nil {
		t.Fatal(err)
	}
	id := m["SpawnData"].(map[string]any)["entity"].(map[string]any)["id"]
	if id != "minecraft:villager" || m["RequiredPlayerRange"] != int16(16) {
		t.Errorf("NBT %v", m)
	}
	if _, data := blockEntityExtra(block.MobSpawner{}); data == nil {
		t.Error("an empty spawner has no block entity data")
	}
	if typ, _ := blockEntityExtra(block.Stone{}); typ != "" {
		t.Error("stone has spawner data")
	}
}
