//go:build crossfly_full

package nav

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

// This file is a port of BDS 1.21.3 PathFinder (PathFinder.cpp, PathfinderNode.cpp, BinaryHeap.cpp).
// Function names in comments are the BDS ones; the logic follows the decompiled bodies, including
// their quirks (cost malus that is added again each time a node is reached, the range check
// measured from the goal, flier nodes getting the malus twice).

const (
	// maxExpansions is the hard cap of nodes popped from the open set per search.
	maxExpansions = 140
	// maxNeighbors is the size of the neighbour buffer (PathFinder +144, 32 entries).
	maxNeighbors = 32
)

// offsets is the 18-entry neighbour table (dword_14344AF80). It is filled at run time in BDS; the
// first six entries are indexed by Facing (down, up, north, south, west, east) and entries 6..17 are
// the sums of the Facing pairs in diagPairs (unk_142E9EA50, read from the 1.21.3 binary).
var offsets [18]cube.Pos

var diagPairs = [12][2]int{{2, 4}, {4, 3}, {3, 5}, {5, 2}, {1, 2}, {1, 4}, {1, 3}, {1, 5}, {0, 2}, {0, 4}, {0, 3}, {0, 5}}

// walkDirs (unk_142E9EB70) and allDirs (qword_142E9EB68) are the neighbour orders, by Facing.
var (
	walkDirs = []int{2, 5, 3, 4}
	allDirs  = []int{1, 0, 2, 3, 4, 5}
)

func init() {
	offsets[0] = cube.Pos{0, -1, 0}
	offsets[1] = cube.Pos{0, 1, 0}
	offsets[2] = cube.Pos{0, 0, -1}
	offsets[3] = cube.Pos{0, 0, 1}
	offsets[4] = cube.Pos{-1, 0, 0}
	offsets[5] = cube.Pos{1, 0, 0}
	for i, p := range diagPairs {
		offsets[6+i] = offsets[p[0]].Add(offsets[p[1]])
	}
}

// Request describes the searching mob (ActorPathingData plus the navigation component flags).
type Request struct {
	// Pos is the feet position, Width and Height the collision box.
	Pos           mgl64.Vec3
	Width, Height float64
	InWater       bool
	InLava        bool
	// Walker, Swimmer and Flier are Actor::isWalker, isSwimmer and canFly, set by the navigation kind.
	Walker, Swimmer, Flier bool
	Options                Options
	Traits                 Traits
}

// node is PathfinderNode.
type node struct {
	heapIdx   int     // +0, -1 when not in the open set
	g         float64 // +4, cost from the start
	f8        float64 // +8, costMalus + h
	f         float64 // +12, heap key
	costMalus float64 // +16, < 0 means unusable
	moveMalus float64 // +20, jump cost
	h         float64 // +24, 1.5 x distance to the goal
	typ       NodeType
	parent    *node
	pos       cube.Pos
	closed    bool
}

func (n *node) inOpen() bool { return n.heapIdx >= 0 }

func (n *node) distTo(o *node) float64 {
	dx := float64(o.pos[0] - n.pos[0])
	dy := float64(o.pos[1] - n.pos[1])
	dz := float64(o.pos[2] - n.pos[2])
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}

func intDistSq(a, b cube.Pos) int {
	dx, dy, dz := a[0]-b[0], a[1]-b[1], a[2]-b[2]
	return dx*dx + dy*dy + dz*dz
}

// Finder runs searches. It keeps its maps between searches to avoid allocations; it is not safe
// for concurrent use. Navigation owns one per mob.
type Finder struct {
	src   BlockSource
	cache map[uint64]*blockInfo
	nodes map[uint64]*node
	mm    map[uint64][2]float64
	heap  nodeHeap
	pages []*[infoPage]blockInfo
	used  int
	// nodePages hold the nodes of a search, reused by the next one (paths
	// copy what they need out of them).
	nodePages []*[nodePage]node
	nodesUsed int
	nb        [maxNeighbors]*node
	nbN       int
	minY      int

	r       Request
	aabb    cube.BBox
	size    cube.Pos
	radius  int
	maxJump float64
	pref    PreferredPath

	// PathFinder +400..+410, copied from the navigation component. avoidWater is changed for the
	// duration of a search that starts in water.
	canPassDoors, canOpenDoors, canOpenIronDoors, avoidWater, avoidDamage, canFloat,
	canPathOverLava, amphibious, avoidPortals, canBreach, canJump bool

	// Expansions is the number of nodes expanded by the last search.
	Expansions int
	// Lookups is the number of distinct block positions read by the last search.
	Lookups int
}

// NewFinder returns a Finder.
func NewFinder() *Finder {
	return &Finder{
		cache: make(map[uint64]*blockInfo, 1024),
		nodes: make(map[uint64]*node, 512),
		mm:    make(map[uint64][2]float64, 128),
		heap:  make(nodeHeap, 0, 1024),
	}
}

// release drops references to the world and the request after a search.
func (f *Finder) release() {
	f.src = nil
	f.r = Request{}
	clear(f.cache)
	clear(f.nodes)
	for _, p := range f.pages[:(f.used+infoPage-1)/infoPage] {
		clear(p[:])
	}
	f.used = 0
	for _, p := range f.nodePages[:(f.nodesUsed+nodePage-1)/nodePage] {
		clear(p[:])
	}
	f.nodesUsed = 0
	f.heap = f.heap[:0]
}

func (f *Finder) setup(src BlockSource, r Request) {
	f.src = src
	clear(f.cache)
	f.used = 0
	f.nodesUsed = 0
	clear(f.nodes)
	clear(f.mm)
	f.heap = f.heap[:0]
	f.nbN = 0
	f.Expansions, f.Lookups = 0, 0

	f.r = r
	w, h, p := r.Width, r.Height, r.Pos
	f.aabb = cube.Box(p[0]-w/2, p[1], p[2]-w/2, p[0]+w/2, p[1]+h, p[2]+w/2)
	f.size = cube.Pos{int(w + 1), int(h + 1), int(w + 1)}
	f.radius = ceil(w)
	f.maxJump = math.Max(r.Traits.StepHeight, 1.2)
	f.pref = DefaultPreferredPath()
	if r.Traits.Preferred != nil {
		f.pref = *r.Traits.Preferred
	}
	o := r.Options
	f.canPassDoors, f.canOpenDoors, f.canOpenIronDoors = o.CanPassDoors, o.CanOpenDoors, o.CanOpenIronDoors
	f.avoidWater, f.avoidDamage, f.canFloat = o.AvoidWater, o.AvoidDamageBlocks, o.CanFloat
	f.canPathOverLava, f.amphibious, f.avoidPortals = o.CanPathOverLava, o.IsAmphibious, o.AvoidPortals
	f.canBreach, f.canJump = o.CanBreach, o.CanJump
	f.minY = -64
	if rs, ok := src.(rangeSource); ok {
		f.minY = rs.Range().Min()
	}
}

// alloc returns a zeroed blockInfo from the Finder's arena. Pages are kept between searches.
func (f *Finder) alloc() *blockInfo {
	page, i := f.used/infoPage, f.used%infoPage
	if page == len(f.pages) {
		f.pages = append(f.pages, new([infoPage]blockInfo))
	}
	f.used++
	bi := &f.pages[page][i]
	*bi = blockInfo{}
	return bi
}

const infoPage = 256

// key packs a position into a map key: 26 bits each for x and z, 12 for y. Positions within the
// world border and height limits never collide.
func key(p cube.Pos) uint64 {
	return (uint64(p[0])&0x3ffffff)<<38 | (uint64(p[2])&0x3ffffff)<<12 | uint64(p[1])&0xfff
}

func (f *Finder) info(pos cube.Pos) *blockInfo {
	k := key(pos)
	if bi, ok := f.cache[k]; ok {
		return bi
	}
	if f.cache == nil {
		f.cache = make(map[uint64]*blockInfo)
	}
	bi := f.alloc()
	describeInto(bi, f.src, pos)
	f.cache[k] = bi
	f.Lookups++
	return bi
}

// nodeAt is _getNode(BlockPos, NodeType): it returns the node at pos, creating it, and sets its type.
func (f *Finder) nodeAt(pos cube.Pos, t NodeType) *node {
	k := key(pos)
	if n, ok := f.nodes[k]; ok {
		n.typ = t
		return n
	}
	page, i := f.nodesUsed/nodePage, f.nodesUsed%nodePage
	if page == len(f.nodePages) {
		f.nodePages = append(f.nodePages, new([nodePage]node))
	}
	f.nodesUsed++
	n := &f.nodePages[page][i]
	*n = node{heapIdx: -1, typ: t, pos: pos}
	f.nodes[k] = n
	return n
}

const nodePage = 128

// FindPathToBlock searches a path to a block, aiming at its centre (PathFinder::findPath(Actor&,
// int, int, int, float)).
func (f *Finder) FindPathToBlock(src BlockSource, r Request, target cube.Pos, maxDist float64) *Path {
	return f.FindPath(src, r, mgl64.Vec3{float64(target[0]) + 0.5, float64(target[1]) + 0.05, float64(target[2]) + 0.5}, maxDist)
}

// FindPath searches a path from r.Pos to goal (PathFinder::_findPath(ActorPathingData const&, float,
// float, float, float)). For an entity goal pass its feet position. It returns nil when no node
// better than the start was found, and an empty (invalid) path for mobs that cannot walk, swim or fly.
func (f *Finder) FindPath(src BlockSource, r Request, goal mgl64.Vec3, maxDist float64) *Path {
	f.setup(src, r)
	gx, gy, gz := goal[0], goal[1], goal[2]
	w := r.Width
	switch {
	case r.Flier:
		start := f.flyStartPos()
		target := cube.Pos{floor(gx - w*0.5), floor(gy + 0.5), floor(gz - w*0.5)}
		start, ok := f.findNearestPathableNeighbor(start)
		if !ok {
			return nil
		}
		s := f.nodeAt(start, NodeFlyable)
		t := f.nodeAt(target, NodeFlyable)
		return f.search(s, t, maxDist)
	case r.Swimmer && r.InWater:
		y := floor(f.aabb.Min().Y())
		if f.info(cube.PosFromVec3(r.Pos)).liquid != matWater {
			x, z := floor(r.Pos[0]), floor(r.Pos[2])
			for y > f.minY {
				bi := f.info(cube.Pos{x, y, z})
				if !(bi.air && bi.liquid == matOther) {
					break
				}
				y--
			}
			if f.info(cube.Pos{x, y, z}).liquid != matWater {
				y++
			}
		}
		start := cube.Pos{floor(f.aabb.Min().X()), floor(float64(y) + 0.5), floor(f.aabb.Min().Z())}
		target := cube.Pos{floor(gx - w*0.5), floor(gy + 0.5), floor(gz - w*0.5)}
		t1 := f.classifyNode(start, f.info(start), start, false, true)
		t2 := f.classifyNode(target, f.info(target), target, false, true)
		s := f.nodeAt(start, t1)
		t := f.nodeAt(target, t2)
		return f.search(s, t, maxDist)
	case !r.Walker:
		return &Path{}
	}

	savedAvoidWater := f.avoidWater
	defer func() { f.avoidWater = savedAvoidWater }()
	startY := floor(f.aabb.Min().Y())
	if f.canFloat && r.InWater {
		x, z := floor(r.Pos[0]), floor(r.Pos[2])
		if !f.info(cube.Pos{x, startY, z}).solid {
			for f.info(cube.Pos{x, startY, z}).liquid == matWater {
				startY++
			}
			f.avoidWater = false
		}
	}
	var sx, sz int
	if w <= 1 {
		sx, sz = floor(r.Pos[0]), floor(r.Pos[2])
	} else {
		sx, sz = floor(f.aabb.Min().X()), floor(f.aabb.Min().Z())
	}
	start, ok := f.findNearestPathableNeighbor(cube.Pos{sx, startY, sz})
	if !ok {
		return nil
	}

	target := cube.Pos{floor(gx), floor(gy + 0.5), floor(gz)}
	var end *node
	if !f.info(down(target)).air {
		end = f.nodeAt(target, NodeWalkable)
	} else {
		half := w * 0.5
		for x := floor(gx - half); x <= floor(gx+half) && end == nil; x++ {
			for z := floor(gz - half); z <= floor(gz+half); z++ {
				p := cube.Pos{x, target[1], z}
				if !f.info(down(p)).air {
					end = f.nodeAt(p, NodeWalkable)
					break
				}
			}
		}
		if end == nil {
			p := target
			for p[1] > f.minY && f.info(down(p)).air {
				p[1]--
			}
			end = f.nodeAt(p, NodeWalkable)
		}
	}
	s := f.nodeAt(start, NodeWalkable)
	if r.Traits.DoorBreaker || r.Traits.DoorOpener {
		if t := f.classifyDoorNode(f.info(start), end.pos, start); t != nodeNone {
			s.typ = t
		}
	}
	return f.search(s, end, maxDist)
}

// search is the A* loop (_findPath(ActorPathingData const&, PathfinderNode*, PathfinderNode*,
// PathfinderNode const&, float)).
func (f *Finder) search(start, target *node, maxDist float64) *Path {
	if start == nil || target == nil {
		return &Path{}
	}
	start.g = 0
	start.h = start.distTo(target) * 1.5
	start.f8, start.f = start.h, start.h
	f.heap = f.heap[:0]
	f.heap.insert(start)
	maxDistSq := int(maxDist * maxDist)
	walkerOnly := f.r.Walker && !f.r.Swimmer && !f.r.Flier

	best := start
	for f.Expansions < maxExpansions && len(f.heap) > 0 {
		cur := f.heap.pop()
		if cur.pos == target.pos {
			return f.reconstruct(target, true)
		}
		if cur.h < best.h {
			best = cur
		}
		cur.closed = true

		var mask uint32
		lo, hi := 0, 18
		if walkerOnly {
			lo, hi = 2, 10
		}
		for i := lo; i < hi; i++ {
			if f.neighborPotentiallyValid(cur, target, offsets[i], maxDistSq) {
				mask |= 1 << i
			}
		}
		n := f.getNeighbors(cur, target, maxDistSq, mask)
		for _, nb := range f.nb[:n] {
			dsq := intDistSq(cur.pos, nb.pos)
			step := 1.0
			if dsq != 1 {
				step = math.Sqrt(float64(dsq))
			}
			g := nb.costMalus + (step + cur.g) + nb.moveMalus
			if !nb.inOpen() || g < nb.g {
				nb.parent = cur
				nb.g = g
				if nb.inOpen() {
					nb.f8 = nb.costMalus + nb.h
					f.heap.changeCost(nb, nb.f8+nb.g)
				} else {
					nb.h = nb.distTo(target) * 1.5
					nb.f8 = nb.costMalus + nb.h
					nb.f = nb.f8 + nb.g
					f.heap.insert(nb)
				}
			}
		}
		f.Expansions++
	}
	if best != start {
		return f.reconstruct(best, false)
	}
	return nil
}

func (f *Finder) reconstruct(end *node, complete bool) *Path {
	n := 0
	for c := end; c != nil; c = c.parent {
		n++
	}
	nodes := make([]PathNode, n)
	for c := end; c != nil; c = c.parent {
		n--
		nodes[n] = PathNode{Pos: c.pos, Type: c.typ}
	}
	return &Path{nodes: nodes, complete: complete}
}

// neighborPotentiallyValid is _isNeighborPotentiallyValid: it prunes neighbours out of range of the
// goal and ones that cannot improve.
func (f *Finder) neighborPotentiallyValid(cur, target *node, off cube.Pos, maxDistSq int) bool {
	p := cur.pos.Add(off)
	dsq := intDistSq(p, target.pos)
	n, ok := f.nodes[key(p)]
	if !ok {
		return dsq < maxDistSq
	}
	if n.closed {
		return false
	}
	if dsq >= maxDistSq {
		n.closed = true
		return false
	}
	step := 1.0
	if s := intDistSq(cur.pos, p); s != 1 {
		step = math.Sqrt(float64(s))
	}
	return !n.inOpen() || step+cur.g < n.g
}

// getNeighbors is _getNeighbors.
func (f *Finder) getNeighbors(cur, target *node, maxDistSq int, mask uint32) int {
	f.nbN = 0
	if f.r.Swimmer {
		bi := f.info(cur.pos)
		if bi.liquid == matWater || bi.mat == matLava {
			f.addNeighbors(cur, target, maxDistSq, mask, allDirs, 0, true)
		}
	}
	if f.r.Flier {
		f.addNeighbors(cur, target, maxDistSq, mask, allDirs, 0, false)
	}
	if f.r.Walker {
		jump := 0.0
		t := f.isFree(cur.pos, up(cur.pos), false, true)
		if f.canJump && (t == NodeOpen || t == NodeClimbable || (f.amphibious && (t == NodeWalkable || t == NodeSwimmable))) {
			jump = f.maxJump
		}
		f.addNeighbors(cur, target, maxDistSq, mask, walkDirs, jump, false)
	}
	return f.nbN
}

// consider adds a usable neighbour to the buffer, closing it when it is out of range of the goal.
func (f *Finder) consider(nb, target *node, maxDistSq int) {
	if nb.closed {
		return
	}
	if intDistSq(nb.pos, target.pos) >= maxDistSq {
		nb.closed = true
		return
	}
	f.nb[f.nbN] = nb
	f.nbN++
}

// addNeighbors is _addNeighborsWithDiagonals<N, Water>.
func (f *Finder) addNeighbors(cur, target *node, maxDistSq int, mask uint32, dirs []int, jump float64, water bool) {
	if f.nbN >= maxNeighbors {
		return
	}
	get := func(to cube.Pos, j float64, climb bool) *node {
		if water {
			return f.getWaterNode(cur.pos, to)
		}
		return f.getNode(cur.pos, to, j, climb)
	}
	var prim [6]*node
	for _, d := range dirs {
		if mask&(1<<d) == 0 {
			continue
		}
		nb := get(cur.pos.Add(offsets[d]), jump, false)
		if nb == nil || nb == cur.parent {
			continue
		}
		prim[d] = nb
		if nb.costMalus >= 0 {
			f.consider(nb, target, maxDistSq)
			if f.nbN >= maxNeighbors {
				return
			}
		}
	}
	if jump > 0 {
		from := cur.pos
		if cur.parent != nil {
			from = cur.parent.pos
		}
		if f.classifyNode(from, f.info(cur.pos), cur.pos, false, true) == NodeClimbable {
			nb := get(up(cur.pos), jump-1, true)
			if nb != nil && nb != cur.parent && nb.costMalus >= 0 {
				f.consider(nb, target, maxDistSq)
				if f.nbN >= maxNeighbors {
					return
				}
			}
		}
	}
	for i := 6; i < 18; i++ {
		if mask&(1<<i) == 0 {
			continue
		}
		ok := true
		for _, c := range diagPairs[i-6] {
			p := prim[c]
			if p == nil || p.pos[1] > cur.pos[1] || (p.pos[1] == cur.pos[1] && p.costMalus < 0) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		nb := get(cur.pos.Add(offsets[i]), jump, true)
		if nb == nil || nb.costMalus < 0 {
			continue
		}
		f.consider(nb, target, maxDistSq)
		if f.nbN >= maxNeighbors {
			return
		}
	}
}

// getNode is _getNode(ActorPathingData const&, BlockPos const& from, BlockPos const& pos,
// PathfinderNode const& size, float jump, CanClimbIntoNode).
func (f *Finder) getNode(from, pos cube.Pos, jump float64, canClimb bool) *node {
	t := f.isFree(from, pos, false, canClimb)
	if f.r.Flier {
		// ((t - 2) & ~5) == 0 && t != 6: open, walkable or flyable.
		if t != NodeOpen && t != NodeWalkable && t != NodeFlyable {
			return nil
		}
		if f.avoidDamage && f.damaging(pos, f.info(pos), f.radius) {
			return nil
		}
		n := f.nodeAt(pos, t)
		n.costMalus += f.malus(t, pos)
		return f.finishNode(n, from)
	}
	var n *node
	if t != NodeBreakable && t != NodeWalkable {
		if t >= NodeOpen {
			n = f.nodeAt(pos, t)
		}
		fb := f.info(from)
		fbHas := fb.has && !fb.isDoor
		noJump := jump <= 0 || fb.props&propPreventsJumping != 0 ||
			((!fbHas || fb.props&propCarpet != 0) && f.info(down(from)).props&propPreventsJumping != 0)
		if !noJump && n == nil {
			base := float64(from[1])
			if fbHas {
				base = fb.shape.Max().Y()
			}
			hp, ok := f.highestReachableByJumping(pos, base+jump)
			if !ok {
				return nil
			}
			t2 := f.isFree(from, hp, true, canClimb)
			accept := t2 == NodeBreakable || t2 == NodeOpen || t2 == NodeClimbable ||
				(t2 == NodeWalkable && f.amphibious) ||
				(t2 == NodeDoor && (f.canOpenDoors || f.r.Traits.DoorBreaker) && f.canPassDoors)
			if !accept {
				return nil
			}
			n = f.nodeAt(hp, t2)
		}
		if n == nil {
			return nil
		}
		n = f.underneathSupported(n, from, canClimb)
	} else {
		n = f.nodeAt(pos, t)
	}
	if n == nil {
		return nil
	}
	n.costMalus += f.malus(n.typ, n.pos)
	return f.finishNode(n, from)
}

// finishNode sets the jump cost (the tail of _getNode).
func (f *Finder) finishNode(n *node, from cube.Pos) *node {
	jc := f.pref.JumpCost
	mm := 0.0
	if jc > 0 {
		below := f.info(down(n.pos))
		if (n.pos[1] > from[1] && below.props&propStair == 0) ||
			(n.pos[1] < from[1] && f.info(down(from)).props&propStair == 0) ||
			(below.props&propTrap != 0 && below.open) {
			mm = jc
		}
	}
	n.moveMalus = mm
	return n
}

// malus is _getPathfindingMalus.
func (f *Finder) malus(t NodeType, pos cube.Pos) float64 {
	if t == NodeLava || t == NodeBlocked {
		return -1
	}
	if f.r.Flier && (t == NodeOpen || t == NodeWalkable) && !f.info(pos).air {
		return 1
	}
	if len(f.pref.Blocks) > 0 {
		below := f.info(down(pos)).b
		for _, e := range f.pref.Blocks {
			for _, m := range e.Blocks {
				if m.Matches(below) {
					return e.Cost
				}
			}
		}
	}
	if t != NodeClimbable && !f.info(pos).powderSnow {
		return f.pref.DefaultBlockCost
	}
	return 1
}

// highestReachableByJumping is _getHighestReachablePosByJumping.
func (f *Finder) highestReachableByJumping(pos cube.Pos, h float64) (cube.Pos, bool) {
	y := floor(h)
	for x := pos[0]; x < pos[0]+f.size[0]; x++ {
		for z := pos[2]; z < pos[2]+f.size[2]; z++ {
			top := float64(y)
			if bi := f.info(cube.Pos{x, y, z}); bi.has && !bi.isDoor {
				top = bi.shape.Max().Y()
			}
			top2 := float64(y - 1)
			if bi := f.info(cube.Pos{x, y - 1, z}); bi.has && !bi.isDoor {
				top2 = bi.shape.Max().Y()
			}
			if h < math.Max(top2, top) {
				return cube.Pos{}, false
			}
		}
	}
	return cube.Pos{pos[0], y, pos[2]}, true
}

// underneathSupported is _getUnderneathSupportedNode: it drops a node down to the ground, up to
// max_fall_blocks.
func (f *Finder) underneathSupported(n *node, from cube.Pos, canClimb bool) *node {
	if n.typ >= NodeOpen && f.info(n.pos).partial() {
		return n
	}
	count := 0
	p := n.pos
	for p[1] > f.minY {
		below := down(p)
		t := f.isFree(from, below, false, canClimb)
		lava := func(n *node) *node {
			if t == NodeLava {
				return f.nodeAt(p, NodeLava)
			}
			return n
		}
		if t == NodeUnwalkable {
			return f.nodeAt(below, NodeBlocked)
		}
		if t == NodeHoney {
			n.costMalus += 1
			return n
		}
		if f.avoidWater && t == NodeWater {
			return f.nodeAt(below, NodeBlocked)
		}
		bb := f.info(below)
		if f.avoidDamage && f.damaging(below, bb, f.radius) {
			return lava(f.nodeAt(below, NodeBlocked))
		}
		if !f.r.Traits.FireImmune && t == NodeHot {
			return f.nodeAt(below, NodeBlocked)
		}
		if f.r.Swimmer {
			if t == NodeSwimmable {
				return lava(f.nodeAt(below, t))
			}
			if t == NodeWalkable {
				if bb.liquid == matWater {
					return lava(f.nodeAt(below, t))
				}
			} else if t < NodeOpen {
				return lava(n)
			}
		} else if t < NodeOpen {
			return lava(n)
		}
		if bb.partial() {
			return lava(f.nodeAt(below, t))
		}
		if t != NodeOpen {
			return lava(n)
		}
		if f.r.Flier {
			return n
		}
		if count >= f.pref.MaxFallBlocks {
			return f.nodeAt(below, NodeBlocked)
		}
		count++
		p = below
		if p[1] > f.minY {
			n = f.nodeAt(p, NodeOpen)
		}
	}
	return n
}

// getWaterNode is _getWaterNode.
func (f *Finder) getWaterNode(from, pos cube.Pos) *node {
	if f.canBreach && f.info(pos).air && f.info(down(pos)).liquid == matWater {
		return f.nodeAt(pos, NodeBreachable)
	}
	if f.isFreeWaterNode(from, pos) != NodeSwimmable {
		return nil
	}
	t := NodeSwimmable
	if f.info(down(pos)).solid {
		t = NodeWalkable
	}
	return f.nodeAt(pos, t)
}

// isFreeWaterNode is _isFreeWaterNode.
func (f *Finder) isFreeWaterNode(from, pos cube.Pos) NodeType {
	off := from.Sub(pos)
	for x := pos[0]; x < pos[0]+f.size[0]; x++ {
		for z := pos[2]; z < pos[2]+f.size[2]; z++ {
			for y := pos[1]; y < pos[1]+f.size[1]; y++ {
				c := cube.Pos{x, y, z}
				if t, ok := f.partialNodeType(c, c.Add(off)); ok {
					return t
				}
				bi := f.info(c)
				if !(f.canBreach && y > pos[1] && bi.air) && bi.liquid != matWater {
					return NodeBlocked
				}
			}
		}
	}
	return NodeSwimmable
}

// isFree is PathFinder::isFree: it classifies every cell of the mob's footprint at pos.
func (f *Finder) isFree(from, pos cube.Pos, canJump, canClimb bool) NodeType {
	off := from.Sub(pos)
	min := nodeNone
	flag := false
	for x := pos[0]; x < pos[0]+f.size[0]; x++ {
		for z := pos[2]; z < pos[2]+f.size[2]; z++ {
			for y := pos[1]; y < pos[1]+f.size[1]; y++ {
				c := cube.Pos{x, y, z}
				bi := f.info(c)
				if bi.props&propWalkable != 0 {
					flag = true
				} else if bi.props&propUnwalkable != 0 {
					flag = false
				} else if !f.avoidWater && bi.mat == matWater && !f.r.Swimmer {
					flag = true
				}
				if f.r.Traits.FireImmune && bi.magma {
					flag = true
				}
				if !f.avoidPortals && bi.mat == matPortal {
					flag = true
				}
				t := f.classifyNode(c.Add(off), bi, c, canJump, canClimb)
				if t < 1 {
					return t
				}
				if t < min {
					min = t
				}
			}
		}
	}
	if !flag {
		return min
	}
	if !f.r.Traits.DoorBreaker && !f.r.Traits.DoorOpener {
		return NodeWalkable
	}
	if min == NodeDoor {
		return NodeDoor
	}
	return NodeWalkable
}

// isFreeStart is _isFreeStartNode (the canUseFreeStartNodeCheck branch only applies to two special
// entity types and is left out).
func (f *Finder) isFreeStart(pos cube.Pos) NodeType {
	return f.isFree(pos, pos, false, true)
}

// findNearestPathableNeighbor is _findNearestPathableNeighbor: it moves the start node out of a
// blocked cell.
func (f *Finder) findNearestPathableNeighbor(start cube.Pos) (cube.Pos, bool) {
	t := f.isFreeStart(start)
	if t > 0 {
		return start, true
	}
	switch t {
	case NodeFence:
		// FenceBlock::fetchPathableNeighbors: approximated by the horizontal neighbours.
		for _, d := range walkDirs {
			c := start.Add(offsets[d])
			if f.isFree(start, c, false, true) >= NodeOpen {
				return c, true
			}
		}
	case NodeStairs:
		u := up(start)
		if f.isFree(u, u, false, true) >= NodeOpen {
			return u, true
		}
	}
	mn, mx := f.aabb.Min(), f.aabb.Max()
	for _, c := range [4]mgl64.Vec3{{mn[0], mn[1], mn[2]}, {mn[0], mn[1], mx[2]}, {mx[0], mn[1], mx[2]}, {mx[0], mn[1], mn[2]}} {
		p := cube.PosFromVec3(c)
		if f.isFreeStart(p) >= NodeOpen {
			return p, true
		}
	}
	best, found, bestD := start, false, math.MaxFloat64
	for _, d := range []int{2, 3, 4, 5} {
		c := start.Add(offsets[d])
		if f.isFree(start, c, false, true) >= NodeOpen {
			dx, dy, dz := float64(c[0])+0.5-f.r.Pos[0], float64(c[1])-f.r.Pos[1], float64(c[2])+0.5-f.r.Pos[2]
			if d := dx*dx + dy*dy + dz*dz; d < bestD {
				best, bestD, found = c, d, true
			}
		}
	}
	return best, found
}

// flyStartPos is _getStartPositionFlyingActor.
func (f *Finder) flyStartPos() cube.Pos {
	p := cube.PosFromVec3(f.r.Pos)
	minY := f.aabb.Min().Y()
	switch {
	case f.canFloat && f.r.InWater:
		p[1] = floor(minY)
		for f.info(p).mat == matWater {
			p[1]++
		}
	case f.canPathOverLava && f.r.InLava:
		p[1] = floor(minY)
		for f.info(p).mat == matLava {
			p[1]++
		}
	default:
		p[1] = floor(minY + 0.5)
	}
	below, above := f.info(down(p)).solid, f.info(up(p)).solid
	if below {
		if !above {
			p[1]++
		}
	} else if above {
		p[1]--
	}
	return p
}

// minMaxHeight is _getMinAndMaxHeightAroundBlock: the floor below and the ceiling above pos for a
// mob of the request's height. It is cached per search (ActorPathingData +304).
func (f *Finder) minMaxHeight(pos cube.Pos) (floorY, ceilY float64) {
	if v, ok := f.mm[key(pos)]; ok {
		return v[0], v[1]
	}
	n := float64(ceil(f.r.Height))
	shapeOf := func(p cube.Pos) (*blockInfo, bool) {
		bi := f.info(p)
		if (bi.isTrapdoor && bi.open) || bi.isDoor || !bi.has {
			return bi, false
		}
		return bi, true
	}
	ceilY = math.MaxFloat32
	for i := 0; float64(i) <= n; i++ {
		p := cube.Pos{pos[0], pos[1] + i, pos[2]}
		if bi, ok := shapeOf(p); ok && (i != 0 || bi.topPartial(p)) {
			ceilY = bi.shape.Min().Y()
			break
		}
	}
	floorY = -math.MaxFloat32
	for j := 0; float64(j) <= n; j++ {
		p := cube.Pos{pos[0], pos[1] - j, pos[2]}
		if bi, ok := shapeOf(p); ok && (j != 0 || !bi.topPartial(p)) {
			floorY = bi.shape.Max().Y()
			break
		}
	}
	f.mm[key(pos)] = [2]float64{floorY, ceilY}
	return floorY, ceilY
}

// partialNodeType is _getPartialBlockNodeType: slabs, carpets and other low blocks next to the
// mob are open if the height step is small enough.
func (f *Finder) partialNodeType(pos, from cube.Pos) (NodeType, bool) {
	if abs(pos[0]-from[0]) > 1 || abs(pos[2]-from[2]) > 1 {
		return 0, false
	}
	bi := f.info(pos)
	if bi.has && math.Abs(bi.shape.Min().Y()-bi.shape.Max().Y()) >= 1 {
		return 0, false
	}
	dy := pos[1] - from[1]
	diff := 0.0
	if pos[0] == from[0] && pos[2] == from[2] {
		if dy != 0 {
			fl, ce := f.minMaxHeight(from)
			if y := float64(pos[1]); y < fl || y > ce {
				return NodeBlocked, true
			}
		}
	} else {
		af, ac := f.minMaxHeight(pos)
		bf, bc := f.minMaxHeight(from)
		if f.r.Height > math.Min(ac, bc)-math.Max(af, bf) {
			return NodeBlocked, true
		}
		diff = af - bf
	}
	if !bi.partial() {
		return 0, false
	}
	top := bi.topPartial(pos)
	if dy >= 0 && top {
		return NodeOpen, true
	}
	if dy <= 0 && !top {
		return NodeOpen, true
	}
	if diff > f.maxJump {
		return NodeBlocked, true
	}
	return NodeOpen, true
}

// pathable is Block::checkIsPathable(Actor&, BlockPos const& from, BlockPos const& pos) with the
// block overrides that matter for vanilla mobs (DoorBlock, TrapDoorBlock, SlabBlock, FireBlock,
// CampfireBlock, LiquidBlock, SweetBerryBushBlock, ScaffoldingBlock). The default is "the material
// does not block motion".
func (f *Finder) pathable(bi *blockInfo, from, pos cube.Pos) bool {
	switch {
	case bi.isDoor:
		if from[0] == pos[0] && from[2] == pos[2] {
			return true
		}
		return moveAxis(from[0]-pos[0], from[2]-pos[2]) != bi.thinAxis
	case bi.isTrapdoor:
		if from == pos || !bi.open {
			return true
		}
		return moveAxis(pos[0]-from[0], pos[2]-from[2]) != bi.thinAxis
	case bi.slabBottom:
		above := up(pos)
		ai := f.info(above)
		if ai.has {
			return false
		}
		return f.pathable(ai, pos, above) || !bi.solid
	case bi.mat == matFire:
		return f.r.Traits.PassFire || f.r.Traits.FireImmune
	case bi.campfire:
		return !bi.campfireLit || f.r.Traits.FireImmune
	case bi.mat == matLava:
		return false
	case bi.mat == matWater:
		return true
	case bi.name == "minecraft:sweet_berry_bush":
		return !bi.berryGrown || f.r.Traits.BerryBushSafe
	case bi.scaffolding:
		return from[1] <= pos[1]
	}
	return !bi.solid
}

// checkNeighbors is _checkBlockAndNeighborsProperty: the block itself, or any block in the ring of
// radius r one level above it, has the property.
func (f *Finder) checkNeighbors(pos cube.Pos, bi *blockInfo, p prop, r int, fn func(*blockInfo) bool) bool {
	if bi.props&p != 0 {
		return fn(bi)
	}
	if r <= 0 {
		return false
	}
	for x := pos[0] - r; x <= pos[0]+r; x++ {
		for z := pos[2] - r; z <= pos[2]+r; z++ {
			if x == pos[0] && z == pos[2] {
				continue
			}
			nb := f.info(cube.Pos{x, pos[1] + 1, z})
			if f.r.Traits.Breakable != nil && f.r.Traits.Breakable(nb.b) {
				continue
			}
			if nb.props&p != 0 {
				return fn(nb)
			}
		}
	}
	return false
}

// damaging is _checkForDamagingBlock.
func (f *Finder) damaging(pos cube.Pos, bi *blockInfo, r int) bool {
	return f.checkNeighbors(pos, bi, propCausesDamage, r, func(b *blockInfo) bool {
		if f.r.Traits.FireImmune && (b.magma || b.campfire) {
			return false
		}
		if f.r.Traits.BerryBushSafe && b.name == "minecraft:sweet_berry_bush" {
			return false
		}
		return !(b.campfire && !b.campfireLit)
	})
}

// climbable is BlockClimberSystem::canBlockBeAscendedByJumping, used as the climb test of the
// pathfinder (ActorPathingData +208).
func (f *Finder) climbable(bi *blockInfo, pos cube.Pos) bool {
	switch {
	case bi.powderSnow:
		return f.r.Traits.ClimbPowderSnow
	case bi.scaffolding:
		b := f.info(down(pos))
		return !b.air && b.mat != matWater && f.r.Traits.BlockClimber
	}
	return false
}

// classifyDoorNode is _classifyDoorNode.
func (f *Finder) classifyDoorNode(bi *blockInfo, pos, from cube.Pos) NodeType {
	if !bi.isDoor || bi.props&propDoor == 0 {
		return nodeNone
	}
	if !f.canPassDoors {
		return NodeBlocked
	}
	if f.pathable(bi, from, pos) {
		return NodeOpen
	}
	if bi.mat == matMetal {
		if !f.canOpenIronDoors {
			return NodeBlocked
		}
	} else if !f.canOpenDoors {
		if f.r.Traits.DoorBreaker {
			return NodeDoor
		}
		return NodeBlocked
	}
	return NodeDoor
}

// classifyNode is _classifyNode: the type of one cell of the mob's footprint.
func (f *Finder) classifyNode(from cube.Pos, bi *blockInfo, pos cube.Pos, canJump, canClimb bool) NodeType {
	for _, m := range f.r.Options.BlocksToAvoid {
		if m.Matches(bi.b) {
			return NodeUnwalkable
		}
	}
	r := f.radius
	if bi.magma {
		return NodeHot
	}
	if bi.campfire && bi.campfireLit {
		return NodeHot
	}
	if t, ok := f.partialNodeType(pos, from); ok {
		return t
	}
	if bi.air {
		if f.r.Flier {
			b := f.info(down(pos))
			if (!b.has || b.isDoor || b.shape.Max().Y() <= float64(pos[1])+0.0001) &&
				(!f.avoidDamage || !f.damaging(pos, bi, r)) {
				return NodeFlyable
			}
			return NodeBlocked
		}
		if f.r.Swimmer && f.canBreach && bi.liquid == matWater {
			return NodeBreachable
		}
		return NodeOpen
	}
	if bi.liquid == matWater && !bi.solid {
		switch {
		case f.r.Swimmer:
			if f.info(down(pos)).solid {
				return NodeWalkable
			}
			return NodeSwimmable
		case f.canFloat:
			if f.r.InWater || !f.avoidWater {
				return NodeOpen
			}
			return NodeWater
		case f.amphibious:
			if !f.info(down(pos)).solid {
				return NodeOpen
			}
		default:
			if f.avoidWater && f.info(from).liquid != matWater {
				return NodeWater
			}
			if !f.pathable(bi, from, pos) && !f.info(down(pos)).solid {
				return NodeWater
			}
		}
	}
	if f.r.Traits.Breakable != nil && f.r.Traits.Breakable(bi.b) {
		return NodeBreakable
	}
	if f.pathable(bi, from, pos) {
		if canJump && bi.props&propHalfSlab != 0 {
			return NodeBlocked
		}
		return NodeOpen
	}
	if f.checkNeighbors(pos, bi, propUnwalkable, r-1, func(*blockInfo) bool { return true }) {
		return NodeUnwalkable
	}
	if f.avoidDamage && f.damaging(pos, bi, r) {
		return NodeBlocked
	}
	if bi.honey {
		return NodeHoney
	}
	if bi.mat == matFire {
		return NodeUnwalkable
	}
	if bi.mat == matLava && f.canPathOverLava {
		return NodeWalkable
	}
	if f.avoidPortals && bi.mat == matPortal {
		return NodeBlocked
	}
	if f.r.Traits.DoorBreaker || f.r.Traits.DoorOpener {
		if t := f.classifyDoorNode(bi, pos, from); t != nodeNone {
			return t
		}
	} else {
		if f.canOpenDoors && bi.isDoor {
			return NodeOpen
		}
		if !f.canPassDoors && bi.props&propDoor != 0 {
			return NodeBlocked
		}
	}
	if from != pos {
		fb := f.info(from)
		if !fb.air && fb.props&propDoor != 0 {
			if !bi.isDoor || (fb.mat == matMetal && !f.canOpenIronDoors) || (fb.mat != matMetal && !f.canOpenDoors) {
				if !f.pathable(fb, from, pos) {
					return NodeBlocked
				}
			}
		}
	}
	switch {
	case bi.props&propUnwalkable != 0:
		return NodeUnwalkable
	case bi.props&propConnects2D != 0:
		return NodeFence
	case bi.props&propStair != 0:
		return NodeStairs
	case bi.props&propFenceGate != 0:
		if bi.open {
			return NodeOpen
		}
		return NodeFence
	}
	if bi.mat != matLava {
		if float64(pos[1]) < f.r.Pos[1] {
			return NodeBlocked
		}
		if canClimb && f.climbable(bi, pos) {
			return NodeClimbable
		}
		if bi.powderSnow {
			return NodeWalkable
		}
		return NodeBlocked
	}
	if f.info(from).mat == matLava {
		return NodeOpen
	}
	return NodeLava
}

// nodeHeap is BinaryHeap: a min-heap on node.f with the index stored in the node.
type nodeHeap []*node

func (h *nodeHeap) insert(n *node) {
	*h = append(*h, n)
	n.heapIdx = len(*h) - 1
	h.up(n.heapIdx)
}

func (h *nodeHeap) pop() *node {
	s := *h
	top := s[0]
	last := len(s) - 1
	s[0] = s[last]
	s[0].heapIdx = 0
	*h = s[:last]
	if last > 0 {
		h.down(0)
	}
	top.heapIdx = -1
	return top
}

func (h *nodeHeap) changeCost(n *node, f float64) {
	old := n.f
	n.f = f
	if f < old {
		h.up(n.heapIdx)
	} else {
		h.down(n.heapIdx)
	}
}

func (h nodeHeap) up(i int) {
	n := h[i]
	for i > 0 {
		p := (i - 1) >> 1
		if n.f >= h[p].f {
			break
		}
		h[i] = h[p]
		h[i].heapIdx = i
		i = p
	}
	h[i] = n
	n.heapIdx = i
}

func (h nodeHeap) down(i int) {
	n := h[i]
	for {
		l := 2*i + 1
		if l >= len(h) {
			break
		}
		r := l + 1
		lf, rf := h[l].f, math.MaxFloat64
		var rn *node
		if r < len(h) {
			rn = h[r]
			rf = rn.f
		}
		if lf >= rf {
			if rf >= n.f || rn == nil {
				break
			}
			h[i] = rn
			rn.heapIdx = i
			i = r
		} else {
			if lf >= n.f {
				break
			}
			h[i] = h[l]
			h[i].heapIdx = i
			i = l
		}
	}
	h[i] = n
	n.heapIdx = i
}
