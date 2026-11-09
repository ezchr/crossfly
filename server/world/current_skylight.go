//go:build crossfly_full

package world

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// CurrentSkyLight samples current geometry rather than the chunk's load-time light cache. Daylight detectors need
// roofs built or removed since chunk load to take effect. A per-column snapshot is invalidated by filtering changes
// in that column or its neighbours; ordinary sensor power updates do not invalidate it.
func (tx *Tx) CurrentSkyLight(pos cube.Pos) uint8 {
	if pos[1] < tx.Range()[0] {
		return 0
	}
	if pos[1] > tx.Range()[1] {
		return 15
	}
	at := chunkPosFromBlockPos(pos)
	centre := tx.chunk(at)
	if centre.currentSky == nil {
		copies := make([]*chunk.Chunk, 0, 9)
		for z := int32(-1); z <= 1; z++ {
			for x := int32(-1); x <= 1; x++ {
				copies = append(copies, tx.chunk(ChunkPos{at[0] + x, at[1] + z}).Chunk.Clone())
			}
		}
		// Only the snapshot is relit: rendering caches and other chunks are never overwritten with edge light from an
		// incomplete area. One chunk of padding covers skylight's maximum lateral reach of fifteen blocks.
		for i, c := range copies {
			chunk.LightArea([]*chunk.Chunk{c}, int(at[0])-1+i%3, int(at[1])-1+i/3).Fill()
		}
		chunk.LightArea(copies, int(at[0])-1, int(at[1])-1).Spread()
		centre.currentSky = copies[4]
	}
	return centre.currentSky.SkyLight(uint8(pos[0]), int16(pos[1]), uint8(pos[2]))
}
func (w *World) invalidateCurrentSky(at ChunkPos) {
	for z := int32(-1); z <= 1; z++ {
		for x := int32(-1); x <= 1; x++ {
			if col, ok := w.chunks[ChunkPos{at[0] + x, at[1] + z}]; ok {
				col.currentSky = nil
			}
		}
	}
}
