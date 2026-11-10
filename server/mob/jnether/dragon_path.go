//go:build crossfly_full

package jnether

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// The ender dragon's flight graph (EnderDragon.findClosestNode, findPath):
// 24 nodes in three rings around the podium, 12 at radius 60, 8 at 40 (10
// higher) and 4 at 20, each at least y 73 and 5 (15 for the middle ring)
// above the terrain, joined by fixed adjacency masks and searched with A*.

// dragonNode is a pathfinder Node of the graph.
type dragonNode struct {
	x, y, z int

	closed  bool
	f, g, h float32
	from    *dragonNode
	heapIdx int
}

// distTo is Node.distanceTo (float).
func (n *dragonNode) distTo(o *dragonNode) float32 {
	return float32(math.Sqrt(float64(n.distSq(o))))
}

// distSq is Node.distanceToSqr (float).
func (n *dragonNode) distSq(o *dragonNode) float32 {
	dx, dy, dz := float32(o.x-n.x), float32(o.y-n.y), float32(o.z-n.z)
	return dx*dx + dy*dy + dz*dz
}

// dragonAdjacency is EnderDragon.nodeAdjacency.
var dragonAdjacency = [24]int{
	6146, 8197, 8202, 16404, 32808, 32848, 65696, 131392, 131712, 263424, 526848, 525313,
	1581057, 3166214, 2138120, 6373424, 4358208, 12910976, 9044480, 9706496,
	15216640, 13688832, 11763712, 8257536,
}

// dragonPath is a pathfinder Path of graph nodes.
type dragonPath struct {
	nodes []*dragonNode
	next  int
}

func (p *dragonPath) done() bool { return p.next >= len(p.nodes) }
func (p *dragonPath) advance()   { p.next++ }
func (p *dragonPath) nextNode() cube.Pos {
	n := p.nodes[p.next]
	return cube.Pos{n.x, n.y, n.z}
}

// buildNodes lays out the nodes the first time (findClosestNode): y is the
// MOTION_BLOCKING_NO_LEAVES height + the ring's offset, at least 73.
func (d *Dragon) buildNodes(tx *world.Tx) {
	if d.nodes[0] != nil {
		return
	}
	for i := range 24 {
		yAdj := 5
		var x, z int
		switch {
		case i < 12:
			a := 2 * (float32(-math.Pi) + float32(math.Pi/12)*float32(i))
			x, z = floorF(60*cosF(a)), floorF(60*sinF(a))
		case i < 20:
			m := i - 12
			a := 2 * (float32(-math.Pi) + float32(math.Pi/8)*float32(m))
			x, z = floorF(40*cosF(a)), floorF(40*sinF(a))
			yAdj += 10
		default:
			m := i - 20
			a := 2 * (float32(-math.Pi) + float32(math.Pi/4)*float32(m))
			x, z = floorF(20*cosF(a)), floorF(20*sinF(a))
		}
		y := max(73, heightNoLeaves(tx, x, z)+yAdj)
		d.nodes[i] = &dragonNode{x: x, y: y, z: z, heapIdx: -1}
	}
}

// closestNode is findClosestNode() from the dragon's position.
func (d *Dragon) closestNode(h DragonHost) int {
	d.buildNodes(h.Tx())
	p := h.Position()
	return d.closestNodeTo(p[0], p[1], p[2])
}

// closestNodeTo is findClosestNode(x, y, z): without crystals only the
// inner rings count.
func (d *Dragon) closestNodeTo(x, y, z float64) int {
	cur := &dragonNode{x: int(math.Floor(x)), y: int(math.Floor(y)), z: int(math.Floor(z))}
	start := 0
	if d.aliveCrystals() == 0 {
		start = 12
	}
	best, idx := float32(10000), 0
	for i := start; i < 24; i++ {
		if n := d.nodes[i]; n != nil {
			if dist := n.distSq(cur); dist < best {
				best, idx = dist, i
			}
		}
	}
	return idx
}

// findPath is EnderDragon.findPath: A* over the graph from node a to node
// b (the outer ring is skipped without crystals); final, when set, is
// appended after b. Without a way to b the path leads to the node closest
// to it, nil when that is a itself.
func (d *Dragon) findPath(a, b int, final *dragonNode) *dragonPath {
	for _, n := range d.nodes {
		n.closed, n.f, n.g, n.h, n.from, n.heapIdx = false, 0, 0, 0, nil, -1
	}
	from, to := d.nodes[a], d.nodes[b]
	from.g, from.h = 0, from.distTo(to)
	from.f = from.h
	var open nodeHeap
	open.insert(from)
	closest := from
	minIdx := 0
	if d.aliveCrystals() == 0 {
		minIdx = 12
	}
	for !open.empty() {
		n := open.pop()
		if n == to {
			if final != nil {
				final.from = to
				to = final
			}
			return reconstructPath(to)
		}
		if n.distTo(to) < closest.distTo(to) {
			closest = n
		}
		n.closed = true
		xi := 0
		for i, o := range d.nodes {
			if o == n {
				xi = i
				break
			}
		}
		for i := minIdx; i < 24; i++ {
			if dragonAdjacency[xi]&(1<<i) == 0 {
				continue
			}
			adj := d.nodes[i]
			if adj.closed {
				continue
			}
			g := n.g + n.distTo(adj)
			if adj.heapIdx < 0 || g < adj.g {
				adj.from, adj.g, adj.h = n, g, adj.distTo(to)
				if adj.heapIdx >= 0 {
					open.changeCost(adj, adj.g+adj.h)
				} else {
					adj.f = adj.g + adj.h
					open.insert(adj)
				}
			}
		}
	}
	if closest == from {
		return nil
	}
	if final != nil {
		final.from = closest
		closest = final
	}
	return reconstructPath(closest)
}

func reconstructPath(to *dragonNode) *dragonPath {
	var nodes []*dragonNode
	for n := to; n != nil; n = n.from {
		nodes = append([]*dragonNode{n}, nodes...)
	}
	return &dragonPath{nodes: nodes}
}

// nodeHeap is the pathfinder BinaryHeap (a min-heap on f).
type nodeHeap struct{ h []*dragonNode }

func (q *nodeHeap) empty() bool { return len(q.h) == 0 }

func (q *nodeHeap) insert(n *dragonNode) {
	n.heapIdx = len(q.h)
	q.h = append(q.h, n)
	q.upHeap(n.heapIdx)
}

func (q *nodeHeap) pop() *dragonNode {
	n := q.h[0]
	last := len(q.h) - 1
	q.h[0] = q.h[last]
	q.h[0].heapIdx = 0
	q.h = q.h[:last]
	if len(q.h) > 0 {
		q.downHeap(0)
	}
	n.heapIdx = -1
	return n
}

func (q *nodeHeap) changeCost(n *dragonNode, f float32) {
	old := n.f
	n.f = f
	if f < old {
		q.upHeap(n.heapIdx)
	} else {
		q.downHeap(n.heapIdx)
	}
}

func (q *nodeHeap) upHeap(i int) {
	n := q.h[i]
	f := n.f
	for i > 0 {
		p := (i - 1) >> 1
		pn := q.h[p]
		if !(f < pn.f) {
			break
		}
		q.h[i] = pn
		pn.heapIdx = i
		i = p
	}
	q.h[i] = n
	n.heapIdx = i
}

func (q *nodeHeap) downHeap(i int) {
	n := q.h[i]
	f := n.f
	for {
		l := 1 + i<<1
		r := l + 1
		if l >= len(q.h) {
			break
		}
		ln := q.h[l]
		var rn *dragonNode
		rf := float32(math.Inf(1))
		if r < len(q.h) {
			rn = q.h[r]
			rf = rn.f
		}
		if ln.f < rf {
			if !(ln.f < f) {
				break
			}
			q.h[i] = ln
			ln.heapIdx = i
			i = l
		} else {
			if !(rf < f) {
				break
			}
			q.h[i] = rn
			rn.heapIdx = i
			i = r
		}
	}
	q.h[i] = n
	n.heapIdx = i
}

// heightNoLeaves is Level.getHeight(MOTION_BLOCKING_NO_LEAVES, x, z): one
// above the highest block that blocks motion (has collision boxes) or holds
// a liquid, leaves left out.
func heightNoLeaves(tx *world.Tx, x, z int) int {
	r := tx.Range()
	for y := r.Max(); y >= r.Min(); y-- {
		pos := cube.Pos{x, y, z}
		if motionBlocking(tx, pos) {
			return y + 1
		}
	}
	return r.Min()
}

// motionBlocking: a block with a collision box (not leaves) or a liquid.
func motionBlocking(tx *world.Tx, pos cube.Pos) bool {
	b := tx.Block(pos)
	if name, _ := b.EncodeBlock(); len(name) > 6 && name[len(name)-6:] == "leaves" {
		return false
	}
	if len(b.Model().BBox(pos, tx)) > 0 {
		return true
	}
	_, liquid := tx.Liquid(pos)
	return liquid
}

func floorF(v float32) int { return int(math.Floor(float64(v))) }

// sinF and cosF are Mth.sin/cos: Java's lookup table of 65536 sines.
func sinF(v float32) float32 { return mthSin(v) }
func cosF(v float32) float32 { return mthCos(v) }

// mthSin is Mth.sin: SIN[(int)(value * 10430.378F) & 0xFFFF].
func mthSin(v float32) float32 {
	return float32(math.Sin(float64(int32(int64(v*10430.378)&0xffff)) * math.Pi * 2 / 65536))
}

// mthCos is Mth.cos: SIN[(int)(value * 10430.378F + 16384.0F) & 0xFFFF].
func mthCos(v float32) float32 {
	return float32(math.Sin(float64(int32(int64(v*10430.378+16384)&0xffff)) * math.Pi * 2 / 65536))
}
