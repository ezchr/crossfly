package block

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// DriedGhast is a small block that, left waterlogged, rehydrates over time and turns into a ghastling, a baby
// happy ghast. Out of water it dries back out.
type DriedGhast struct {
	transparent
	sourceWaterDisplacer

	// Facing is the direction the dried ghast faces.
	Facing cube.Direction
	// Hydration is how far the dried ghast rehydrated, 0-3 (rehydration_level). One step further in water it
	// becomes a ghastling.
	Hydration int
}

// driedGhastTickDelay is the time between two steps of a dried ghast: 6000 ticks (the delay the 26.60
// DriedGhastBlock queues its tick with on placement and after every tick).
const driedGhastTickDelay = 6000 * time.Second / 20

// Model ...
func (DriedGhast) Model() world.BlockModel {
	return driedGhastModel{}
}

// driedGhastModel is the model of a dried ghast: 10/16 wide and deep, 10/16 high.
type driedGhastModel struct{}

// BBox ...
func (driedGhastModel) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	return []cube.BBox{cube.Box(0.1875, 0, 0.1875, 0.8125, 0.625, 0.8125)}
}

// FaceSolid ...
func (driedGhastModel) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool {
	return false
}

// UseOnBlock places the dried ghast facing the user and starts its rehydration ticks.
func (d DriedGhast) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, d)
	if !used {
		return false
	}
	d.Facing = user.Rotation().Direction().Opposite()
	d.Hydration = 0
	place(tx, pos, d, user, ctx)
	if placed(ctx) {
		tx.ScheduleBlockUpdate(pos, d, driedGhastTickDelay)
	}
	return placed(ctx)
}

// ScheduledTick moves the dried ghast one step every 6000 ticks (the 26.60 DriedGhastBlock tick): waterlogged
// it rehydrates a level, and at the last level it turns into a ghastling, leaving the water behind; out of
// water it dries out a level until it is fully dry.
func (d DriedGhast) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	water := false
	if l, ok := tx.Liquid(pos); ok {
		_, water = l.(Water)
	}
	switch {
	case water && d.Hydration >= 3:
		if d.emerge(pos, tx) {
			return
		}
	case water:
		d.Hydration++
		tx.SetBlock(pos, d, nil)
	case d.Hydration > 0:
		d.Hydration--
		tx.SetBlock(pos, d, nil)
	}
	tx.ScheduleBlockUpdate(pos, d, driedGhastTickDelay)
}

// ghastlingHost is implemented by the MobHost of the mobs: it gives a ghastling that came out of a dried ghast
// its home at the block and plays its spawn sound.
type ghastlingHost interface {
	GhastlingEmerged(tx *world.Tx, ghastling world.Entity, home cube.Pos)
}

// emerge turns the dried ghast into a ghastling (a baby happy ghast, spawned with minecraft:spawn_baby) and
// removes the block. It reports false if there are no mobs to spawn.
func (d DriedGhast) emerge(pos cube.Pos, tx *world.Tx) bool {
	e, ok := SpawnMob(tx, "minecraft:happy_ghast", pos.Vec3Middle(), MobSpawnOptions{Event: "minecraft:spawn_baby"})
	if !ok {
		return false
	}
	if h, ok := Mobs().(ghastlingHost); ok {
		h.GhastlingEmerged(tx, e, pos)
	}
	tx.SetBlock(pos, nil, nil)
	return true
}

// HasLiquidDrops ...
func (DriedGhast) HasLiquidDrops() bool {
	return false
}

// SideClosed ...
func (DriedGhast) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// BreakInfo ...
func (d DriedGhast) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(DriedGhast{}))
}

// EncodeItem ...
func (DriedGhast) EncodeItem() (name string, meta int16) {
	return "minecraft:dried_ghast", 0
}

// EncodeBlock ...
func (d DriedGhast) EncodeBlock() (string, map[string]any) {
	return "minecraft:dried_ghast", map[string]any{
		"minecraft:cardinal_direction": d.Facing.String(),
		"rehydration_level":            int32(d.Hydration),
	}
}

// allDriedGhasts returns every state of a dried ghast.
func allDriedGhasts() (b []world.Block) {
	for _, f := range cube.Directions() {
		for h := 0; h <= 3; h++ {
			b = append(b, DriedGhast{Facing: f, Hydration: h})
		}
	}
	return
}

func init() {
	registerAll(allDriedGhasts())
	world.RegisterItem(DriedGhast{})
}
