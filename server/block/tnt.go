package block

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/gameevent"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// TNT is an explosive block that can be primed to generate an explosion.
type TNT struct {
	solid
}

var _ world.RedstonePowerAction = TNT{}

func (TNT) RedstoneNonConductive() {}

// CoreRedstoneOnly: with Java redstone (Crossfly Full), TNT is no redstone action. As Java Edition's TntBlock, it
// then only looks at its power when it is placed and when a neighbour updates it (tnt_java.go).
func (TNT) CoreRedstoneOnly() {}

// RedstonePowerAction primes TNT when it first receives redstone power. Redstone-primed TNT is not
// blockable by shields.
func (t TNT) RedstonePowerAction(pos cube.Pos, tx *world.Tx, oldPower, newPower int) {
	if world.JavaRedstone() || oldPower > 0 || newPower == 0 {
		return
	}
	spawnTnt(pos, tx, world.TNTSpawnConfig{Fuse: time.Second * 4, UnblockableByShield: true})
	postBlockEvent(tx, gameevent.PrimeFuse, pos, nil)
}

// ProjectileHit ...
func (t TNT) ProjectileHit(pos cube.Pos, tx *world.Tx, e world.Entity, _ cube.Face) {
	if f, ok := e.(flammableEntity); ok && f.OnFireDuration() > 0 {
		t.Ignite(pos, tx, e)
	}
}

// Activate ...
func (t TNT) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	held, _ := u.HeldItems()
	if _, ok := held.Enchantment(enchantment.FireAspect); ok {
		t.Ignite(pos, tx, u)
		ctx.DamageItem(1)
		return true
	}
	return false
}

// Ignite primes the TNT with source credited for the resulting explosion.
func (t TNT) Ignite(pos cube.Pos, tx *world.Tx, source world.Entity) bool {
	spawnTnt(pos, tx, world.TNTSpawnConfig{Fuse: time.Second * 4, Source: tntIgnitionSourceHandle(source)})
	// TntBlock::destroy: prime_fuse, made by who lit it.
	postBlockEvent(tx, gameevent.PrimeFuse, pos, source)
	return true
}

// Explode primes TNT with a short random fuse, preserving the explosion source and shield blockability.
func (t TNT) Explode(src world.ExplosionSource, pos cube.Pos, tx *world.Tx) {
	conf := world.TNTSpawnConfig{
		Fuse: time.Second/2 + time.Duration(rand.IntN(int(time.Second+time.Second/2))),
	}
	if s, ok := src.(world.ShieldBlockInfoSource); ok {
		info, blockable := s.ShieldBlockInfo()
		conf.UnblockableByShield = !blockable
		if info.Source != nil {
			conf.Source = info.Source.H()
		}
	}
	spawnTnt(pos, tx, conf)
}

// BreakInfo ...
func (t TNT) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(t))
}

// FlammabilityInfo ...
func (t TNT) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(15, 100, true)
}

// EncodeItem ...
func (t TNT) EncodeItem() (name string, meta int16) {
	return "minecraft:tnt", 0
}

// EncodeBlock ...
func (t TNT) EncodeBlock() (name string, properties map[string]interface{}) {
	return "minecraft:tnt", map[string]interface{}{"explode_bit": false}
}

// ownerEntity exposes the owner of a projectile-like entity.
type ownerEntity interface {
	ProjectileOwner() *world.EntityHandle
}

// tntIgnitionSourceHandle returns the entity credited for an ignition, resolving projectile owners.
func tntIgnitionSourceHandle(source world.Entity) *world.EntityHandle {
	if source == nil {
		return nil
	}
	if o, ok := source.(ownerEntity); ok {
		if owner := o.ProjectileOwner(); owner != nil {
			return owner
		}
	}
	return source.H()
}

// spawnTnt replaces the block at pos with a primed TNT entity spawned with conf.
func spawnTnt(pos cube.Pos, tx *world.Tx, conf world.TNTSpawnConfig) {
	tx.PlaySound(pos.Vec3Centre(), sound.TNT{})
	tx.SetBlock(pos, nil, nil)
	opts := world.EntitySpawnOpts{Position: pos.Vec3Centre()}
	if world.JavaRedstone() {
		// Java primes TNT at the bottom centre of the block (TntBlock.prime).
		opts.Position = pos.Vec3Middle()
	}
	if spawn := tx.World().EntityRegistry().Config().TNTWithConfig; spawn != nil {
		tx.AddEntity(spawn(opts, conf))
	}
}
