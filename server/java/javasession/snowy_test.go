package javasession

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/java/javamap"
	jchunk "github.com/df-mc/dragonfly/server/java/protocol/chunk"
	"github.com/df-mc/dragonfly/server/java/protocol/version"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// TestSnowyGrassInChunks checks that Java clients get grass_block[snowy=true] under snow, also when the snow is in
// the section above, and snowy=false elsewhere.
func TestSnowyGrassInChunks(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	s := &Session{ver: version.Newest, dim: "minecraft:overworld"}
	s.blk = blocksFor(s.ver)
	c := chunk.New(world.DefaultBlockRegistry, cube.Range{-64, 319})
	set := func(x uint8, y int16, b world.Block) { c.SetBlock(x, y, 0, 0, world.BlockRuntimeID(b)) }
	set(0, 10, block.Grass{})
	set(0, 11, block.SnowLayer{})
	set(1, 63, block.Grass{}) // top of a section: the snow is in the next one
	set(1, 64, block.SnowLayer{Height: 7})
	set(2, 20, block.Grass{})
	set(3, 30, block.Podzol{})
	set(3, 31, block.Snow{})
	set(4, 40, block.Grass{})
	set(4, 41, block.Stone{})

	chunk.LightArea([]*chunk.Chunk{c}, 0, 0).Fill()

	grass := javamap.DefaultBlockStates()[world.BlockRuntimeID(block.Grass{})]
	snowyGrass, _ := javamap.Snowy(grass)
	snowyPodzol, _ := javamap.Snowy(javamap.DefaultBlockStates()[world.BlockRuntimeID(block.Podzol{})])
	col := s.column(world.ChunkPos{}, c)
	at := func(x int, y int) int32 {
		return int32(col.Sections[(y+64)>>4].Blocks[jchunk.BlockIndex(x, (y+64)&15, 0)])
	}
	for _, c := range []struct {
		x, y int
		want int32
	}{
		{0, 10, snowyGrass}, {1, 63, snowyGrass}, {2, 20, grass}, {3, 30, snowyPodzol}, {4, 40, grass},
	} {
		if got := at(c.x, c.y); got != c.want {
			t.Errorf("block at %d,%d: Java state %d, want %d", c.x, c.y, got, c.want)
		}
	}
	if got, want := at(0, 11), javamap.DefaultBlockStates()[world.BlockRuntimeID(block.SnowLayer{})]; got != want {
		t.Errorf("snow layer: %d, want %d", got, want)
	}
}
