package block

import "github.com/df-mc/dragonfly/server/world"

// explosionBreaker is the actor an explosion reports with the blocks it
// breaks (world.BlockBroken): BDS Explosion::explode passes its source actor
// (the creeper, the primed TNT, ...) to BlockSource::setBlock. That is the
// exploding entity of a world.EntityExplosionSource, else the configured
// Source, else nil.
func explosionBreaker(src world.ExplosionSource, source world.Entity) world.Entity {
	if c, ok := src.(configuredExplosionSource); ok {
		src = c.ExplosionSource
	}
	if e, ok := src.(world.EntityExplosionSource); ok && e.Entity != nil {
		return e.Entity
	}
	return source
}

// blockTypeChanged reports whether b replaced a with a different block type
// (BDS only dispatches block changes whose BlockLegacy differs).
func blockTypeChanged(a, b world.Block) bool {
	an, _ := a.EncodeBlock()
	bn, _ := b.EncodeBlock()
	return an != bn
}
