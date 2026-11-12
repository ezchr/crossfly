package javasession

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// A border block turns its whole column into a wall (world.Tx.HasBorderBlock). Bedrock clients know the columns
// from the chunk payload and collide with them themselves. Java has no such block (it is shown as a barrier at its
// own position only), so a Java client walks into the column and the server takes it back out.

const (
	// borderShrink shrinks the moving box before it is clipped, so that a box resting flush against a column
	// (with a float error either way) is clipped by it rather than taken as already inside it.
	borderShrink = 1e-7
	// borderInside is how far a box must reach into a column to be inside it.
	borderInside = 1e-6
	// borderMaxColumns limits the columns a single move is checked against (a 64x64 area): longer moves are
	// teleports in all but name and are left alone.
	borderMaxColumns = 64 * 64
)

// clipBorderMove clips the move delta of box so that it does not enter a column for which border returns true,
// sliding along the column as block collisions do: X first, then Z. Y is never clipped, the columns span every
// height. A box that already reaches into a column (a border block placed on it, a teleport) is not clipped at all,
// so it walks out of the wall instead of being held or pulled back over and over.
func clipBorderMove(box cube.BBox, delta mgl64.Vec3, border func(x, z int) bool) mgl64.Vec3 {
	if delta[0] == 0 && delta[2] == 0 {
		return delta
	}
	area := box.Extend(delta)
	minX, minZ := int(math.Floor(area.Min()[0])), int(math.Floor(area.Min()[2]))
	maxX, maxZ := int(math.Floor(area.Max()[0])), int(math.Floor(area.Max()[2]))
	if (maxX-minX+1)*(maxZ-minZ+1) > borderMaxColumns {
		return delta
	}
	var cols []cube.BBox
	for x := minX; x <= maxX; x++ {
		for z := minZ; z <= maxZ; z++ {
			if !border(x, z) {
				continue
			}
			if inBorderColumn(box, x, z) {
				return delta // already in the wall: free to leave it, never pulled back
			}
			cols = append(cols, cube.Box(float64(x), area.Min()[1]-1, float64(z), float64(x+1), area.Max()[1]+1, float64(z+1)))
		}
	}
	if len(cols) == 0 {
		return delta
	}
	b := box.Grow(-borderShrink).Translate(mgl64.Vec3{0, delta[1]})
	dx, dz := delta[0], delta[2]
	for _, c := range cols {
		dx = b.XOffset(c, dx)
	}
	b = b.Translate(mgl64.Vec3{dx})
	for _, c := range cols {
		dz = b.ZOffset(c, dz)
	}
	out := mgl64.Vec3{dx, delta[1], dz}
	// Never end up in a column that was not entered before, whatever the float error.
	end := box.Translate(out)
	for _, c := range cols {
		if inBorderColumn(end, int(c.Min()[0]), int(c.Min()[2])) {
			return mgl64.Vec3{0, delta[1], 0}
		}
	}
	return out
}

// inBorderColumn reports whether box reaches into the column at x, z.
func inBorderColumn(box cube.BBox, x, z int) bool {
	return box.Min()[0] < float64(x+1)-borderInside && box.Max()[0] > float64(x)+borderInside &&
		box.Min()[2] < float64(z+1)-borderInside && box.Max()[2] > float64(z)+borderInside
}

// borderClip clips a move of e from its position by delta against the border columns of tx. It reports whether
// the move was changed. Entities without collision (spectators) pass through, as on Bedrock.
func borderClip(tx *world.Tx, e world.Entity, delta mgl64.Vec3) (mgl64.Vec3, bool) {
	if g, ok := e.(interface{ GameMode() world.GameMode }); ok && !g.GameMode().HasCollision() {
		return delta, false
	}
	box := e.H().Type().BBox(e).Translate(e.Position())
	clipped := clipBorderMove(box, delta, tx.HasBorderBlock)
	return clipped, clipped != delta
}

// Java player_position relative flags (RelativeMovement).
const (
	relativeYaw    = 1 << 3
	relativePitch  = 1 << 4
	relativeDeltaY = 1 << 6
)

// snapBack puts the client back where the server has the player after a move into a border column was clipped.
// The look and the vertical speed stay the client's own (relative, unchanged), so a player falling or jumping
// along the wall does not hitch. The horizontal speed into the wall is dropped.
func (s *Session) snapBack(c session.Controllable) {
	pos := c.Position()
	s.teleportFlags(pos[0], pos[1], pos[2], 0, 0, relativeYaw|relativePitch|relativeDeltaY)
}

// snapBackVehicle puts the vehicle a Java client drives back where the server has it (move_vehicle), with the
// client's own look.
func (s *Session) snapBackVehicle(pos mgl64.Vec3, yaw, pitch float32) {
	w := s.packet()
	w.Float64(pos[0])
	w.Float64(pos[1])
	w.Float64(pos[2])
	w.Float32(yaw)
	w.Float32(pitch)
	s.queue(v777.ClientboundPlayMoveVehicle, w)
}
