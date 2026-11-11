//go:build crossfly_full

package path

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Kind is the kind of navigation a mob has (the PathNavigation subclass its createNavigation returns).
type Kind uint8

const (
	// Ground is GroundPathNavigation (zombies, skeletons, creepers, endermen, animals).
	Ground Kind = iota
	// WallClimber is WallClimberNavigation (spiders): ground navigation that walks straight at the target once the
	// path ends, so the spider climbs the wall in its way.
	WallClimber
	// WaterBound is WaterBoundPathNavigation (guardians, fish, dolphins): see NewWaterNavigation.
	WaterBound
)

// Navigation is a PathNavigation of one mob: it finds paths and feeds their nodes to the mob's move control.
// Water-bound, flying and amphibious navigation are to come; they plug in as more Kinds.
type Navigation struct {
	kind Kind
	eval *Evaluator
	// walk is the walk evaluator a water-bound navigation keeps for WalkTypeAt.
	walk *Evaluator
	find *Finder

	path          *Path
	speedModifier float64
	tick          int64
	lastStuck     int64
	lastStuckPos  mgl64.Vec3
	timeoutNode   cube.Pos
	timeoutTimer  int64
	lastTimeout   int64
	timeoutLimit  float64
	maxDistToWP   float64
	delayedRecomp bool
	lastRecompute int64
	targetPos     *cube.Pos
	reachRange    int
	visitedMul    float32
	stuck         bool
	requiredLen   float32

	// avoidSun and belowSurface are GroundPathNavigation's settings.
	avoidSun, belowSurface bool
	// climbTo is WallClimberNavigation.pathToPosition.
	climbTo *cube.Pos

	// Paper's "Optimize Pathfinding" throttle of moveTo(Entity).
	failures, lastFailure int64
}

// NewNavigation returns a navigation of the kind for m (PathNavigation's constructor: the path finder visits at
// most floor(follow range base * 16) nodes).
func NewNavigation(kind Kind, m Mob) *Navigation {
	e := NewEvaluator()
	n := &Navigation{kind: kind, eval: e, visitedMul: 1, requiredLen: 16, maxDistToWP: 0.5}
	n.find = NewFinder(e, int(math.Floor(m.FollowRangeBase()*16)))
	return n
}

// Evaluator returns the node evaluator (setCanOpenDoors and so on).
func (n *Navigation) Evaluator() *Evaluator { return n.eval }

// SetAvoidSun is GroundPathNavigation.setAvoidSun.
func (n *Navigation) SetAvoidSun(v bool) { n.avoidSun = v }

// SetCanPathToTargetsBelowSurface is GroundPathNavigation.setCanPathToTargetsBelowSurface.
func (n *Navigation) SetCanPathToTargetsBelowSurface(v bool) { n.belowSurface = v }

// SetCanOpenDoors, SetCanPassDoors, SetCanFloat and SetCanWalkOverFences are the node evaluator settings.
func (n *Navigation) SetCanOpenDoors(v bool) { n.eval.SetCanOpenDoors(v) }
func (n *Navigation) SetCanPassDoors(v bool) { n.eval.SetCanPassDoors(v) }
func (n *Navigation) SetCanFloat(v bool) {
	if n.kind != WaterBound { // WaterBoundPathNavigation.setCanFloat does nothing.
		n.eval.SetCanFloat(v)
	}
}
func (n *Navigation) SetCanWalkOverFences(v bool) { n.eval.SetCanWalkOverFences(v) }

// CanFloat reports whether the mob floats in water while it paths.
func (n *Navigation) CanFloat() bool { return n.eval.CanFloat() }

// SetRequiredPathLength is PathNavigation.setRequiredPathLength.
func (n *Navigation) SetRequiredPathLength(m Mob, l float32) {
	n.requiredLen = l
	n.find.MaxVisitedNodes = int(math.Floor(float64(n.maxPathLength(m) * 16)))
}

// SetMaxVisitedNodesMultiplier is PathNavigation.setMaxVisitedNodesMultiplier.
func (n *Navigation) SetMaxVisitedNodesMultiplier(v float32) { n.visitedMul = v }

// ResetMaxVisitedNodesMultiplier is PathNavigation.resetMaxVisitedNodesMultiplier.
func (n *Navigation) ResetMaxVisitedNodesMultiplier() { n.visitedMul = 1 }

func (n *Navigation) maxPathLength(m Mob) float32 {
	return max(float32(m.FollowRange()), n.requiredLen)
}

// SetSpeedModifier is PathNavigation.setSpeedModifier.
func (n *Navigation) SetSpeedModifier(v float64) { n.speedModifier = v }

// Path returns the current path, or nil.
func (n *Navigation) Path() *Path { return n.path }

// TargetPos returns the block the last found path leads to.
func (n *Navigation) TargetPos() (cube.Pos, bool) {
	if n.targetPos == nil {
		return cube.Pos{}, false
	}
	return *n.targetPos, true
}

// Done reports whether there is no path left to follow (isDone).
func (n *Navigation) Done() bool { return n.path == nil || n.path.Done() }

// InProgress is !Done.
func (n *Navigation) InProgress() bool { return !n.Done() }

// Stuck reports whether the last stuck check found the mob stuck.
func (n *Navigation) Stuck() bool { return n.stuck }

// Stop drops the path.
func (n *Navigation) Stop() { n.path = nil }

// canUpdatePath is GroundPathNavigation.canUpdatePath.
func (n *Navigation) canUpdatePath(m Mob) bool {
	if n.kind == WaterBound {
		return n.eval.breach || m.InLiquid()
	}
	return m.OnGround() || m.InLiquid() || m.Passenger()
}

// RecomputePath is PathNavigation.recomputePath.
func (n *Navigation) RecomputePath(m Mob) {
	now := m.Tx().CurrentTick()
	if now-n.lastRecompute <= 20 || !n.canUpdatePath(m) {
		n.delayedRecomp = true
	} else if n.targetPos != nil {
		n.path = nil
		n.path = n.CreatePath(m, *n.targetPos, n.reachRange)
		n.lastRecompute = now
		n.delayedRecomp = false
	}
}

// CreatePath is GroundPathNavigation.createPath(BlockPos, reachRange): a path to the surface above or below pos
// (unless the mob may path to targets below the surface).
func (n *Navigation) CreatePath(m Mob, pos cube.Pos, reachRange int) *Path {
	if n.kind == WallClimber {
		p := pos
		n.climbTo = &p
	}
	tx := m.Tx()
	if _, loaded := tx.BlockLoaded(pos); !loaded {
		return nil
	}
	if !n.belowSurface && n.kind != WaterBound {
		pos = surfacePosition(tx, pos)
	}
	return n.createPath(m, []cube.Pos{pos}, 8, false, reachRange, n.maxPathLength(m))
}

// CreatePathToEntity is createPath(Entity, reachRange): a path to the block the entity is in.
func (n *Navigation) CreatePathToEntity(m Mob, e world.Entity, reachRange int) *Path {
	return n.CreatePath(m, cube.PosFromVec3(e.Position()), reachRange)
}

// CreatePathToAny is createPath(Set<BlockPos>, reachRange): the best path to any of the positions.
func (n *Navigation) CreatePathToAny(m Mob, targets []cube.Pos, reachRange int) *Path {
	return n.createPath(m, targets, 8, false, reachRange, n.maxPathLength(m))
}

// surfacePosition is GroundPathNavigation.findSurfacePosition.
func surfacePosition(tx *world.Tx, pos cube.Pos) cube.Pos {
	r := tx.Range()
	air := func(p cube.Pos) bool { return infoOf(tx.BlockRuntimeIDAt(p)).kind == kindAir }
	if air(pos) {
		p := pos.Side(cube.FaceDown)
		for p[1] >= r[0] && air(p) {
			p = p.Side(cube.FaceDown)
		}
		if p[1] >= r[0] {
			return p.Side(cube.FaceUp)
		}
		p[1] = pos[1] + 1
		for p[1] <= r[1] && air(p) {
			p = p.Side(cube.FaceUp)
		}
		pos = p
	}
	solid := func(p cube.Pos) bool { return !p.OutOfBounds(r) && infoOf(tx.BlockRuntimeIDAt(p)).solid }
	if !solid(pos) {
		return pos
	}
	p := pos.Side(cube.FaceUp)
	for p[1] <= r[1] && solid(p) {
		p = p.Side(cube.FaceUp)
	}
	return p
}

// createPath is PathNavigation.createPath(Set, radiusOffset, above, reachRange, maxPathLength) with Paper's check
// that targets are within the world border (the border is the world's range here).
func (n *Navigation) createPath(m Mob, targets []cube.Pos, _ int, above bool, reachRange int, maxLen float32) *Path {
	if len(targets) == 0 {
		return nil
	}
	tx := m.Tx()
	if m.Position()[1] < float64(tx.Range()[0]) || !n.canUpdatePath(m) {
		return nil
	}
	if n.path != nil && !n.path.Done() && n.targetPos != nil {
		for _, t := range targets {
			if t == *n.targetPos {
				return n.path
			}
		}
	}
	p := n.find.FindPath(tx, m, targets, maxLen, reachRange, n.visitedMul)
	if p != nil {
		t := p.Target()
		n.targetPos = &t
		n.reachRange = reachRange
		n.resetStuckTimeout()
	}
	return p
}

// MoveTo is moveTo(x, y, z, speed): find a path to the block and follow it.
func (n *Navigation) MoveTo(m Mob, pos mgl64.Vec3, reachRange int, speed float64) bool {
	return n.MoveAlong(m, n.CreatePath(m, cube.PosFromVec3(pos), reachRange), speed)
}

// MoveToEntity is moveTo(Entity, speed) with Paper's throttle: after more than 10 failed searches in a row, no
// new search for 40 ticks after the last failure. For a wall climber it is WallClimberNavigation.moveTo: without a
// path the spider walks straight at the entity.
func (n *Navigation) MoveToEntity(m Mob, e world.Entity, speed float64) bool {
	if n.kind == WallClimber {
		// WallClimberNavigation.moveTo overrides Paper's throttled one.
		if p := n.CreatePathToEntity(m, e, 0); p != nil {
			return n.MoveAlong(m, p, speed)
		}
		pos := cube.PosFromVec3(e.Position())
		n.climbTo = &pos
		n.speedModifier = speed
		return true
	}
	now := m.Tx().CurrentTick()
	if n.failures > 10 && n.path == nil && now < n.lastFailure+40 {
		return false
	}
	p := n.CreatePathToEntity(m, e, 1)
	ok := p != nil && n.MoveAlong(m, p, speed)
	if ok {
		n.failures, n.lastFailure = 0, 0
	} else {
		n.failures++
		n.lastFailure = now
	}
	return ok
}

// MoveAlong is moveTo(Path, speed): follow the path.
func (n *Navigation) MoveAlong(m Mob, p *Path, speed float64) bool {
	if p == nil {
		n.path = nil
		return false
	}
	if !p.SameAs(n.path) {
		n.path = p
	}
	if n.Done() {
		return false
	}
	n.trimPath(m)
	if n.path.NodeCount() <= 0 {
		return false
	}
	n.speedModifier = speed
	n.lastStuck = n.tick
	n.lastStuckPos = n.tempMobPos(m)
	return true
}

// Tick is PathNavigation.tick (WallClimberNavigation.tick for a wall climber).
func (n *Navigation) Tick(m Mob) {
	if n.kind == WallClimber && n.Done() {
		if n.climbTo != nil {
			pos, at := m.Position(), *n.climbTo
			w := m.Width()
			if !closerToCentre(at, pos, w) && (!(pos[1] > float64(at[1])) || !closerToCentre(cube.Pos{at[0], int(math.Floor(pos[1])), at[2]}, pos, w)) {
				m.SetWantedPosition(float64(at[0]), float64(at[1]), float64(at[2]), n.speedModifier)
			} else {
				n.climbTo = nil
			}
		}
		return
	}
	n.tick++
	if n.delayedRecomp {
		n.RecomputePath(m)
	}
	if n.Done() {
		return
	}
	if n.canUpdatePath(m) {
		n.followThePath(m)
	} else if n.path != nil && !n.path.Done() {
		mp := n.tempMobPos(m)
		next := n.path.NextEntityPos(m.Width())
		if mp[1] > next[1] && !m.OnGround() && math.Floor(mp[0]) == math.Floor(next[0]) && math.Floor(mp[2]) == math.Floor(next[2]) {
			n.path.Advance()
		}
	}
	if !n.Done() {
		t := n.path.NextEntityPos(m.Width())
		m.SetWantedPosition(t[0], n.groundY(m.Tx(), t), t[2], n.speedModifier)
	}
}

func closerToCentre(p cube.Pos, v mgl64.Vec3, d float64) bool {
	return mgl64.Vec3{float64(p[0]) + 0.5, float64(p[1]) + 0.5, float64(p[2]) + 0.5}.Sub(v).LenSqr() < d*d
}

// groundY is PathNavigation.getGroundY.
func (n *Navigation) groundY(tx *world.Tx, t mgl64.Vec3) float64 {
	if n.kind == WaterBound {
		return t[1]
	}
	pos := cube.PosFromVec3(t)
	if infoOf(tx.BlockRuntimeIDAt(pos.Side(cube.FaceDown))).kind == kindAir {
		return t[1]
	}
	return FloorLevel(tx, pos)
}

// followThePath is PathNavigation.followThePath.
func (n *Navigation) followThePath(m Mob) {
	mp := n.tempMobPos(m)
	w := float64(float32(m.Width()))
	if w > 0.75 {
		n.maxDistToWP = w / 2
	} else {
		n.maxDistToWP = 0.75 - w/2
	}
	next := n.path.NextNode()
	pos := m.Position()
	xd := math.Abs(pos[0] - (float64(next.X) + 0.5))
	yd := math.Abs(pos[1] - float64(next.Y))
	zd := math.Abs(pos[2] - (float64(next.Z) + 0.5))
	maxY := 1.0 // getMaxVerticalDistanceToWaypoint
	if n.kind == WaterBound {
		maxY = float64(float32(0.5))
	}
	close := xd < n.maxDistToWP && zd < n.maxDistToWP && yd < maxY
	if close || canCutCorner(next.Type) && n.shouldTargetNextNodeInDirection(m, mp) {
		n.path.Advance()
	}
	n.doStuckDetection(m, mp)
}

// canCutCorner is PathNavigation.canCutCorner.
func canCutCorner(t Type) bool {
	return t != FireInNeighbour && t != DamagingInNeighbour && t != WalkableDoor
}

func (n *Navigation) shouldTargetNextNodeInDirection(m Mob, mp mgl64.Vec3) bool {
	if n.path.NextIndex()+1 >= n.path.NodeCount() {
		return false
	}
	cur := n.path.NextNode().BottomCentre()
	if cur.Sub(mp).LenSqr() >= 4 {
		return false
	}
	// GroundPathNavigation.canMoveDirectly is false.
	nextNode := n.path.Node(n.path.NextIndex() + 1).BottomCentre()
	toCur, toNext := cur.Sub(mp), nextNode.Sub(mp)
	curSqr, nextSqr := toCur.LenSqr(), toNext.LenSqr()
	if !(nextSqr < curSqr) && !(curSqr < 0.5) {
		return false
	}
	return safeNormalize(toNext).Dot(safeNormalize(toCur)) < 0
}

func safeNormalize(v mgl64.Vec3) mgl64.Vec3 {
	l := v.Len()
	if l < 1e-5 {
		return mgl64.Vec3{}
	}
	return v.Mul(1 / l)
}

// doStuckDetection is PathNavigation.doStuckDetection.
func (n *Navigation) doStuckDetection(m Mob, mp mgl64.Vec3) {
	if n.tick-n.lastStuck > 100 {
		speed := float32(m.MoveSpeed())
		eff := speed
		if speed < 1 {
			eff = speed * speed
		}
		threshold := float64(eff * 100 * 0.25)
		if mp.Sub(n.lastStuckPos).LenSqr() < threshold*threshold {
			n.stuck = true
			n.Stop()
		} else {
			n.stuck = false
		}
		n.lastStuck = n.tick
		n.lastStuckPos = mp
	}
	if n.path != nil && !n.path.Done() {
		p := n.path.NextNode().Pos()
		now := m.Tx().CurrentTick()
		if p == n.timeoutNode {
			n.timeoutTimer += now - n.lastTimeout
		} else {
			n.timeoutNode = p
			d := mp.Sub(n.path.NextNode().BottomCentre()).Len()
			n.timeoutLimit = 0
			if s := m.MoveSpeed(); s > 0 {
				n.timeoutLimit = d / float64(float32(s)) * 20
			}
		}
		if n.timeoutLimit > 0 && float64(n.timeoutTimer) > n.timeoutLimit*3 {
			n.resetStuckTimeout()
			n.Stop()
		}
		n.lastTimeout = now
	}
}

func (n *Navigation) resetStuckTimeout() {
	n.timeoutNode = cube.Pos{}
	n.timeoutTimer = 0
	n.timeoutLimit = 0
	n.stuck = false
}

// tempMobPos is GroundPathNavigation.getTempMobPos.
func (n *Navigation) tempMobPos(m Mob) mgl64.Vec3 {
	pos := m.Position()
	if n.kind == WaterBound { // WaterBoundPathNavigation.getTempMobPos: getY(0.5).
		return mgl64.Vec3{pos[0], pos[1] + m.Height()*0.5, pos[2]}
	}
	return mgl64.Vec3{pos[0], float64(n.surfaceY(m)), pos[2]}
}

// surfaceY is GroundPathNavigation.getSurfaceY.
func (n *Navigation) surfaceY(m Mob) int {
	pos := m.Position()
	if m.InFloatableFluid() && n.CanFloat() {
		y := int(math.Floor(pos[1]))
		x, z := int(math.Floor(pos[0])), int(math.Floor(pos[2]))
		steps := 0
		for isWater(m.Tx(), cube.Pos{x, y, z}) {
			y++
			if steps++; steps > 16 {
				return int(math.Floor(pos[1]))
			}
		}
		return y
	}
	return int(math.Floor(pos[1] + 0.5))
}

// trimPath is GroundPathNavigation.trimPath: up out of cauldrons, and cut before the first node under the sky for a
// mob that avoids the sun.
func (n *Navigation) trimPath(m Mob) {
	tx := m.Tx()
	if n.path == nil || n.kind == WaterBound {
		return
	}
	for i := 0; i < n.path.NodeCount(); i++ {
		node := n.path.Node(i)
		if infoOf(tx.BlockRuntimeIDAt(node.Pos())).cauldron {
			n.path.ReplaceNode(i, node.cloneAndMove(node.X, node.Y+1, node.Z))
			if i+1 < n.path.NodeCount() {
				next := n.path.Node(i + 1)
				if node.Y >= next.Y {
					n.path.ReplaceNode(i+1, node.cloneAndMove(next.X, node.Y+1, next.Z))
				}
			}
		}
	}
	if !n.avoidSun {
		return
	}
	pos := m.Position()
	if CanSeeSky(tx, cube.PosFromVec3(mgl64.Vec3{pos[0], pos[1] + 0.5, pos[2]})) {
		return
	}
	for i := 0; i < n.path.NodeCount(); i++ {
		if CanSeeSky(tx, n.path.Node(i).Pos()) {
			n.path.TruncateNodes(i)
			return
		}
	}
}

// CanSeeSky is Level.canSeeSky: nothing that blocks light above pos.
func CanSeeSky(tx *world.Tx, pos cube.Pos) bool {
	return pos[1] > tx.HighestLightBlocker(pos[0], pos[2])
}

// IsStableDestination is PathNavigation.isStableDestination: the block below is solid.
func IsStableDestination(tx *world.Tx, pos cube.Pos) bool {
	return infoOf(tx.BlockRuntimeIDAt(pos.Side(cube.FaceDown))).solidRender
}

// ShouldRecomputePath is PathNavigation.shouldRecomputePath: a block changed near the rest of the path.
func (n *Navigation) ShouldRecomputePath(m Mob, pos cube.Pos) bool {
	if n.delayedRecomp || n.path == nil || n.path.Done() || n.path.NodeCount() == 0 {
		return false
	}
	t := n.path.EndNode()
	mp := m.Position()
	mid := mgl64.Vec3{(float64(t.X) + mp[0]) / 2, (float64(t.Y) + mp[1]) / 2, (float64(t.Z) + mp[2]) / 2}
	d := float64(n.path.NodeCount() - n.path.NextIndex())
	c := mgl64.Vec3{float64(pos[0]) + 0.5, float64(pos[1]) + 0.5, float64(pos[2]) + 0.5}
	return c.Sub(mid).LenSqr() < d*d
}

// TypeAt is NodeEvaluator.getPathType(mob, pos) (MoveControl.isWalkable).
func (n *Navigation) TypeAt(m Mob, pos cube.Pos) Type { return n.eval.TypeForMob(m.Tx(), m, pos) }
