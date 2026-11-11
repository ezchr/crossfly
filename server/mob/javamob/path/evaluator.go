//go:build crossfly_full

package path

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Mob is what the pathfinder and the navigation need of a mob. *javamob.Mob implements it.
type Mob interface {
	Tx() *world.Tx
	Position() mgl64.Vec3
	// BBox is the mob's collision box in the world.
	BBox() cube.BBox
	// Width and Height are getBbWidth and getBbHeight.
	Width() float64
	Height() float64
	OnGround() bool
	// InLiquid is isInLiquid (water or lava), InFloatableFluid isInFloatableFluid (water).
	InLiquid() bool
	InFloatableFluid() bool
	Passenger() bool
	// MaxUpStep is the step height attribute.
	MaxUpStep() float64
	// MaxFallDistance is Mob.getMaxFallDistance.
	MaxFallDistance() int
	// PathfindingMalus is Mob.getPathfindingMalus.
	PathfindingMalus(t Type) float32
	// FollowRange is the follow range attribute's value, FollowRangeBase its base value.
	FollowRange() float64
	FollowRangeBase() float64
	// MoveSpeed is LivingEntity.getSpeed (set by the move control).
	MoveSpeed() float64
	// SetWantedPosition is getMoveControl().setWantedPosition.
	SetWantedPosition(x, y, z, speed float64)
}

// horizontal are Direction.Plane.HORIZONTAL in order (north, east, south, west) and dataValue2D their
// get2DDataValue.
var (
	horizontal  = [4]cube.Pos{{0, 0, -1}, {1, 0, 0}, {0, 0, 1}, {-1, 0, 0}}
	dataValue2D = [4]int{2, 3, 0, 1}
	// clockwise[i] is the index in horizontal of getClockWise of horizontal[i].
	clockwise = [4]int{1, 2, 3, 0}
)

// Evaluator is a WalkNodeEvaluator: it turns blocks into nodes for one mob's searches.
type Evaluator struct {
	tx        *world.Tx
	mob       Mob
	mobPos    cube.Pos
	nodes     map[cube.Pos]*Node
	typeCache map[cube.Pos]Type
	mobCache  map[cube.Pos]Type
	collCache map[cube.BBox]bool

	width, height, depth int

	canPassDoors      bool
	canOpenDoors      bool
	canFloat          bool
	canWalkOverFences bool

	// swim makes this a SwimNodeEvaluator (swim.go); breach is its allowBreaching.
	swim, breach bool

	neighbours [4]*Node
}

// NewEvaluator returns a walk node evaluator (canPassDoors on, the rest off, as NodeEvaluator).
func NewEvaluator() *Evaluator {
	return &Evaluator{canPassDoors: true, nodes: map[cube.Pos]*Node{}, typeCache: map[cube.Pos]Type{},
		mobCache: map[cube.Pos]Type{}, collCache: map[cube.BBox]bool{}}
}

// SetCanPassDoors, SetCanOpenDoors, SetCanFloat and SetCanWalkOverFences set what the mob may path through.
func (e *Evaluator) SetCanPassDoors(v bool)      { e.canPassDoors = v }
func (e *Evaluator) SetCanOpenDoors(v bool)      { e.canOpenDoors = v }
func (e *Evaluator) SetCanFloat(v bool)          { e.canFloat = v }
func (e *Evaluator) SetCanWalkOverFences(v bool) { e.canWalkOverFences = v }

// CanPassDoors, CanOpenDoors, CanFloat and CanWalkOverFences report the settings.
func (e *Evaluator) CanPassDoors() bool      { return e.canPassDoors }
func (e *Evaluator) CanOpenDoors() bool      { return e.canOpenDoors }
func (e *Evaluator) CanFloat() bool          { return e.canFloat }
func (e *Evaluator) CanWalkOverFences() bool { return e.canWalkOverFences }

// prepare is NodeEvaluator.prepare.
func (e *Evaluator) prepare(tx *world.Tx, m Mob) {
	e.tx, e.mob = tx, m
	e.mobPos = cube.PosFromVec3(m.Position())
	clear(e.nodes)
	e.width = int(math.Floor(float64(float32(m.Width()) + 1)))
	e.height = int(math.Floor(float64(float32(m.Height()) + 1)))
	e.depth = e.width
}

// done is WalkNodeEvaluator.done.
func (e *Evaluator) done() {
	clear(e.mobCache)
	clear(e.collCache)
	clear(e.typeCache)
	e.tx, e.mob = nil, nil
}

func (e *Evaluator) node(x, y, z int) *Node {
	p := cube.Pos{x, y, z}
	if n, ok := e.nodes[p]; ok {
		return n
	}
	n := newNode(x, y, z)
	e.nodes[p] = n
	return n
}

// start is WalkNodeEvaluator.getStart.
func (e *Evaluator) start() *Node {
	if e.swim {
		return e.swimStart()
	}
	tx, m := e.tx, e.mob
	pos := m.Position()
	startY := int(math.Floor(pos[1]))
	x, z := int(math.Floor(pos[0])), int(math.Floor(pos[2]))
	if e.canFloat && m.InFloatableFluid() {
		for isWater(tx, cube.Pos{x, startY, z}) {
			startY++
		}
		startY--
	} else if m.OnGround() {
		startY = int(math.Floor(pos[1] + 0.5))
	} else {
		y := int(math.Floor(pos[1] + 1))
		for y > tx.Range()[0] {
			startY = y
			y--
			bp := cube.Pos{x, y, z}
			info := infoOf(tx.BlockRuntimeIDAt(bp))
			if info.kind != kindAir && !e.landPathfindable(bp, info) {
				break
			}
		}
	}
	startPos := cube.PosFromVec3(pos)
	if !e.canStartAt(cube.Pos{startPos[0], startY, startPos[2]}) {
		bb := m.BBox()
		for _, c := range [][2]float64{{bb.Min()[0], bb.Min()[2]}, {bb.Min()[0], bb.Max()[2]}, {bb.Max()[0], bb.Min()[2]}, {bb.Max()[0], bb.Max()[2]}} {
			p := cube.Pos{int(math.Floor(c[0])), startY, int(math.Floor(c[1]))}
			if e.canStartAt(p) {
				return e.startNode(p)
			}
		}
	}
	return e.startNode(cube.Pos{startPos[0], startY, startPos[2]})
}

// landPathfindable is isPathfindable(LAND) of the block at pos.
func (e *Evaluator) landPathfindable(pos cube.Pos, info blockInfo) bool {
	switch info.kind {
	case kindBlocked, kindLava, kindFence, kindDoorWood, kindDoorIron, kindLeaves, kindDamaging, kindCocoa:
		return false
	case kindTrapdoor:
		return false
	}
	return true
}

func (e *Evaluator) startNode(p cube.Pos) *Node {
	n := e.node(p[0], p[1], p[2])
	n.Type = e.cachedType(p[0], p[1], p[2])
	n.CostMalus = e.mob.PathfindingMalus(n.Type)
	return n
}

func (e *Evaluator) canStartAt(p cube.Pos) bool {
	t := e.cachedType(p[0], p[1], p[2])
	return t != Open && e.mob.PathfindingMalus(t) >= 0
}

// neighbours is WalkNodeEvaluator.getNeighbors.
func (e *Evaluator) neighboursOf(out []*Node, pos *Node) int {
	if e.swim {
		return e.swimNeighbours(out, pos)
	}
	p, jump := 0, 0
	above := e.cachedType(pos.X, pos.Y+1, pos.Z)
	cur := e.cachedType(pos.X, pos.Y, pos.Z)
	if e.mob.PathfindingMalus(above) >= 0 && cur != StickyHoney {
		jump = int(math.Floor(math.Max(1, float64(float32(e.mob.MaxUpStep())))))
	}
	height := e.floorLevel(cube.Pos{pos.X, pos.Y, pos.Z})
	for i, d := range horizontal {
		n := e.acceptedNode(pos.X+d[0], pos.Y, pos.Z+d[2], jump, height, i, cur)
		e.neighbours[dataValue2D[i]] = n
		if e.neighbourValid(n, pos) {
			out[p] = n
			p++
		}
	}
	for i, d := range horizontal {
		j := clockwise[i]
		d2 := horizontal[j]
		if e.diagonalValid(pos, e.neighbours[dataValue2D[i]], e.neighbours[dataValue2D[j]]) {
			n := e.acceptedNode(pos.X+d[0]+d2[0], pos.Y, pos.Z+d[2]+d2[2], jump, height, i, cur)
			if e.diagonalNodeValid(n) {
				out[p] = n
				p++
			}
		}
	}
	return p
}

func (e *Evaluator) neighbourValid(n, cur *Node) bool {
	return n != nil && !n.closed && (n.CostMalus >= 0 || cur.CostMalus < 0)
}

func (e *Evaluator) diagonalValid(pos, ew, ns *Node) bool {
	if ns == nil || ew == nil || ns.Y > pos.Y || ew.Y > pos.Y {
		return false
	}
	if ew.Type == WalkableDoor || ns.Type == WalkableDoor {
		return false
	}
	w := float32(e.mob.Width())
	if w > 1 && (ew.CostMalus > 0 || ns.CostMalus > 0) {
		return false
	}
	between := ns.Type == Fence && ew.Type == Fence && w < 0.5
	return (ns.Y < pos.Y || ns.CostMalus >= 0 || between) && (ew.Y < pos.Y || ew.CostMalus >= 0 || between)
}

func (e *Evaluator) diagonalNodeValid(n *Node) bool {
	return n != nil && !n.closed && n.Type != WalkableDoor && n.CostMalus >= 0
}

func partialCollision(t Type) bool { return t == Fence || t == DoorWoodClosed || t == DoorIronClosed }

// canReachWithoutCollision is WalkNodeEvaluator.canReachWithoutCollision.
func (e *Evaluator) canReachWithoutCollision(to *Node) bool {
	bb := e.mob.BBox()
	pos := e.mob.Position()
	size := bb.Max().Sub(bb.Min())
	delta := mgl64.Vec3{float64(to.X) - pos[0] + size[0]/2, float64(to.Y) - pos[1] + size[1]/2, float64(to.Z) - pos[2] + size[2]/2}
	avg := (size[0] + size[1] + size[2]) / 3
	steps := int(math.Ceil(delta.Len() / avg))
	delta = delta.Mul(1 / float64(float32(steps)))
	for i := 1; i <= steps; i++ {
		bb = bb.Translate(delta)
		if e.hasCollisions(bb) {
			return false
		}
	}
	return true
}

// floorLevel is WalkNodeEvaluator.getFloorLevel(pos).
func (e *Evaluator) floorLevel(pos cube.Pos) float64 {
	if e.canFloat && isWater(e.tx, pos) {
		return float64(pos[1]) + 0.5
	}
	return FloorLevel(e.tx, pos)
}

// acceptedNode is WalkNodeEvaluator.findAcceptedNode. dir is the index in horizontal of the travel direction.
func (e *Evaluator) acceptedNode(x, y, z, jump int, nodeHeight float64, dir int, cur Type) *Node {
	var best *Node
	if e.floorLevel(cube.Pos{x, y, z})-nodeHeight > e.mobJumpHeight() {
		return nil
	}
	t := e.cachedType(x, y, z)
	cost := e.mob.PathfindingMalus(t)
	if cost >= 0 {
		best = e.nodeCostMax(x, y, z, t, cost)
	}
	if partialCollision(cur) && best != nil && best.CostMalus >= 0 && !e.canReachWithoutCollision(best) {
		best = nil
	}
	if t == Walkable {
		return best
	}
	switch {
	case (best == nil || best.CostMalus < 0) && jump > 0 && (t != Fence || e.canWalkOverFences) &&
		t != UnpassableRail && t != Trapdoor && t != PowderSnow:
		best = e.tryJumpOn(x, y, z, jump, nodeHeight, dir, cur)
	case t == Water && !e.canFloat:
		best = e.firstNonWaterBelow(x, y, z, best)
	case t == Open:
		best = e.firstGroundBelow(x, y, z)
	case partialCollision(t) && best == nil:
		best = e.closedNode(x, y, z, t)
	}
	return best
}

func (e *Evaluator) mobJumpHeight() float64 {
	return math.Max(1.125, float64(float32(e.mob.MaxUpStep())))
}

func (e *Evaluator) nodeCostMax(x, y, z int, t Type, cost float32) *Node {
	n := e.node(x, y, z)
	n.Type = t
	n.CostMalus = max(n.CostMalus, cost)
	return n
}

func (e *Evaluator) blockedNode(x, y, z int) *Node {
	n := e.node(x, y, z)
	n.Type, n.CostMalus = Blocked, -1
	return n
}

func (e *Evaluator) closedNode(x, y, z int, t Type) *Node {
	n := e.node(x, y, z)
	n.closed, n.Type, n.CostMalus = true, t, t.Malus()
	return n
}

// tryJumpOn is WalkNodeEvaluator.tryJumpOn.
func (e *Evaluator) tryJumpOn(x, y, z, jump int, nodeHeight float64, dir int, cur Type) *Node {
	above := e.acceptedNode(x, y+1, z, jump-1, nodeHeight, dir, cur)
	if above == nil {
		return nil
	}
	w := float64(float32(e.mob.Width()))
	if w >= 1 || above.Type != Open && above.Type != Walkable {
		return above
	}
	d := horizontal[dir]
	cx, cz := float64(x-d[0])+0.5, float64(z-d[2])+0.5
	half := w / 2
	minY := e.floorLevel(cube.PosFromVec3(mgl64.Vec3{cx, float64(y + 1), cz})) + 0.001
	maxY := float64(float32(e.mob.Height())) + e.floorLevel(cube.Pos{above.X, above.Y, above.Z}) - 0.002
	bb := cube.Box(cx-half, minY, cz-half, cx+half, maxY, cz+half)
	if e.hasCollisions(bb) {
		return nil
	}
	return above
}

func (e *Evaluator) firstNonWaterBelow(x, y, z int, best *Node) *Node {
	y--
	for y > e.tx.Range()[0] {
		t := e.cachedType(x, y, z)
		if t != Water {
			return best
		}
		best = e.nodeCostMax(x, y, z, t, e.mob.PathfindingMalus(t))
		y--
	}
	return best
}

func (e *Evaluator) firstGroundBelow(x, y, z int) *Node {
	for cy := y - 1; cy >= e.tx.Range()[0]; cy-- {
		if y-cy > e.mob.MaxFallDistance() {
			return e.blockedNode(x, cy, z)
		}
		t := e.cachedType(x, cy, z)
		cost := e.mob.PathfindingMalus(t)
		if t != Open {
			if cost >= 0 {
				return e.nodeCostMax(x, cy, z, t, cost)
			}
			return e.blockedNode(x, cy, z)
		}
	}
	return e.blockedNode(x, y, z)
}

// hasCollisions is WalkNodeEvaluator.hasCollisions: a block collision box overlaps bb.
func (e *Evaluator) hasCollisions(bb cube.BBox) bool {
	if v, ok := e.collCache[bb]; ok {
		return v
	}
	v := Collides(e.tx, bb)
	e.collCache[bb] = v
	return v
}

// Collides reports whether any block collision box overlaps bb with a positive volume (the opposite of
// CollisionGetter.noCollision for blocks).
func Collides(tx *world.Tx, bb cube.BBox) bool {
	min, max := bb.Min(), bb.Max()
	x0, y0, z0 := int(math.Floor(min[0]-1e-7))-1, int(math.Floor(min[1]-1e-7))-1, int(math.Floor(min[2]-1e-7))-1
	x1, y1, z1 := int(math.Floor(max[0]+1e-7))+1, int(math.Floor(max[1]+1e-7))+1, int(math.Floor(max[2]+1e-7))+1
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			for y := y0; y <= y1; y++ {
				pos := cube.Pos{x, y, z}
				if pos.OutOfBounds(tx.Range()) {
					continue
				}
				if infoOf(tx.BlockRuntimeIDAt(pos)).kind == kindAir {
					continue
				}
				for _, b := range tx.Block(pos).Model().BBox(pos, tx) {
					b = b.Translate(pos.Vec3())
					if overlaps(b, bb) {
						return true
					}
				}
			}
		}
	}
	return false
}

func overlaps(a, b cube.BBox) bool {
	return a.Min()[0] < b.Max()[0] && a.Max()[0] > b.Min()[0] &&
		a.Min()[1] < b.Max()[1] && a.Max()[1] > b.Min()[1] &&
		a.Min()[2] < b.Max()[2] && a.Max()[2] > b.Min()[2]
}

// cachedType is WalkNodeEvaluator.getCachedPathType.
func (e *Evaluator) cachedType(x, y, z int) Type {
	p := cube.Pos{x, y, z}
	if t, ok := e.mobCache[p]; ok {
		return t
	}
	t := e.typeOfMob(x, y, z)
	e.mobCache[p] = t
	return t
}

// typeOfMob is WalkNodeEvaluator.getPathTypeOfMob: the path type of the blocks the mob's box takes up at pos.
func (e *Evaluator) typeOfMob(x, y, z int) Type {
	var seen [TypeCount]bool
	count := 0
	for dx := 0; dx < e.width; dx++ {
		for dy := 0; dy < e.height; dy++ {
			for dz := 0; dz < e.depth; dz++ {
				t := e.TypeAt(x+dx, y+dy, z+dz)
				if t == DoorWoodClosed && e.canOpenDoors && e.canPassDoors {
					t = WalkableDoor
				}
				if t == DoorOpen && !e.canPassDoors {
					t = Blocked
				}
				if t == Rail && e.TypeAt(e.mobPos[0], e.mobPos[1], e.mobPos[2]) != Rail &&
					e.TypeAt(e.mobPos[0], e.mobPos[1]-1, e.mobPos[2]) != Rail {
					t = UnpassableRail
				}
				if !seen[t] {
					seen[t] = true
					count++
				}
			}
		}
	}
	if count == 1 {
		for t := Type(0); t < TypeCount; t++ {
			if seen[t] {
				return t
			}
		}
	}
	if seen[Fence] {
		return Fence
	}
	if seen[UnpassableRail] {
		return UnpassableRail
	}
	highestType := Blocked
	highest := e.mob.PathfindingMalus(Blocked)
	// EnumSet iteration is in ordinal order.
	for t := Type(0); t < TypeCount; t++ {
		if !seen[t] {
			continue
		}
		m := e.mob.PathfindingMalus(t)
		if m < 0 {
			return t
		}
		if m >= highest {
			highest, highestType = m, t
		}
	}
	cur := e.TypeAt(x, y, z)
	if e.width > 1 {
		cheaper := e.mob.PathfindingMalus(cur) < highest
		if cheaper && e.mob.PathfindingMalus(BigMobsCloseToDanger) < highest {
			return BigMobsCloseToDanger
		}
		return highestType
	}
	if cur == Open && highestType != Open && highest == 0 {
		return Open
	}
	return highestType
}

// TypeAt is WalkNodeEvaluator.getPathTypeStatic: the path type of one block position for any walking mob.
func (e *Evaluator) TypeAt(x, y, z int) Type {
	t := e.stateType(x, y, z)
	if t == Open && y >= e.tx.Range()[0]+1 {
		switch e.stateType(x, y-1, z) {
		case Open, Water, Lava, Walkable:
			return Open
		case Fire:
			return Fire
		case Damaging:
			return Damaging
		case StickyHoney:
			return StickyHoney
		case PowderSnow:
			return OnTopOfPowderSnow
		case DamageCautious:
			return DamageCautious
		case Trapdoor:
			return OnTopOfTrapdoor
		default:
			return e.checkNeighbours(x, y, z, Walkable)
		}
	}
	return t
}

// checkNeighbours is WalkNodeEvaluator.checkNeighbourBlocks.
func (e *Evaluator) checkNeighbours(x, y, z int, t Type) Type {
	for dx := -1; dx <= 1; dx++ {
		for dy := -1; dy <= 1; dy++ {
			for dz := -1; dz <= 1; dz++ {
				if dx == 0 && dz == 0 {
					continue
				}
				switch e.stateType(x+dx, y+dy, z+dz) {
				case Damaging:
					return DamagingInNeighbour
				case Fire, Lava:
					return FireInNeighbour
				case Water:
					return WaterBorder
				case DamageCautious:
					return DamageCautious
				}
			}
		}
	}
	return t
}

// stateType is WalkNodeEvaluator.getPathTypeFromState, cached for the search (Paper: an unloaded block is
// BLOCKED).
func (e *Evaluator) stateType(x, y, z int) Type {
	p := cube.Pos{x, y, z}
	if t, ok := e.typeCache[p]; ok {
		return t
	}
	t := StateType(e.tx, p)
	e.typeCache[p] = t
	return t
}

// StateType is WalkNodeEvaluator.getPathTypeFromState for the block at pos.
func StateType(tx *world.Tx, pos cube.Pos) Type {
	if pos.OutOfBounds(tx.Range()) {
		if pos[1] < tx.Range()[0] {
			return Blocked
		}
		return Open
	}
	info := infoOf(tx.BlockRuntimeIDAt(pos))
	switch info.kind {
	case kindAir:
		return Open
	case kindTrapdoor:
		return Trapdoor
	case kindPowderSnow:
		return PowderSnow
	case kindDamaging:
		return Damaging
	case kindHoney:
		return StickyHoney
	case kindCocoa:
		return Cocoa
	case kindCautious:
		return DamageCautious
	case kindLava:
		return Lava
	case kindFire:
		return Fire
	case kindDoorOpen:
		return DoorOpen
	case kindDoorWood:
		return DoorWoodClosed
	case kindDoorIron:
		return DoorIronClosed
	case kindRail:
		return Rail
	case kindLeaves:
		return Leaves
	case kindFence:
		return Fence
	case kindBlocked:
		if l, ok := tx.Liquid(pos); ok && l.LiquidType() == "lava" {
			return Lava
		}
		return Blocked
	case kindWater:
		return Water
	}
	if l, ok := tx.Liquid(pos); ok {
		if l.LiquidType() == "lava" {
			return Lava
		}
		return Water
	}
	return Open
}

// TypeForMob is NodeEvaluator.getPathType(mob, pos) as MoveControl.isWalkable asks it.
func (e *Evaluator) TypeForMob(tx *world.Tx, m Mob, pos cube.Pos) Type {
	if e.swim {
		if e.tx != nil {
			return e.swimCachedType(pos[0], pos[1], pos[2])
		}
		e.tx, e.mob = tx, m
		e.width, e.height, e.depth = int(math.Floor(float64(float32(m.Width())+1))), int(math.Floor(float64(float32(m.Height())+1))), int(math.Floor(float64(float32(m.Width())+1)))
		t := e.swimCachedType(pos[0], pos[1], pos[2])
		clear(e.typeCache)
		e.tx, e.mob = nil, nil
		return t
	}
	if e.tx != nil {
		return e.TypeAt(pos[0], pos[1], pos[2])
	}
	e.tx, e.mob = tx, m
	t := e.TypeAt(pos[0], pos[1], pos[2])
	clear(e.typeCache)
	e.tx, e.mob = nil, nil
	return t
}

func isWater(tx *world.Tx, pos cube.Pos) bool {
	if pos.OutOfBounds(tx.Range()) {
		return false
	}
	l, ok := tx.Liquid(pos)
	return ok && l.LiquidType() == "water"
}
