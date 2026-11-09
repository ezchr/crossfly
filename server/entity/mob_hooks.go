//go:build crossfly_full

package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// BlockBBoxesAround returns the world-space collision boxes of the blocks
// around box (box grown by 0.25), the boxes MovementComputer.CheckCollision
// collides with. It is exported for the mob physics in server/mob.
func BlockBBoxesAround(tx *world.Tx, box cube.BBox) []cube.BBox {
	return blockBBoxsAround(tx, box)
}

// AppendBlockBBoxesAround is BlockBBoxesAround appending to dst, so that a
// mob moving every tick reuses one buffer.
func AppendBlockBBoxesAround(dst []cube.BBox, tx *world.Tx, box cube.BBox) []cube.BBox {
	grown := box.Grow(0.25)
	min, max := grown.Min(), grown.Max()
	minX, minY, minZ := int(math.Floor(min[0])), int(math.Floor(min[1])), int(math.Floor(min[2]))
	maxX, maxY, maxZ := int(math.Ceil(max[0])), int(math.Ceil(max[1])), int(math.Ceil(max[2]))
	for y := minY; y < maxY; y++ {
		for x := minX; x < maxX; x++ {
			for z := minZ; z < maxZ; z++ {
				pos := cube.Pos{x, y, z}
				for _, b := range tx.Block(pos).Model().BBox(pos, tx) {
					dst = append(dst, b.Translate(mgl64.Vec3{float64(x), float64(y), float64(z)}))
				}
			}
		}
	}
	return dst
}
