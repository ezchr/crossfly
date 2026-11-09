//go:build crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"math"
)

// pistonClipMovement clips a forced displacement against stationary block collisions. Moving blocks in the same
// piston operation are ignored, matching Java's temporary no-clip direction for piston collision resolution.
func pistonClipMovement(box cube.BBox, move mgl64.Vec3, tx *world.Tx, base cube.Pos) mgl64.Vec3 {
	swept := box.Extend(move).Grow(.01)
	lo, hi := swept.Min(), swept.Max()
	for x := int(math.Floor(lo[0])); x <= int(math.Floor(hi[0])); x++ {
		for y := int(math.Floor(lo[1])); y <= int(math.Floor(hi[1])); y++ {
			for z := int(math.Floor(lo[2])); z <= int(math.Floor(hi[2])); z++ {
				at := cube.Pos{x, y, z}
				b := tx.Block(at)
				if m, ok := b.(MovingPiston); ok && m.Piston == base {
					continue
				}
				if p, ok := b.(Piston); ok && p.State == 3 && at == base {
					continue
				}
				for _, collision := range b.Model().BBox(at, tx) {
					collision = collision.Translate(at.Vec3())
					move[0] = box.XOffset(collision, move[0])
					move[1] = box.YOffset(collision, move[1])
					move[2] = box.ZOffset(collision, move[2])
				}
			}
		}
	}
	return move
}
