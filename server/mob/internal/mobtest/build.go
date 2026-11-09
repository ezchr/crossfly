//go:build crossfly_full

package mobtest

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// The builders take a transaction (use them inside World.Do) and the
// *World so Reload knows which chunks to load again; pass nil to skip that.

func boxAround(p mgl64.Vec3, r float64) cube.BBox {
	return cube.Box(p[0]-r, p[1]-r, p[2]-r, p[0]+r, p[1]+r, p[2]+r)
}

func (w *World) mark(a, b cube.Pos) {
	if w != nil {
		w.Mark(a, b)
	}
}

// Fill sets every block in the box between a and b (inclusive) to b.
func (w *World) Fill(tx *world.Tx, a, b cube.Pos, blk world.Block) {
	lo := cube.Pos{min(a[0], b[0]), min(a[1], b[1]), min(a[2], b[2])}
	hi := cube.Pos{max(a[0], b[0]), max(a[1], b[1]), max(a[2], b[2])}
	for x := lo[0]; x <= hi[0]; x++ {
		for y := lo[1]; y <= hi[1]; y++ {
			for z := lo[2]; z <= hi[2]; z++ {
				tx.SetBlock(cube.Pos{x, y, z}, blk, nil)
			}
		}
	}
	w.mark(lo, hi)
}

// Clear fills the box with air.
func (w *World) Clear(tx *world.Tx, a, b cube.Pos) { w.Fill(tx, a, b, block.Air{}) }

// Floor builds a square floor of blk at height y from -r to r on x and z.
// Mobs stand on it at y+1.
func (w *World) Floor(tx *world.Tx, y, r int, blk world.Block) {
	w.Fill(tx, cube.Pos{-r, y, -r}, cube.Pos{r, y, r}, blk)
}

// Wall builds a wall of blk from a to b (a straight or diagonal line on x/z
// at a's height), height blocks tall.
func (w *World) Wall(tx *world.Tx, a, b cube.Pos, height int, blk world.Block) {
	w.Fill(tx, a, cube.Pos{b[0], a[1] + height - 1, b[2]}, blk)
}

// Platform builds a solid block of blk with its top surface at a.Y()+height-1,
// from a to b on x/z. Mobs stand on it at a.Y()+height.
func (w *World) Platform(tx *world.Tx, a, b cube.Pos, height int, blk world.Block) {
	w.Fill(tx, a, cube.Pos{b[0], a[1] + height - 1, b[2]}, blk)
}

// Stairs builds a staircase of steps stair blocks climbing in dir, starting
// at base (the first, lowest step), width blocks wide (extending to the right
// of dir), with fill under each step. It returns the position on top of the
// last step: a mob standing there is steps blocks higher than base.Y().
func (w *World) Stairs(tx *world.Tx, base cube.Pos, dir cube.Direction, steps, width int, stair, fill world.Block) cube.Pos {
	if width < 1 {
		width = 1
	}
	right := cube.Pos{}.Side(dir.RotateRight().Face())
	fwd := cube.Pos{}.Side(dir.Face())
	var last cube.Pos
	for i := range steps {
		for j := range width {
			p := base.Add(cube.Pos{fwd[0] * i, i, fwd[2] * i}).Add(cube.Pos{right[0] * j, 0, right[2] * j})
			for y := base[1]; y < p[1]; y++ {
				tx.SetBlock(cube.Pos{p[0], y, p[2]}, fill, nil)
			}
			tx.SetBlock(p, block.Stairs{Block: stair, Facing: dir}, nil)
			w.mark(cube.Pos{p[0], base[1], p[2]}, p)
			if j == 0 {
				last = p
			}
		}
	}
	return last.Side(cube.FaceUp)
}

// Water fills the box with still water sources.
func (w *World) Water(tx *world.Tx, a, b cube.Pos) {
	w.Fill(tx, a, b, block.Water{Still: true, Depth: 8})
}

// Grass is a stone-backed grass floor at y (dirt under it), for grazing
// and passive spawning tests.
func (w *World) Grass(tx *world.Tx, y, r int) {
	w.Fill(tx, cube.Pos{-r, y - 1, -r}, cube.Pos{r, y - 1, r}, block.Dirt{})
	w.Fill(tx, cube.Pos{-r, y, -r}, cube.Pos{r, y, r}, block.Grass{})
}

// Arena is the common start: a stone floor at y 0 from -r to r with barrier
// walls (glass, so mobs can see through) wallHeight tall around it. Mobs
// stand at y 1.
func (w *World) Arena(tx *world.Tx, r, wallHeight int) {
	w.Floor(tx, 0, r+1, block.Stone{})
	if wallHeight <= 0 {
		return
	}
	g := block.Glass{}
	w.Wall(tx, cube.Pos{-r - 1, 1, -r - 1}, cube.Pos{r + 1, 1, -r - 1}, wallHeight, g)
	w.Wall(tx, cube.Pos{-r - 1, 1, r + 1}, cube.Pos{r + 1, 1, r + 1}, wallHeight, g)
	w.Wall(tx, cube.Pos{-r - 1, 1, -r - 1}, cube.Pos{-r - 1, 1, r + 1}, wallHeight, g)
	w.Wall(tx, cube.Pos{r + 1, 1, -r - 1}, cube.Pos{r + 1, 1, r + 1}, wallHeight, g)
}

// Center is the middle of the top face of the block at p: where an entity
// standing on p is placed.
func Center(p cube.Pos) mgl64.Vec3 {
	return mgl64.Vec3{float64(p[0]) + 0.5, float64(p[1]) + 1, float64(p[2]) + 0.5}
}

// At is the middle of block p at its bottom: where an entity standing in p
// (on the block below) is placed.
func At(p cube.Pos) mgl64.Vec3 {
	return mgl64.Vec3{float64(p[0]) + 0.5, float64(p[1]), float64(p[2]) + 0.5}
}
