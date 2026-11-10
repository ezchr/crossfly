package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Amethyst is a decorative block crafted from four amethyst shards. A sculk sensor next to it makes it
// resonate: it re-emits the vibrations the sensor receives, so that sensors further away hear them.
type Amethyst struct {
	solid
}

// ProjectileHit chimes when a projectile hits the block (AmethystBlock::onProjectileHit).
func (Amethyst) ProjectileHit(pos cube.Pos, tx *world.Tx, _ world.Entity, _ cube.Face) {
	tx.PlaySound(pos.Vec3Centre(), amethystChime{})
}

// BreakInfo ...
func (a Amethyst) BreakInfo() BreakInfo {
	return newBreakInfo(1.5, pickaxeHarvestable, pickaxeHarvestable, oneOf(a))
}

// EncodeItem ...
func (Amethyst) EncodeItem() (name string, meta int16) {
	return "minecraft:amethyst_block", 0
}

// EncodeBlock ...
func (Amethyst) EncodeBlock() (string, map[string]any) {
	return "minecraft:amethyst_block", nil
}

// amethystChime is the chime.amethyst_block level sound event (Java block.amethyst_block.chime).
type amethystChime struct{}

// Play ...
func (amethystChime) Play(*world.World, mgl64.Vec3) {}

// LevelSoundEvent ...
func (amethystChime) LevelSoundEvent() (event, entityType string, baby bool) {
	return "chime.amethyst_block", "", false
}
