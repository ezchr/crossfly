//go:build crossfly_full

package vanilla

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javaspawn"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// SpawnerHost spawns the Java mobs of monster spawners (block.MobSpawner) as Java Edition's BaseSpawner does.
// Install installs it; mobs not ported to Java keep the Bedrock rules.
type SpawnerHost struct{}

// SpawnerMob reports whether id has a Java implementation.
func (SpawnerHost) SpawnerMob(id string) bool {
	if !javamob.Installed() {
		return false
	}
	_, ok := javamob.TypeOf(id)
	return ok
}

// SpawnerTry is one try of BaseSpawner.serverTick: no block collision and the type's spawn rules for the reason
// SPAWNER at the spot, fewer than MaxNearbyEntities mobs of the type in the spawner's block box grown by
// SpawnRange (else the spawner is full), then Mob.checkSpawnRules(SPAWNER) and checkSpawnObstruction, and the
// mob spawns with a random yaw, finalized for the reason SPAWNER.
func (SpawnerHost) SpawnerTry(tx *world.Tx, pos cube.Pos, s block.MobSpawner, at mgl64.Vec3) (world.Entity, bool) {
	id := s.EntityID
	typ := javaTypeID(id)
	if !javaspawn.SpawnerPlacementOK(tx, typ, at) {
		return nil, false
	}
	r := float64(s.SpawnRange)
	box := cube.Box(float64(pos[0]), float64(pos[1]), float64(pos[2]), float64(pos[0]+1), float64(pos[1]+1), float64(pos[2]+1)).Grow(r)
	n := 0
	for e := range tx.EntitiesWithin(box) {
		if e.H().Type().EncodeEntity() == id {
			n++
		}
	}
	if n >= int(s.MaxNearbyEntities) {
		return nil, true
	}
	if !javaspawn.SpawnerMobOK(tx, typ, at) {
		return nil, false
	}
	m, err := javamob.Spawn(tx, id, at, javamob.SpawnSpawner, javamob.SpawnOptions{Rotation: cube.Rotation{rand.Float64() * 360, 0}})
	if err != nil {
		return nil, false
	}
	return m, false
}

// javaTypeID is the Java entity type id of a Bedrock entity identifier, for the ones that differ.
func javaTypeID(id string) string {
	switch id {
	case "minecraft:villager_v2":
		return "minecraft:villager"
	case "minecraft:zombie_villager_v2":
		return "minecraft:zombie_villager"
	case "minecraft:evocation_illager":
		return "minecraft:evoker"
	case "minecraft:zombie_pigman":
		return "minecraft:zombified_piglin"
	case "minecraft:tropicalfish":
		return "minecraft:tropical_fish"
	}
	return id
}
