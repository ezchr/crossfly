package block

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// SnifferEgg is the egg a sniffer lays. It cracks twice and then hatches into a baby sniffer, twice as fast on a
// moss block.
type SnifferEgg struct {
	transparent

	// Cracks is how far the egg cracked, 0-2. Cracked twice, the egg hatches at its next step.
	Cracks int
}

// Model ...
func (SnifferEgg) Model() world.BlockModel {
	return snifferEggModel{}
}

// snifferEggModel is the model of a sniffer egg: 14/16 wide, 12/16 deep and a block high.
// Java fallback: no Bedrock data (the shape SnifferEggBlock gives itself is in data the decompile does not show).
type snifferEggModel struct{}

// BBox ...
func (snifferEggModel) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	return []cube.BBox{cube.Box(0.0625, 0, 0.125, 0.9375, 1, 0.875)}
}

// FaceSolid ...
func (snifferEggModel) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool {
	return false
}

// snifferEggDelay returns the delay to the next step of the sniffer egg at pos
// (SnifferEggBlock::_addToRandomTickingQueue): 7200-8799 ticks, half that on a moss block.
func snifferEggDelay(tx *world.Tx, pos cube.Pos) time.Duration {
	d := float64(7200 + rand.IntN(1600))
	if snifferEggOnMoss(tx, pos) {
		d *= 0.5
	}
	return time.Duration(int(d)) * time.Second / 20
}

// snifferEggOnMoss reports whether the sniffer egg at pos hatches faster (SnifferEggBlock::_shouldHatchFaster):
// on a moss block.
func snifferEggOnMoss(tx *world.Tx, pos cube.Pos) bool {
	n, _ := tx.Block(pos.Side(cube.FaceDown)).EncodeBlock()
	return n == "minecraft:moss_block"
}

// UseOnBlock ...
func (s SnifferEgg) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, s)
	if !used {
		return false
	}
	s = SnifferEgg{}
	place(tx, pos, s, user, ctx)
	if placed(ctx) {
		s.OnPlace(pos, tx)
	}
	return placed(ctx)
}

// OnPlace queues the first step of a sniffer egg set at pos (SnifferEggBlock::onPlace).
func (s SnifferEgg) OnPlace(pos cube.Pos, tx *world.Tx) {
	tx.ScheduleBlockUpdate(pos, s, snifferEggDelay(tx, pos))
}

// ScheduledTick is a step of the sniffer egg (SnifferEggBlock::tick): it cracks a level, and cracked twice it
// hatches into a baby sniffer (minecraft:entity_born) and goes.
func (s SnifferEgg) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if s.Cracks+1 < 3 {
		s.Cracks++
		tx.SetBlock(pos, s, nil)
		tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: "block.sniffer_egg.crack", Volume: 1, Pitch: 1})
		s.OnPlace(pos, tx)
		return
	}
	SpawnMob(tx, "minecraft:sniffer", pos.Vec3Middle(), MobSpawnOptions{Event: "minecraft:entity_born"})
	tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: "block.sniffer_egg.hatch", Volume: 1, Pitch: 1})
	tx.SetBlock(pos, nil, nil)
}

// SideClosed ...
func (SnifferEgg) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// BreakInfo ...
func (s SnifferEgg) BreakInfo() BreakInfo {
	return newBreakInfo(0.5, alwaysHarvestable, nothingEffective, oneOf(SnifferEgg{}))
}

// EncodeItem ...
func (SnifferEgg) EncodeItem() (name string, meta int16) {
	return "minecraft:sniffer_egg", 0
}

// EncodeBlock ...
func (s SnifferEgg) EncodeBlock() (string, map[string]any) {
	return "minecraft:sniffer_egg", map[string]any{"cracked_state": crackedStates[s.Cracks]}
}

// allSnifferEggs returns every state of a sniffer egg.
func allSnifferEggs() (b []world.Block) {
	for c := 0; c <= 2; c++ {
		b = append(b, SnifferEgg{Cracks: c})
	}
	return
}
