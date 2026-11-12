//go:build crossfly_full

package javasession

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

// blockEntityExtra returns the Java block entity type and network NBT of blocks that are not JavaBlocks but
// whose block entity carries data Java clients draw from: the monster spawner's mob (BaseSpawner.load reads
// SpawnData.entity.id for the spinning mob, RequiredPlayerRange for when it spins, Delay for how fast).
func blockEntityExtra(b world.Block) (string, []byte) {
	s, ok := b.(block.MobSpawner)
	if !ok {
		return "", nil
	}
	enc := s.EncodeNBT()
	m := map[string]any{}
	for _, k := range []string{"Delay", "MinSpawnDelay", "MaxSpawnDelay", "SpawnCount", "MaxNearbyEntities", "RequiredPlayerRange", "SpawnRange"} {
		m[k] = enc[k]
	}
	if id := javaSpawnerEntity(s.EntityID); id != "" {
		m["SpawnData"] = map[string]any{"entity": map[string]any{"id": id}}
	}
	data, err := nbt.MarshalEncoding(m, nbt.BigEndian)
	if err != nil || len(data) < 3 {
		return "", nil
	}
	// Network NBT: the compound tag type, then its payload without a name.
	return "minecraft:mob_spawner", append([]byte{data[0]}, data[3:]...)
}

// javaSpawnerEntity is the Java entity type id of a Bedrock entity identifier, "" for none.
func javaSpawnerEntity(id string) string {
	if j, ok := javaEntityName[id]; ok {
		return j
	}
	return id
}
