//go:build crossfly_full

package entity

import "github.com/df-mc/dragonfly/server/world"

// JavaRedstoneTypes returns the entity types of Crossfly Full's Java redstone: the minecarts, and the boats.
func JavaRedstoneTypes() []world.EntityType {
	return []world.EntityType{MinecartType, ChestMinecartType, HopperMinecartType, TNTMinecartType, BoatType, ChestBoatType}
}

// WithJavaRedstone returns c with the functions the Java redstone blocks spawn entities with (minecarts from
// minecart items and dispensers).
func WithJavaRedstone(c world.EntityRegistryConfig) world.EntityRegistryConfig {
	c.Minecart = func(opts world.EntitySpawnOpts, kind int) *world.EntityHandle {
		return NewMinecart(opts, MinecartKind(kind))
	}
	c.Boat = func(opts world.EntitySpawnOpts, wood int, chest bool) *world.EntityHandle {
		return NewBoat(opts, BoatWood(wood), chest)
	}
	return c
}
