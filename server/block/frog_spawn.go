package block

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// FrogSpawn is a thin layer of frog eggs that frogs lay on the surface of still water. After a while it hatches
// into tadpoles.
type FrogSpawn struct {
	empty
	transparent
}

// frogSpawnHatchDelay returns the delay until frogspawn hatches: 3600-11999 ticks (FrogSpawnBlock::onPlace).
func frogSpawnHatchDelay() time.Duration {
	return time.Duration(3600+rand.IntN(8400)) * time.Second / 20
}

// UseOnBlock places frogspawn on the water clicked, as a lily pad (FrogSpawnBlockItem::_useOn): on an empty block
// above a water source.
func (f FrogSpawn) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, f)
	if !used || !frogSpawnCanSurvive(tx, pos) {
		return false
	}
	place(tx, pos, f, user, ctx)
	if placed(ctx) {
		f.OnPlace(pos, tx)
	}
	return placed(ctx)
}

// frogSpawnCanSurvive reports whether frogspawn can be at pos (FrogSpawnBlock::canSurvive): in an empty block
// (or its own) above a water source.
func frogSpawnCanSurvive(tx *world.Tx, pos cube.Pos) bool {
	if _, ok := tx.Liquid(pos); ok {
		return false
	}
	switch tx.Block(pos).(type) {
	case Air, FrogSpawn:
	default:
		return false
	}
	w, ok := tx.Liquid(pos.Side(cube.FaceDown))
	if !ok {
		return false
	}
	_, water := w.(Water)
	return water && w.LiquidDepth() == 8 && !w.LiquidFalling()
}

// OnPlace starts the hatching of frogspawn set at pos (FrogSpawnBlock::onPlace): it hatches 3600-11999 ticks
// later. Frogs laying frogspawn call it too.
func (f FrogSpawn) OnPlace(pos cube.Pos, tx *world.Tx) {
	tx.ScheduleBlockUpdate(pos, f, frogSpawnHatchDelay())
}

// ScheduledTick hatches the frogspawn (FrogSpawnBlock::tick): it becomes 2-6 tadpoles (minecraft:entity_born,
// persistent) and goes. Frogspawn that lost its water breaks instead.
func (f FrogSpawn) ScheduledTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	if !frogSpawnCanSurvive(tx, pos) {
		f.destroy(pos, tx)
		return
	}
	tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: "hatch.frog_spawn", Volume: 1, Pitch: 1})
	for n := 2 + r.IntN(5); n > 0; n-- {
		tx.AddParticle(pos.Vec3Centre(), particle.BlockBreak{Block: f})
		SpawnMob(tx, "minecraft:tadpole", pos.Vec3Middle(), MobSpawnOptions{Event: "minecraft:entity_born", Persistent: true})
	}
	tx.SetBlock(pos, nil, nil)
}

// NeighbourUpdateTick breaks the frogspawn once the water under it is gone.
func (f FrogSpawn) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !frogSpawnCanSurvive(tx, pos) {
		f.destroy(pos, tx)
	}
}

// EntityInside breaks the frogspawn when a falling block lands in it (FrogSpawnBlock::entityInside).
func (f FrogSpawn) EntityInside(pos cube.Pos, tx *world.Tx, e world.Entity) {
	if e.H().Type().EncodeEntity() == "minecraft:falling_block" {
		f.destroy(pos, tx)
	}
}

// destroy breaks the frogspawn without drops, with its break sound.
func (f FrogSpawn) destroy(pos cube.Pos, tx *world.Tx) {
	tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: "break.frog_spawn", Volume: 1, Pitch: 1})
	breakBlockNoDrops(f, pos, tx)
}

// HasLiquidDrops ...
func (FrogSpawn) HasLiquidDrops() bool {
	return false
}

// SideClosed ...
func (FrogSpawn) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// BreakInfo ...
func (f FrogSpawn) BreakInfo() BreakInfo {
	// Frogspawn breaks at once and drops nothing, not even with silk touch. A player breaking it also hears its
	// break sound (FrogSpawnBlock::playerWillDestroy).
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, simpleDrops()).withBreakHandler(func(pos cube.Pos, tx *world.Tx, u item.User) {
		if u != nil {
			tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: "break.frog_spawn", Volume: 1, Pitch: 1})
		}
	})
}

// EncodeItem ...
func (FrogSpawn) EncodeItem() (name string, meta int16) {
	return "minecraft:frog_spawn", 0
}

// EncodeBlock ...
func (FrogSpawn) EncodeBlock() (string, map[string]any) {
	return "minecraft:frog_spawn", nil
}
