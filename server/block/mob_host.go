package block

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// MobHost lets blocks (and the entities of package entity) spawn the mobs of Crossfly Full: golems built
// from a carved pumpkin, eggs hatching, bees leaving their hive. Package mob installs it with SetMobHost;
// without mobs it is nil and those blocks spawn nothing. Features that need more of the mobs define their
// own optional interface next to the block and assert the host to it.
type MobHost interface {
	// SpawnMob spawns a mob of the entity identifier id at pos and reports whether it did.
	SpawnMob(tx *world.Tx, id string, pos mgl64.Vec3, opts MobSpawnOptions) (world.Entity, bool)
}

// MobSpawnOptions are the options of MobHost.SpawnMob.
type MobSpawnOptions struct {
	// Yaw is the facing of the mob in degrees.
	Yaw float64
	// Event is the event fired once the mob is in the world. Empty fires minecraft:entity_spawned,
	// "minecraft:entity_born" makes most mobs a baby.
	Event string
	// Persistent keeps the mob from despawning.
	Persistent bool
}

// mobHost is the MobHost set by SetMobHost.
var mobHost MobHost

// SetMobHost installs the MobHost blocks spawn mobs through. Call it once, from an init function.
func SetMobHost(h MobHost) { mobHost = h }

// Mobs returns the MobHost, or nil if there are no mobs.
func Mobs() MobHost { return mobHost }

// SpawnMob spawns a mob through the MobHost. It reports false if there are no mobs or the mob has no type.
func SpawnMob(tx *world.Tx, id string, pos mgl64.Vec3, opts MobSpawnOptions) (world.Entity, bool) {
	if mobHost == nil {
		return nil, false
	}
	return mobHost.SpawnMob(tx, id, pos, opts)
}
