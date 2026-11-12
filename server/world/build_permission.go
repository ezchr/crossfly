package world

import (
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// BuildMarker is a block that decides whether players may build above it: the allow and deny blocks of Bedrock
// Edition (BDS Material types 35 and 36). BuildAllowed returns true for an allow block and false for a deny block.
type BuildMarker interface {
	Block
	BuildAllowed() bool
}

// BorderColumnBlock is a block that turns its whole column into a border: the border block of Bedrock Edition.
// Nothing collides through such a column and no one builds in it (BDS LevelChunk::setBorder, BlockSource::
// hasBorderBlock).
type BorderColumnBlock interface {
	Block
	BorderColumn()
}

// buildMarkersOn is set once a BuildMarker or BorderColumnBlock is registered (EnableBuildMarkers). Until then the
// checks below cost nothing: no world can hold such a block.
var buildMarkersOn atomic.Bool

// EnableBuildMarkers turns on the allow/deny/border block checks. The blocks call it when they register.
func EnableBuildMarkers() { buildMarkersOn.Store(true) }

// paletteHas reports whether the palette holds a block that f accepts.
func paletteHas(p *chunk.Palette, f func(Block) bool) bool {
	for i := 0; i < p.Len(); i++ {
		if b, ok := BlockByRuntimeID(p.Value(uint16(i))); ok && f(b) {
			return true
		}
	}
	return false
}

func isMarker(b Block) bool {
	_, ok := b.(BuildMarker)
	return ok
}

func isBorder(b Block) bool {
	_, ok := b.(BorderColumnBlock)
	return ok
}

// BuildPermitted reports whether the column scan of Bedrock Edition's allow and deny blocks lets a player build at
// pos, as BDS BlockSource::_getBlockPermissions does:
//   - at or above the top of the world: never;
//   - in a column holding a border block: never;
//   - below the bottom of the world: def;
//   - otherwise the blocks from pos down to the bottom are checked, pos included: the first allow block found
//     permits building, the first deny block found forbids it, and with neither def is returned.
//
// It only looks down, never up. def is true in BDS unless the world is immutable or the player is in adventure
// mode, in which case it is the player's privileged builder ability.
func (tx *Tx) BuildPermitted(pos cube.Pos, def bool) bool {
	if !buildMarkersOn.Load() {
		return def
	}
	r := tx.Range()
	if pos[1] > r[1] {
		return false
	}
	if tx.HasBorderBlock(pos[0], pos[2]) {
		return false
	}
	if pos[1] < r[0] {
		return def
	}
	c := tx.chunk(chunkPosFromBlockPos(pos))
	x, z := uint8(pos[0]), uint8(pos[2])
	y := pos[1]
	for y >= r[0] {
		sub := c.SubChunk(int16(y))
		subBase := int(c.SubY(c.SubIndex(int16(y))))
		if sub.Empty() || !paletteHas(sub.Layer(0).Palette(), isMarker) {
			y = subBase - 1
			continue
		}
		for ; y >= subBase && y >= r[0]; y-- {
			if b, ok := BlockByRuntimeID(c.Block(x, int16(y), z, 0)); ok {
				if m, ok := b.(BuildMarker); ok {
					return m.BuildAllowed()
				}
			}
		}
	}
	return def
}

// HasBorderBlock reports whether the column at x, z holds a border block anywhere between the bottom and the top
// of the world (BDS BlockSource::hasBorderBlock). A chunk that is not loaded has no border.
func (tx *Tx) HasBorderBlock(x, z int) bool {
	if !buildMarkersOn.Load() {
		return false
	}
	c, ok := tx.World().chunks[chunkPosFromBlockPos(cube.Pos{x, 0, z})]
	if !ok {
		return false
	}
	return columnHasBorder(c.Chunk, uint8(x), uint8(z))
}

func columnHasBorder(c *chunk.Chunk, x, z uint8) bool {
	for _, sub := range c.Sub() {
		if sub.Empty() || !paletteHas(sub.Layer(0).Palette(), isBorder) {
			continue
		}
		for y := uint8(0); y < 16; y++ {
			if b, ok := BlockByRuntimeID(sub.Block(x&15, y, z&15, 0)); ok && isBorder(b) {
				return true
			}
		}
	}
	return false
}

// BorderColumns returns the columns of the chunk that hold a border block, as the LevelChunk packet lists them
// (BDS LevelChunk::serializeBorderBlocks): one byte per column, x | z<<4, in increasing order.
func BorderColumns(c *chunk.Chunk) []byte {
	if !buildMarkersOn.Load() {
		return nil
	}
	found := false
	for _, sub := range c.Sub() {
		if !sub.Empty() && paletteHas(sub.Layer(0).Palette(), isBorder) {
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	var cols []byte
	for i := 0; i < 256; i++ {
		if columnHasBorder(c, uint8(i&15), uint8(i>>4)) {
			cols = append(cols, byte(i))
		}
	}
	return cols
}
