//go:build crossfly_full

package physics

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// collision records which axes a move was stopped on.
type collision struct{ x, y, z bool }

// move moves box by vel against the block collision boxes and returns how
// far it got. Like SweptMovement::computeYXZMoveWithDepenetration it
// collides Y, then X, then Z. A mob that walks into a block while standing
// (or landing) also tries the move raised by its max auto step
// (AutoStepSystem: up, across, back down) and keeps whichever goes further
// horizontally.
func move(tx *world.Tx, s *state, box cube.BBox, vel mgl64.Vec3, onGround bool) (mgl64.Vec3, collision) {
	if !s.collision {
		return vel, collision{}
	}
	s.boxes = entity.AppendBlockBBoxesAround(s.boxes[:0], tx, box.Extend(vel))
	boxes := s.boxes
	d := collide(box, vel, boxes)
	c := collision{x: d[0] != vel[0], y: d[1] != vel[1], z: d[2] != vel[2]}
	if step := s.maxStep; step > 0 && (onGround || c.y && vel[1] < 0) && (c.x || c.z) {
		up := mgl64.Vec3{vel[0], step, vel[2]}
		s.stepBoxes = entity.AppendBlockBBoxesAround(s.stepBoxes[:0], tx, box.Extend(up).Extend(mgl64.Vec3{0, math.Min(vel[1], 0)}))
		stepBoxes := s.stepBoxes
		u := collide(box, up, stepBoxes)
		raised := box.Translate(u)
		dy := vel[1] - u[1]
		for _, b := range stepBoxes {
			dy = raised.YOffset(b, dy)
		}
		u[1] += dy
		if u[0]*u[0]+u[2]*u[2] > d[0]*d[0]+d[2]*d[2] {
			d = u
			c = collision{x: d[0] != vel[0], y: d[1] != vel[1], z: d[2] != vel[2]}
		}
	}
	return d, c
}

// collide returns how far box moves by vel along Y, X, Z before hitting one
// of boxes.
func collide(box cube.BBox, vel mgl64.Vec3, boxes []cube.BBox) mgl64.Vec3 {
	dx, dy, dz := vel[0], vel[1], vel[2]
	if dy != 0 {
		for _, b := range boxes {
			dy = box.YOffset(b, dy)
		}
		box = box.Translate(mgl64.Vec3{0, dy})
	}
	if dx != 0 {
		for _, b := range boxes {
			dx = box.XOffset(b, dx)
		}
		box = box.Translate(mgl64.Vec3{dx})
	}
	if dz != 0 {
		for _, b := range boxes {
			dz = box.ZOffset(b, dz)
		}
	}
	return mgl64.Vec3{dx, dy, dz}
}

// free reports whether box touches no block collision box and no liquid
// (Java isFree; MobMovementClimbOutOfLiquid tests the same).
func free(tx *world.Tx, box cube.BBox) bool {
	for _, b := range entity.BlockBBoxesAround(tx, box) {
		if b.IntersectsWith(box) {
			return false
		}
	}
	min, max := box.Min(), box.Max()
	for x := int(math.Floor(min[0])); x <= int(math.Floor(max[0])); x++ {
		for y := int(math.Floor(min[1])); y <= int(math.Floor(max[1])); y++ {
			for z := int(math.Floor(min[2])); z <= int(math.Floor(max[2])); z++ {
				if _, ok := tx.Liquid(cube.Pos{x, y, z}); ok {
					return false
				}
			}
		}
	}
	return true
}

// liquidHeight is the height of the liquid surface in its block: full when
// falling or below the same liquid, depth/9 otherwise (source 8/9).
func liquidHeight(tx *world.Tx, pos cube.Pos, l world.Liquid) float64 {
	if l.LiquidFalling() {
		return 1
	}
	if a, ok := tx.Liquid(pos.Side(cube.FaceUp)); ok && a.LiquidType() == l.LiquidType() {
		return 1
	}
	return float64(l.LiquidDepth()) / 9
}

// liquidState reports whether the mob's box is in water or lava and whether
// its eyes are under water. As in Bedrock's (and old Java's) checks, the box
// is first shrunk by 0.4 at the top and bottom (by 0.1 sideways for lava),
// so a mob only counts as in water once the water reaches 0.4 up its body.
// source: java 1.12 Entity.handleWaterMovement / isInLava.
func liquidState(tx *world.Tx, box cube.BBox, eye mgl64.Vec3) (water, lava, underwater bool) {
	min, max := box.Min(), box.Max()
	yLo, yHi := min[1]+0.4, max[1]-0.4
	if yHi < yLo {
		yLo = (min[1] + max[1]) / 2
		yHi = yLo
	}
	for x := int(math.Floor(min[0] + 0.001)); x <= int(math.Floor(max[0]-0.001)); x++ {
		for z := int(math.Floor(min[2] + 0.001)); z <= int(math.Floor(max[2]-0.001)); z++ {
			for y := int(math.Floor(yLo)); y <= int(math.Floor(yHi)); y++ {
				pos := cube.Pos{x, y, z}
				l, ok := tx.Liquid(pos)
				if !ok {
					continue
				}
				top := float64(y) + liquidHeight(tx, pos, l)
				if top <= yLo {
					continue
				}
				switch l.(type) {
				case block.Water:
					water = true
				case block.Lava:
					// Lava uses a box 0.1 narrower on each side.
					if float64(x+1) > min[0]+0.1 && float64(x) < max[0]-0.1 && float64(z+1) > min[2]+0.1 && float64(z) < max[2]-0.1 {
						lava = true
					}
				}
			}
		}
	}
	// Java isEyeInFluid: eyes 1/9 below the surface.
	ey := eye[1] - 0.11111111
	epos := cube.Pos{int(math.Floor(eye[0])), int(math.Floor(ey)), int(math.Floor(eye[2]))}
	if l, ok := tx.Liquid(epos); ok {
		if _, w := l.(block.Water); w && ey < float64(epos[1])+liquidHeight(tx, epos, l) {
			underwater = true
		}
	}
	return water, lava, underwater
}

// pushEntities is Mob::pushActors: m pushes the pushable mobs whose boxes
// touch its box grown by 0.2 sideways, and players push m. Each pair moves
// apart by 0.05 x (1 - push_through) per tick, less when far apart
// (PushableComponent::push); a mob that is not pushable pushes others but
// does not move.
func pushEntities(m *mob.Mob, s *state) {
	tx := m.Tx()
	area := m.BBox().GrowVec3(mgl64.Vec3{0.2, 0, 0.2})
	search := area.GrowVec3(mgl64.Vec3{2, 2, 2})
	self := m.H()
	for e := range tx.EntitiesWithin(search) {
		if e.H() == self {
			continue
		}
		if !e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(area) {
			continue
		}
		if p, ok := e.(*player.Player); ok {
			// The player's own pushActors pushes the mob, never itself.
			if s.pushable && p.GameMode().HasCollision() && !p.Dead() {
				if d := pushVector(m.Position(), p.Position(), 0); d != (mgl64.Vec3{}) {
					m.SetVelocity(m.Velocity().Add(d))
					m.SyncMovement()
				}
			}
			continue
		}
		o, ok := mob.Of(e)
		if !ok || o.Dead() {
			continue
		}
		os := stateOf(o)
		if !os.pushable {
			continue
		}
		d := pushVector(m.Position(), o.Position(), os.pushThrough)
		if d == (mgl64.Vec3{}) {
			continue
		}
		// A push shows both mobs to Java viewers at once (Entity.push sets
		// needsSync).
		o.SetVelocity(o.Velocity().Sub(d))
		o.SyncMovement()
		if s.pushable {
			m.SetVelocity(m.Velocity().Add(d))
			m.SyncMovement()
		}
	}
}

// pushVector is the push on an entity at self away from one at other.
func pushVector(self, other mgl64.Vec3, pushThrough float64) mgl64.Vec3 {
	dx, dz := self[0]-other[0], self[2]-other[2]
	g := math.Max(math.Abs(dx), math.Abs(dz))
	if g < 0.01 {
		return mgl64.Vec3{}
	}
	g = math.Sqrt(g)
	inv := math.Min(1/g, 1)
	k := inv * 0.05 * (1 - pushThrough) / g
	return mgl64.Vec3{dx * k, 0, dz * k}
}
