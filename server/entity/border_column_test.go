//go:build crossfly_full

package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestBorderColumnCollision: server side collision (what Bedrock clients get from the chunk's border columns)
// stops an entity at a border column at every height and lets it slide along it.
func TestBorderColumnCollision(t *testing.T) {
	w := minecartWorld(t)
	doWorld(w, func(tx *world.Tx) {
		for z := -3; z <= 3; z++ {
			tx.SetBlock(cube.Pos{5, 0, z}, block.BorderBlock{Post: true}, nil)
		}
		e := boatAt(tx, mgl64.Vec3{3, 100, 10.5}, item.BoatWoods()[0], false)
		bb := e.H().Type().BBox(e)
		half := (bb.Max()[0] - bb.Min()[0]) / 2
		var mc MovementComputer
		// Far above the border block itself: still a wall.
		pos := mgl64.Vec3{5 - half - 0.1, 100, 0.5}
		d, vel := mc.CheckCollision(tx, e, pos, mgl64.Vec3{0.5, 0, 0})
		if d[0] > 0.1+1e-9 || vel[0] != 0 {
			t.Fatalf("into the column: delta %v vel %v", d, vel)
		}
		// Diagonally: X stops, Z slides.
		d, _ = mc.CheckCollision(tx, e, pos, mgl64.Vec3{0.5, 0, 0.5})
		if d[0] > 0.1+1e-9 || d[2] != 0.5 {
			t.Fatalf("along the column: delta %v", d)
		}
		// Past the end of the wall the columns are open.
		d, _ = mc.CheckCollision(tx, e, mgl64.Vec3{3, 100, 10.5}, mgl64.Vec3{0.5, 0, 0})
		if d[0] != 0.5 {
			t.Fatalf("open column: delta %v", d)
		}
	})
}
