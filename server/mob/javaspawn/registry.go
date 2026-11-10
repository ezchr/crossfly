//go:build crossfly_full

package javaspawn

import (
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Registry is what spawning needs of the Java mob base: which types have a
// Java implementation and a constructor for them. Spawning decides where
// and whether a mob spawns (all of SpawnPlacements and the mob's
// checkSpawnRules/checkSpawnObstruction are in this package); the
// constructor builds the mob, runs its Mob.finalizeSpawn for
// EntitySpawnReason.NATURAL and adds it to the world.
type Registry interface {
	// Has reports whether the Java type id ("minecraft:zombie") has an
	// implementation. Types without one are never spawned (like
	// NaturalSpawner.getMobForSpawn returning null).
	Has(typ string) bool
	// Spawn creates a mob of typ at pos with the yaw passed (pitch 0),
	// finalizes it for a natural spawn with the group data of the pack
	// (nil for the first mob of a pack) and adds it to tx. It returns the
	// mob and the group data for the next pack member.
	Spawn(tx *world.Tx, typ string, pos mgl64.Vec3, yaw float64, group any) (m Mob, groupOut any, ok bool)
	// Mob returns e as a Java mob, false for any other entity.
	Mob(e world.Entity) (Mob, bool)
}

// Mob is a Java mode mob as spawning and despawning see it.
//
// A Mob may also implement these optional methods (vanilla Mob methods);
// without them the type's vanilla default for a fresh mob is used:
//
//	PersistenceRequired() bool              // Mob.isPersistenceRequired (default false)
//	RequiresCustomPersistence() bool        // passenger, leashed, from a bucket, carrying a block, ... (default false)
//	RemoveWhenFarAway(distSqr float64) bool // per type (see removeWhenFarAway)
//	NoActionTime() int                      // Mob.noActionTime (no random despawning without it)
//	SetNoActionTime(ticks int)
//	NaturallySpawned() bool                 // spawn reason NATURAL or CHUNK_GEN (default: spawned by this package)
//	IsMaxGroupSizeReached(n int) bool       // Mob.isMaxGroupSizeReached (tropical fish not in a school)
//	Discard()                               // Entity.discard (default: removed from the world and closed)
type Mob interface {
	world.Entity
	// JavaType is the mob's Java entity type id, e.g. "minecraft:zombie".
	JavaType() string
}

type (
	persistenceMob       interface{ PersistenceRequired() bool }
	customPersistenceMob interface{ RequiresCustomPersistence() bool }
	farAwayMob           interface {
		RemoveWhenFarAway(distSqr float64) bool
	}
	noActionMob interface {
		NoActionTime() int
		SetNoActionTime(ticks int)
	}
	naturalMob   interface{ NaturallySpawned() bool }
	groupSizeMob interface{ IsMaxGroupSizeReached(n int) bool }
	discardMob   interface{ Discard() }
)

type registryBox struct{ r Registry }

var registry atomic.Pointer[registryBox]

// SetRegistry sets the Java mob registry spawning builds mobs with. Until
// it is set, nothing spawns or despawns.
func SetRegistry(r Registry) {
	if r == nil {
		registry.Store(nil)
		return
	}
	registry.Store(&registryBox{r})
}

func currentRegistry() Registry {
	if b := registry.Load(); b != nil {
		return b.r
	}
	return nil
}
