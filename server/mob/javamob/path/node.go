//go:build crossfly_full

// Package path is Java Edition's mob pathfinding (26.3, as on Paper): net.minecraft.world.level.pathfinder
// (Node, BinaryHeap, Path, PathType, WalkNodeEvaluator, PathFinder) and net.minecraft.world.entity.ai.navigation
// (PathNavigation, GroundPathNavigation, WallClimberNavigation). Package javamob's mobs use it through the Mob
// interface (mob.go).
package path

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

// Node is a pathfinder node (Node.java). Costs are float32 like Java's, so ties break the same way.
type Node struct {
	X, Y, Z  int
	heapIdx  int
	g, h, f  float32
	cameFrom *Node
	closed   bool
	// walkedDistance is the path length from the start; CostMalus the malus of the node's path type.
	walkedDistance float32
	CostMalus      float32
	Type           Type
}

func newNode(x, y, z int) *Node { return &Node{X: x, Y: y, Z: z, heapIdx: -1, Type: Blocked} }

// Pos is the node's block position.
func (n *Node) Pos() cube.Pos { return cube.Pos{n.X, n.Y, n.Z} }

// cloneAndMove is Node.cloneAndMove.
func (n *Node) cloneAndMove(x, y, z int) *Node {
	c := newNode(x, y, z)
	c.heapIdx, c.g, c.h, c.f = n.heapIdx, n.g, n.h, n.f
	c.cameFrom, c.closed, c.walkedDistance, c.CostMalus, c.Type = n.cameFrom, n.closed, n.walkedDistance, n.CostMalus, n.Type
	return c
}

func (n *Node) inOpenSet() bool { return n.heapIdx >= 0 }

// distanceTo is Node.distanceTo.
func (n *Node) distanceTo(o *Node) float32 {
	xd, yd, zd := float32(o.X-n.X), float32(o.Y-n.Y), float32(o.Z-n.Z)
	return float32(math.Sqrt(float64(xd*xd + yd*yd + zd*zd)))
}

// distanceToPos is Node.distanceTo(BlockPos).
func (n *Node) distanceToPos(p cube.Pos) float32 {
	xd, yd, zd := float32(p[0]-n.X), float32(p[1]-n.Y), float32(p[2]-n.Z)
	return float32(math.Sqrt(float64(xd*xd + yd*yd + zd*zd)))
}

// distanceManhattan is Node.distanceManhattan.
func (n *Node) distanceManhattan(p cube.Pos) float32 {
	return float32(abs(p[0]-n.X) + abs(p[1]-n.Y) + abs(p[2]-n.Z))
}

// BottomCentre is Vec3.atBottomCenterOf(node).
func (n *Node) BottomCentre() mgl64.Vec3 {
	return mgl64.Vec3{float64(n.X) + 0.5, float64(n.Y), float64(n.Z) + 0.5}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// target is a pathfinder Target: a node to reach, with the best node found towards it.
type target struct {
	Node
	pos     cube.Pos
	bestH   float32
	best    *Node
	reached bool
}

func (t *target) updateBest(h float32, n *Node) {
	if h < t.bestH {
		t.bestH, t.best = h, n
	}
}

// heap is BinaryHeap: a min-heap of nodes by f.
type heap struct{ nodes []*Node }

func (h *heap) clear() {
	for i := range h.nodes {
		h.nodes[i] = nil
	}
	h.nodes = h.nodes[:0]
}

func (h *heap) empty() bool { return len(h.nodes) == 0 }

func (h *heap) insert(n *Node) {
	h.nodes = append(h.nodes, n)
	n.heapIdx = len(h.nodes) - 1
	h.up(n.heapIdx)
}

func (h *heap) pop() *Node {
	n := h.nodes[0]
	last := len(h.nodes) - 1
	h.nodes[0] = h.nodes[last]
	h.nodes[last] = nil
	h.nodes = h.nodes[:last]
	if len(h.nodes) > 0 {
		h.down(0)
	}
	n.heapIdx = -1
	return n
}

func (h *heap) changeCost(n *Node, cost float32) {
	old := n.f
	n.f = cost
	if cost < old {
		h.up(n.heapIdx)
	} else {
		h.down(n.heapIdx)
	}
}

func (h *heap) up(idx int) {
	n := h.nodes[idx]
	for idx > 0 {
		pi := (idx - 1) >> 1
		p := h.nodes[pi]
		if !(n.f < p.f) {
			break
		}
		h.nodes[idx] = p
		p.heapIdx = idx
		idx = pi
	}
	h.nodes[idx] = n
	n.heapIdx = idx
}

func (h *heap) down(idx int) {
	n := h.nodes[idx]
	size := len(h.nodes)
	for {
		li := 1 + idx<<1
		ri := li + 1
		if li >= size {
			break
		}
		l := h.nodes[li]
		lc := l.f
		var r *Node
		rc := float32(math.Inf(1))
		if ri < size {
			r = h.nodes[ri]
			rc = r.f
		}
		if lc < rc {
			if !(lc < n.f) {
				break
			}
			h.nodes[idx] = l
			l.heapIdx = idx
			idx = li
		} else {
			if !(rc < n.f) {
				break
			}
			h.nodes[idx] = r
			r.heapIdx = idx
			idx = ri
		}
	}
	h.nodes[idx] = n
	n.heapIdx = idx
}

// Path is a found path (Path.java): its nodes, the next one to walk to, the target and whether it reaches it.
type Path struct {
	nodes        []*Node
	next         int
	target       cube.Pos
	distToTarget float32
	reached      bool
}

func newPath(nodes []*Node, target cube.Pos, reached bool) *Path {
	p := &Path{nodes: nodes, target: target, reached: reached, distToTarget: math.MaxFloat32}
	if len(nodes) > 0 {
		p.distToTarget = nodes[len(nodes)-1].distanceManhattan(target)
	}
	return p
}

// Advance moves on to the next node.
func (p *Path) Advance() { p.next++ }

// NotStarted reports whether no node was reached yet.
func (p *Path) NotStarted() bool { return p.next <= 0 }

// Done reports whether every node was reached.
func (p *Path) Done() bool { return p.next >= len(p.nodes) }

// EndNode is the last node, or nil.
func (p *Path) EndNode() *Node {
	if len(p.nodes) == 0 {
		return nil
	}
	return p.nodes[len(p.nodes)-1]
}

// Node returns node i.
func (p *Path) Node(i int) *Node { return p.nodes[i] }

// NodeCount is the number of nodes.
func (p *Path) NodeCount() int { return len(p.nodes) }

// NextIndex is the index of the next node.
func (p *Path) NextIndex() int { return p.next }

// SetNextIndex sets the index of the next node.
func (p *Path) SetNextIndex(i int) { p.next = i }

// NextNode is the next node.
func (p *Path) NextNode() *Node { return p.nodes[p.next] }

// PreviousNode is the node before the next one, or nil.
func (p *Path) PreviousNode() *Node {
	if p.next > 0 {
		return p.nodes[p.next-1]
	}
	return nil
}

// Target is the block the path was searched to.
func (p *Path) Target() cube.Pos { return p.target }

// CanReach reports whether the path reaches its target (within the reach range).
func (p *Path) CanReach() bool { return p.reached }

// DistToTarget is the Manhattan distance from the end node to the target.
func (p *Path) DistToTarget() float32 { return p.distToTarget }

// TruncateNodes drops the nodes from index i on.
func (p *Path) TruncateNodes(i int) {
	if len(p.nodes) > i {
		p.nodes = p.nodes[:i]
	}
}

// ReplaceNode replaces node i.
func (p *Path) ReplaceNode(i int, n *Node) { p.nodes[i] = n }

// EntityPosAt is Path.getEntityPosAtNode: where an entity of the width stands at node i.
func (p *Path) EntityPosAt(width float64, i int) mgl64.Vec3 {
	n := p.nodes[i]
	off := float64(int(float32(width)+1)) * 0.5
	return mgl64.Vec3{float64(n.X) + off, float64(n.Y), float64(n.Z) + off}
}

// NextEntityPos is EntityPosAt of the next node.
func (p *Path) NextEntityPos(width float64) mgl64.Vec3 { return p.EntityPosAt(width, p.next) }

// SameAs is Path.sameAs: the same nodes.
func (p *Path) SameAs(o *Path) bool {
	if o == nil || len(o.nodes) != len(p.nodes) {
		return false
	}
	for i, n := range p.nodes {
		m := o.nodes[i]
		if n.X != m.X || n.Y != m.Y || n.Z != m.Z {
			return false
		}
	}
	return true
}
