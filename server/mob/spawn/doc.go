//go:build crossfly_full

// Package spawn ports Bedrock natural spawning and despawning (BDS Spawner,
// MobSpawnRules, SpawnConditions, DespawnComponent) to the mob framework.
//
// Natural spawning is off in every world. A server turns it on per world:
//
//	spawn.Enable(w, spawn.Config{})
//
// The package registers minecraft:despawn and minecraft:persistent and a
// StageDespawn system; despawning follows the vanilla rules in every world
// (SetDespawning turns it off per world). See README.md.
package spawn
