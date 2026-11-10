//go:build crossfly_full

package path

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Finder is a PathFinder: A* over the nodes of an Evaluator, visiting at most MaxVisitedNodes nodes.
type Finder struct {
	eval            *Evaluator
	MaxVisitedNodes int
	open            heap
	neighbours      [32]*Node
}

// NewFinder returns a path finder.
func NewFinder(e *Evaluator, maxVisited int) *Finder {
	return &Finder{eval: e, MaxVisitedNodes: maxVisited}
}

// FindPath is PathFinder.findPath, with Paper's list of targets (its order is the order targets are given in):
// the shortest path that reaches one of the targets within reachRange, or else the path that gets closest.
func (f *Finder) FindPath(tx *world.Tx, m Mob, targets []cube.Pos, maxPathLength float32, reachRange int, visitedMultiplier float32) *Path {
	f.open.clear()
	f.eval.prepare(tx, m)
	defer f.eval.done()
	from := f.eval.start()
	if from == nil {
		return nil
	}
	ts := make([]*target, len(targets))
	for i, p := range targets {
		n := f.eval.node(p[0], p[1], p[2])
		ts[i] = &target{Node: *newNode(n.X, n.Y, n.Z), pos: p, bestH: math.MaxFloat32}
	}
	return f.find(from, ts, maxPathLength, reachRange, visitedMultiplier)
}

func (f *Finder) find(from *Node, targets []*target, maxPathLength float32, reachRange int, visitedMultiplier float32) *Path {
	from.g = 0
	from.h = f.bestH(from, targets)
	from.f = from.h
	f.open.clear()
	f.open.insert(from)
	count := 0
	var reached []*target
	maxVisited := int(float32(f.MaxVisitedNodes) * visitedMultiplier)
	for !f.open.empty() {
		if count++; count >= maxVisited {
			break
		}
		cur := f.open.pop()
		cur.closed = true
		for _, t := range targets {
			if cur.distanceManhattan(cube.Pos{t.X, t.Y, t.Z}) <= float32(reachRange) {
				t.reached = true
				reached = append(reached, t)
			}
		}
		if len(reached) > 0 {
			break
		}
		if cur.distanceTo(from) >= maxPathLength {
			continue
		}
		n := f.eval.neighboursOf(f.neighbours[:], cur)
		for i := 0; i < n; i++ {
			nb := f.neighbours[i]
			d := cur.distanceTo(nb)
			nb.walkedDistance = cur.walkedDistance + d
			g := cur.g + d + nb.CostMalus
			if nb.walkedDistance < maxPathLength && (!nb.inOpenSet() || g < nb.g) {
				nb.cameFrom = cur
				nb.g = g
				nb.h = f.bestH(nb, targets) * 1.5
				if nb.inOpenSet() {
					f.open.changeCost(nb, nb.g+nb.h)
				} else {
					nb.f = nb.g + nb.h
					f.open.insert(nb)
				}
			}
		}
	}
	var best *Path
	if len(reached) > 0 {
		for _, t := range reached {
			p := reconstruct(t.best, t.pos, true)
			if best == nil || p.NodeCount() < best.NodeCount() {
				best = p
			}
		}
		return best
	}
	for _, t := range targets {
		p := reconstruct(t.best, t.pos, false)
		if best == nil || p.distToTarget < best.distToTarget || p.distToTarget == best.distToTarget && p.NodeCount() < best.NodeCount() {
			best = p
		}
	}
	return best
}

func (f *Finder) bestH(n *Node, targets []*target) float32 {
	best := float32(math.MaxFloat32)
	for _, t := range targets {
		h := n.distanceTo(&t.Node)
		t.updateBest(h, n)
		best = min(best, h)
	}
	return best
}

func reconstruct(closest *Node, target cube.Pos, reached bool) *Path {
	var nodes []*Node
	for n := closest; n != nil; n = n.cameFrom {
		nodes = append(nodes, n)
	}
	for i, j := 0, len(nodes)-1; i < j; i, j = i+1, j-1 {
		nodes[i], nodes[j] = nodes[j], nodes[i]
	}
	return newPath(nodes, target, reached)
}
