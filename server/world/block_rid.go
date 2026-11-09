package world

import "github.com/df-mc/dragonfly/server/block/cube"

// BlockRuntimeIDAt returns the runtime ID of the block at pos in the World's
// block registry (BlockRuntimeID of Block(pos), the first layer), without
// making the Block: block searches over many positions test the runtime ID
// first. Positions out of the World's range are air.
func (tx *Tx) BlockRuntimeIDAt(pos cube.Pos) uint32 {
	w := tx.World()
	if pos.OutOfBounds(w.ra) {
		return w.conf.Blocks.BlockRuntimeID(w.conf.Blocks.Air())
	}
	return tx.chunk(chunkPosFromBlockPos(pos)).Block(uint8(pos[0]), int16(pos[1]), uint8(pos[2]), 0)
}

// SectionMayHold reports whether the 16x16x16 section of the chunk holding
// pos may hold a block (first layer) whose runtime ID is set in ids. It is
// false only if none in the section's palette is, so a block search can
// skip the section.
func (tx *Tx) SectionMayHold(pos cube.Pos, ids []bool) bool {
	w := tx.World()
	has := func(rid uint32) bool { return int(rid) < len(ids) && ids[rid] }
	if pos.OutOfBounds(w.ra) {
		return has(w.conf.Blocks.BlockRuntimeID(w.conf.Blocks.Air()))
	}
	sub := tx.chunk(chunkPosFromBlockPos(pos)).SubChunk(int16(pos[1]))
	layers := sub.Layers()
	if len(layers) == 0 {
		return has(w.conf.Blocks.BlockRuntimeID(w.conf.Blocks.Air()))
	}
	p := layers[0].Palette()
	for i := range p.Len() {
		if has(p.Value(uint16(i))) {
			return true
		}
	}
	return false
}
