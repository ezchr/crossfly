package block

import (
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// explosionGrid holds what an explosion found out about the blocks in its box: what its rays found (resistance,
// whether a block stops a ray, whether it is in the blast) and, while the exposure of one entity is worked out, the
// blocks its rays passed. Looking a block up is an index into the box instead of a map lookup.
type explosionGrid struct {
	min        cube.Pos
	dx, dy, dz int
	cells      []explosionCell
	// rayGen and blockGen mark the cells filled for this explosion and this exposure: cells of an earlier one are
	// ignored, so the grid is not cleared between uses.
	rayGen, blockGen uint32
}

// explosionCell is a block in an explosion's box.
type explosionCell struct {
	rayGen, blockGen uint32
	info             explosionBlockInfo
	block            world.Block
}

// explosionGridLimit is the most blocks a box may hold to be kept in a grid. A bigger explosion uses a map.
const explosionGridLimit = 1 << 16

var explosionGrids = sync.Pool{New: func() any { return &explosionGrid{} }}

// newExplosionGrid returns a grid for the box from min to max (inclusive), or nil if the box is too big.
func newExplosionGrid(min, max cube.Pos) *explosionGrid {
	dx, dy, dz := max[0]-min[0]+1, max[1]-min[1]+1, max[2]-min[2]+1
	if dx <= 0 || dy <= 0 || dz <= 0 || dx*dy*dz > explosionGridLimit {
		return nil
	}
	g := explosionGrids.Get().(*explosionGrid)
	g.min, g.dx, g.dy, g.dz = min, dx, dy, dz
	if n := dx * dy * dz; cap(g.cells) < n {
		g.cells = make([]explosionCell, n)
		g.rayGen, g.blockGen = 0, 0
	} else {
		g.cells = g.cells[:n]
	}
	g.rayGen++
	if g.rayGen == 0 {
		// Wrapped around: old cells could look filled.
		clear(g.cells[:cap(g.cells)])
		g.rayGen, g.blockGen = 1, 0
	}
	return g
}

// release returns the grid for reuse. The blocks it holds are dropped.
func (g *explosionGrid) release() {
	if g == nil {
		return
	}
	for i := range g.cells {
		g.cells[i].block = nil
	}
	explosionGrids.Put(g)
}

// cell returns the cell of pos, or nil if pos is outside the box.
func (g *explosionGrid) cell(pos cube.Pos) *explosionCell {
	x, y, z := pos[0]-g.min[0], pos[1]-g.min[1], pos[2]-g.min[2]
	if x < 0 || y < 0 || z < 0 || x >= g.dx || y >= g.dy || z >= g.dz {
		return nil
	}
	return &g.cells[(x*g.dy+y)*g.dz+z]
}

// nextExposure starts the block lookups of a new exposure.
func (g *explosionGrid) nextExposure() {
	g.blockGen++
	if g.blockGen == 0 {
		for i := range g.cells {
			g.cells[i].blockGen = 0
		}
		g.blockGen = 1
	}
}

// block returns the block at pos as tx.Block does, looked up once during an exposure.
func (g *explosionGrid) block(tx *world.Tx, pos cube.Pos) world.Block {
	if g == nil {
		return tx.Block(pos)
	}
	c := g.cell(pos)
	if c == nil {
		return tx.Block(pos)
	}
	if c.blockGen != g.blockGen {
		c.block, c.blockGen = tx.Block(pos), g.blockGen
	}
	return c.block
}
