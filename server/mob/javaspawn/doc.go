//go:build crossfly_full

// Package javaspawn ports Java Edition natural mob spawning and despawning
// (Paper 26.3 with its default configuration) for Crossfly's Java mob mode
// (mobs=java): NaturalSpawner with Paper's per-player mob caps, the
// SpawnPlacements and checkSpawnRules of every 26.3 mob, the biome and
// structure spawn lists of the vanilla data, Mob.checkDespawn with Paper's
// despawn ranges, and the PhantomSpawner.
//
// Nothing runs until a server calls RegisterHook (the Java mob mode does),
// and natural spawning is then still off in every world until Enable:
//
//	javaspawn.SetRegistry(reg) // the Java mob base: constructors per type
//	javaspawn.RegisterHook()
//	javaspawn.Enable(w, javaspawn.Config{})
//
// Bedrock mode (package spawn) is independent of this package. See README.md.
package javaspawn
