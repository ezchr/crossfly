//go:build crossfly_full

package jnether

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javaspawn"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Install turns on what this package adds to the world in Java mob mode,
// besides its mob types: zombified piglins from nether portal blocks. The
// server calls it once after javamob.Install, before any world ticks
// (full.go).
func Install() {
	InstallPortalSpawning(PortalSpawnHooks{
		SpawnZombifiedPiglin: func(tx *world.Tx, pos mgl64.Vec3, yaw float64) world.Entity {
			m, err := javamob.Spawn(tx, ZombifiedPiglinID, pos, javamob.SpawnStructure, javamob.SpawnOptions{Rotation: cube.Rotation{yaw, 0}})
			if err != nil {
				return nil
			}
			return m
		},
		// BlockState.isValidSpawn and anyPlayerCloseEnoughForSpawning as
		// natural spawning has them (Java type ids).
		ValidSpawn: func(tx *world.Tx, pos cube.Pos) bool {
			return javaspawn.IsValidSpawn(tx, pos, "minecraft:zombified_piglin")
		},
		PlayerNear: javaspawn.AnyPlayerCloseEnoughForSpawning,
	})
}
