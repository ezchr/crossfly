//go:build crossfly_full

package path

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// The water-bound navigation (Kind WaterBound): WaterBoundPathNavigation with Paper's createPathFinder, and its
// SwimNodeEvaluator. Guardians, fish, squid-less water mobs and dolphins (which may breach) use it.

// NewWaterNavigation is WaterBoundPathNavigation for m. allowBreaching is Paper's `mob.is(EntityTypes.DOLPHIN)`:
// the path may go through the air just above the water, and the path is updated out of water too.
func NewWaterNavigation(m Mob, allowBreaching bool) *Navigation {
	n := NewNavigation(WaterBound, m)
	n.eval.swim, n.eval.breach = true, allowBreaching
	n.eval.SetCanPassDoors(false)
	return n
}

// swimDirections are Direction.values() in order: down, up, north, south, west, east.
var swimDirections = [6]cube.Pos{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

// swimStart is SwimNodeEvaluator.getStart.
func (e *Evaluator) swimStart() *Node {
	bb := e.mob.BBox()
	return e.node(int(math.Floor(bb.Min()[0])), int(math.Floor(bb.Min()[1]+0.5)), int(math.Floor(bb.Min()[2])))
}

// swimNeighbours is SwimNodeEvaluator.getNeighbors.
func (e *Evaluator) swimNeighbours(out []*Node, pos *Node) int {
	count := 0
	var nodes [6]*Node
	for i, d := range swimDirections {
		n := e.swimAcceptedNode(pos.X+d[0], pos.Y+d[1], pos.Z+d[2])
		nodes[i] = n
		if n != nil && !n.closed {
			out[count] = n
			count++
		}
	}
	// Plane.HORIZONTAL (north, east, south, west) with getClockWise; swimDirections index of each.
	idx := [4]int{2, 5, 3, 4}
	for i, d := range horizontal {
		d2 := horizontal[clockwise[i]]
		a, b := nodes[idx[i]], nodes[idx[clockwise[i]]]
		if a != nil && a.CostMalus >= 0 && b != nil && b.CostMalus >= 0 {
			n := e.swimAcceptedNode(pos.X+d[0]+d2[0], pos.Y, pos.Z+d[2]+d2[2])
			if n != nil && !n.closed {
				out[count] = n
				count++
			}
		}
	}
	return count
}

// swimAcceptedNode is SwimNodeEvaluator.findAcceptedNode.
func (e *Evaluator) swimAcceptedNode(x, y, z int) *Node {
	t := e.swimCachedType(x, y, z)
	if !(e.breach && t == Breach || t == Water) {
		return nil
	}
	cost := e.mob.PathfindingMalus(t)
	if cost < 0 {
		return nil
	}
	n := e.node(x, y, z)
	n.Type = t
	n.CostMalus = max(n.CostMalus, cost)
	if _, ok := e.tx.Liquid(cube.Pos{x, y, z}); !ok {
		n.CostMalus += 8
	}
	return n
}

func (e *Evaluator) swimCachedType(x, y, z int) Type {
	p := cube.Pos{x, y, z}
	if t, ok := e.typeCache[p]; ok {
		return t
	}
	t := e.swimTypeOfMob(e.tx, x, y, z)
	e.typeCache[p] = t
	return t
}

// swimTypeOfMob is SwimNodeEvaluator.getPathTypeOfMob: Breach where the box would stick out of the water into
// the air above it, Water where it is all in water, Blocked otherwise.
func (e *Evaluator) swimTypeOfMob(tx *world.Tx, x, y, z int) Type {
	var last cube.Pos
	for xx := x; xx < x+e.width; xx++ {
		for yy := y; yy < y+e.height; yy++ {
			for zz := z; zz < z+e.depth; zz++ {
				last = cube.Pos{xx, yy, zz}
				_, fluid := tx.Liquid(last)
				air := !last.OutOfBounds(tx.Range()) && infoOf(tx.BlockRuntimeIDAt(last)).kind == kindAir
				if !fluid && air && isWater(tx, last.Side(cube.FaceDown)) {
					return Breach
				}
				if !isWater(tx, last) {
					return Blocked
				}
			}
		}
	}
	if isWater(tx, last) {
		return Water
	}
	return Blocked
}

// waterStableDestination is WaterBoundPathNavigation.isStableDestination: the block is not a solid render block.
func waterStableDestination(tx *world.Tx, pos cube.Pos) bool {
	return !infoOf(tx.BlockRuntimeIDAt(pos)).solidRender
}

// StableDestination is isStableDestination of the navigation's kind.
func (n *Navigation) StableDestination(tx *world.Tx, pos cube.Pos) bool {
	if n.kind == WaterBound {
		return waterStableDestination(tx, pos)
	}
	return IsStableDestination(tx, pos)
}

// WalkTypeAt is WalkNodeEvaluator.getPathTypeStatic (GoalUtils.hasMalus asks it whatever the navigation is): the
// walk evaluator's type, also for a water-bound navigation.
func (n *Navigation) WalkTypeAt(m Mob, pos cube.Pos) Type {
	if !n.eval.swim {
		return n.TypeAt(m, pos)
	}
	if n.walk == nil {
		n.walk = NewEvaluator()
	}
	n.walk.prepare(m.Tx(), m)
	defer n.walk.done()
	return n.walk.TypeAt(pos[0], pos[1], pos[2])
}
