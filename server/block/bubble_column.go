//go:build crossfly_full

package block

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// BubbleColumn is a column of bubbles in water above soul sand (pushing up) or a magma block (pulling down). It
// holds a water source in its liquid layer. Java Edition's BubbleColumnBlock: the column grows from its bottom block
// a second after water lands on it, rebuilds itself five ticks after the block under it changes and pushes or pulls
// the entities in it (entity.javaItemBlockEffects for items).
type BubbleColumn struct {
	empty
	transparent
	sourceWaterDisplacer

	// DragDown is true over a magma block: entities are pulled down instead of pushed up.
	DragDown bool
}

// ReplaceableBy ...
func (BubbleColumn) ReplaceableBy(world.Block) bool { return true }

// HasLiquidDrops ...
func (BubbleColumn) HasLiquidDrops() bool { return false }

// SideClosed ...
func (BubbleColumn) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool { return false }

// EncodeBlock ...
func (c BubbleColumn) EncodeBlock() (string, map[string]any) {
	return "minecraft:bubble_column", map[string]any{"drag_down": boolByte(c.DragDown)}
}

// Hash ...
func (c BubbleColumn) Hash() (uint64, uint64) {
	return hashBubbleColumn, uint64(boolByte(c.DragDown))
}

// NeighbourUpdateTick is Java's BubbleColumnBlock.updateShape: the column rebuilds itself five ticks after the block
// under it changes, when it can no longer stand, or when a water source above it could take a column.
func (c BubbleColumn) NeighbourUpdateTick(pos, changed cube.Pos, tx *world.Tx) {
	above := pos.Side(cube.FaceUp)
	if !bubbleColumnSupport(tx.Block(pos.Side(cube.FaceDown))) || changed == pos.Side(cube.FaceDown) ||
		changed == above && !isBubbleColumn(tx.Block(above)) && bubbleColumnCanOccupy(tx.Block(above)) {
		// Java keeps one tick per block type: schedule both states, the one the column has then runs.
		for _, drag := range []bool{false, true} {
			tx.ScheduleBlockUpdate(pos, BubbleColumn{DragDown: drag}, time.Second/4)
		}
	}
}

// ScheduledTick ...
func (c BubbleColumn) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	updateBubbleColumn(tx, pos, tx.Block(pos.Side(cube.FaceDown)))
}

// bubbleColumnSupport reports whether a bubble column can stand on b (BubbleColumnBlock.canSurvive).
func bubbleColumnSupport(b world.Block) bool {
	switch b.(type) {
	case BubbleColumn, SoulSand, Magma:
		return true
	}
	return false
}

func isBubbleColumn(b world.Block) bool {
	_, ok := b.(BubbleColumn)
	return ok
}

// bubbleColumnCanOccupy is BubbleColumnBlock.canOccupy: a bubble column or a still water source.
func bubbleColumnCanOccupy(b world.Block) bool {
	switch w := b.(type) {
	case BubbleColumn:
		return true
	case Water:
		return w.Depth == 8 && !w.Falling
	}
	return false
}

// updateBubbleColumn is BubbleColumnBlock.updateColumn: the block at pos and every block above it that can take a
// column become a column of the kind the block below gives (or water again, if it gives none).
func updateBubbleColumn(tx *world.Tx, pos cube.Pos, below world.Block) {
	if !bubbleColumnCanOccupy(tx.Block(pos)) {
		return
	}
	var column world.Block
	switch b := below.(type) {
	case BubbleColumn:
		column = b
	case SoulSand:
		column = BubbleColumn{}
	case Magma:
		column = BubbleColumn{DragDown: true}
	default:
		if isBubbleColumn(tx.Block(pos)) {
			column = Water{Depth: 8, Still: true}
		} else {
			column = tx.Block(pos)
		}
	}
	for at := pos; at[1] <= tx.Range()[1] && bubbleColumnCanOccupy(tx.Block(at)); at = at.Side(cube.FaceUp) {
		if tx.Block(at) == column {
			return
		}
		setBubbleColumn(tx, at, column)
	}
}

// setBubbleColumn sets a column (with its water source) or plain water at pos.
func setBubbleColumn(tx *world.Tx, pos cube.Pos, b world.Block) {
	if isBubbleColumn(b) {
		tx.SetBlock(pos, b, nil)
		tx.SetLiquid(pos, Water{Depth: 8, Still: true})
		return
	}
	tx.SetLiquid(pos, nil)
	tx.SetBlock(pos, b, nil)
}

func allBubbleColumns() []world.Block {
	return []world.Block{BubbleColumn{}, BubbleColumn{DragDown: true}}
}

// NeighbourUpdateTick is SoulSandBlock.updateShape: water landing on soul sand starts a bubble column a second later.
func (s SoulSand) NeighbourUpdateTick(pos, changed cube.Pos, tx *world.Tx) {
	scheduleBubbleColumnBase(s, pos, changed, tx)
}

// ScheduledTick ...
func (s SoulSand) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if world.JavaRedstone() {
		updateBubbleColumn(tx, pos.Side(cube.FaceUp), s)
	}
}

// NeighbourUpdateTick is MagmaBlock.updateShape: water landing on a magma block starts a bubble column a second later.
func (m Magma) NeighbourUpdateTick(pos, changed cube.Pos, tx *world.Tx) {
	scheduleBubbleColumnBase(m, pos, changed, tx)
}

// ScheduledTick ...
func (m Magma) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if world.JavaRedstone() {
		updateBubbleColumn(tx, pos.Side(cube.FaceUp), m)
	}
}

// scheduleBubbleColumnBase schedules the tick of a bubble column base (soul sand, magma) when water is now above it.
func scheduleBubbleColumnBase(b world.Block, pos, changed cube.Pos, tx *world.Tx) {
	if !world.JavaRedstone() || changed != pos.Side(cube.FaceUp) {
		return
	}
	if _, ok := tx.Block(changed).(Water); ok {
		tx.ScheduleBlockUpdate(pos, b, time.Second)
	}
}
