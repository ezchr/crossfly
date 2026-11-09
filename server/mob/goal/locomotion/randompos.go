//go:build crossfly_full

package locomotion

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/goal/core"
	"github.com/go-gl/mathgl/mgl64"
)

// posQuery is one RandomPos::generateRandomPos call.
type posQuery struct {
	xz, y, yOff int
	// view, when set, flips candidates behind the mob (dx*view.x + dz*view.z
	// < 0).
	view *mgl64.Vec3
	// fly and swim skip the ground snap (canFly / isSwimmer).
	fly, swim bool
	// hover raises a snapped candidate by up to hover blocks (hover_height).
	hover floatRange
	tries int
}

// randomPos is RandomPos::generateRandomPos with the hover height and
// canFly/swimmer switches random_hover and random_fly need: tries candidates
// around the mob's block, flipped to face view, snapped to the ground unless
// the mob flies, swims or is in a liquid, then raised by hover_height; the
// best walk target value wins. The home restriction is not applied.
func randomPos(m *mob.Mob, q posQuery) (cube.Pos, bool) {
	if q.tries <= 0 || q.xz < 1 {
		return cube.Pos{}, false
	}
	tx := m.Tx()
	base := cube.PosFromVec3(m.Position())
	liquid := m.Physics().InWater || m.Physics().InLava
	r := m.Rand()
	best, found := math.Inf(-1), false
	var bestPos cube.Pos
	for i := 0; i < q.tries; i++ {
		dx := r.IntN(2*q.xz+1) - q.xz
		dz := r.IntN(2*q.xz+1) - q.xz
		dy := r.IntN(2*q.y+1) + q.yOff - q.y
		if q.view != nil && float64(dx)*q.view[0]+float64(dz)*q.view[2] < 0 {
			dx, dz = -dx, -dz
		}
		c := base.Add(cube.Pos{dx, dy, dz})
		if !inRange(tx, c) {
			continue
		}
		if !(q.swim || q.fly || liquid) {
			var ok bool
			if c, ok = snapToGround(m, c, base[1]-q.y, base[1]+q.y); !ok {
				continue
			}
			if h := q.hover.rollInt(m); h > 0 {
				// RandomPos::snapToGround raises the position up to h blocks
				// and fails when a solid block is in the way.
				blocked := false
				for j := 0; j < h; j++ {
					up := c.Side(cube.FaceUp)
					if blocksMotion(tx, up) {
						blocked = true
						break
					}
					c = up
				}
				if blocked {
					continue
				}
			}
		}
		if q.swim && !waterAt(tx, c) {
			// NavigationUtility::isStableDestination for swimmers.
			continue
		}
		if v := core.WalkTargetValue(m, c); v > best {
			best, bestPos, found = v, c, true
		}
	}
	return bestPos, found
}

// snapToGround moves pos to the first free block standing on a solid one,
// searching down to lo, then up to hi.
func snapToGround(m *mob.Mob, pos cube.Pos, lo, hi int) (cube.Pos, bool) {
	tx := m.Tx()
	solid := func(p cube.Pos) bool { return blocksMotion(tx, p) }
	if solid(pos) && !solid(pos.Side(cube.FaceUp)) {
		return pos.Side(cube.FaceUp), true
	}
	p, prev := pos, solid(pos.Side(cube.FaceUp))
	for p[1] >= lo && p[1] >= tx.Range()[0] {
		s := solid(p)
		if s && !prev {
			return p.Side(cube.FaceUp), true
		}
		prev = s
		p[1]--
	}
	p, below := pos, solid(pos)
	for p[1] < hi && p[1] < tx.Range()[1] {
		p[1]++
		s := solid(p)
		if !s && below {
			return p, true
		}
		below = s
	}
	return pos, false
}

// canFlyFlag reports minecraft:can_fly (Actor::canFly).
func canFlyFlag(m *mob.Mob) bool { return m.HasComponent("minecraft:can_fly") }

// isSwimmer reports whether m paths as a swimmer (navigation.swim, or
// generic in water).
func isSwimmer(m *mob.Mob) bool {
	return m.HasComponent("minecraft:navigation.swim") || (m.HasComponent("minecraft:navigation.generic") && m.Physics().InWater)
}
