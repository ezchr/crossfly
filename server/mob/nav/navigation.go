//go:build crossfly_full

package nav

import (
	"encoding/json"
	"math"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

// Navigation is a mob's navigation: BDS NavigationComponent plus its PathNavigation subclass
// (PathNavigation, WallClimberPathNavigation, FlyingPathNavigation, HoverPathNavigation,
// GenericPathNavigation, WaterBoundPathNavigation). Goals call MoveTo/MoveToEntity/Stop; the mob
// calls Tick once per tick, after the goal selectors and before the move control.
type Navigation struct {
	Kind    Kind
	Options Options
	// Traits are used for searches unless the Agent implements TraitSource.
	Traits Traits
	// Budget limits searches per tick. When nil, the world's shared budget is used if the
	// BlockSource gives access to the world (a *world.Tx does); set NoBudget to turn it off.
	Budget   *Budget
	NoBudget bool
	// TerminationThreshold and StuckInterval are the stuck check: every StuckInterval ticks the path
	// stops if the mob moved less than TerminationThreshold blocks (NavigationComponent +40, +24).
	TerminationThreshold float64
	StuckInterval        int

	walker, swimmer, flier bool

	lastExpansions, lastLookups int
	path                        *Path
	speed                       float64
	ticks                       int
	lastStuckTick               int
	lastStuckPos                mgl64.Vec3
	endRadiusSq                 float64
	hasEndRadius                bool
	pending                     *request
	localTick                   int64

	// WallClimberPathNavigation state.
	breach      bool
	climbTarget cube.Pos
}

type request struct {
	goal   mgl64.Vec3
	entity Target
	speed  float64
}

// New returns a navigation of the given kind (NavigationComponent::initializeFromDefinition plus
// the subclass initializeInternal).
func New(kind Kind, opts Options) *Navigation {
	n := &Navigation{Kind: kind, Options: opts, TerminationThreshold: 1.5, StuckInterval: 100}
	switch kind {
	case KindGeneric:
		n.walker, n.swimmer = opts.CanWalk, opts.CanSwim
	case KindFly, KindHover:
		n.flier = true
	case KindSwim:
		n.swimmer = true
		n.Options.AvoidWater = false
	default:
		n.walker = true
	}
	return n
}

// FromComponent builds a navigation from a minecraft:navigation.* component.
func FromComponent(name string, raw json.RawMessage) (*Navigation, bool, error) {
	kind, ok := KindFromComponent(name)
	if !ok {
		return nil, false, nil
	}
	opts, err := ParseOptions(raw)
	if err != nil {
		return nil, true, err
	}
	return New(kind, opts), true, nil
}

// Walker, Swimmer and Flier report the movement abilities the navigation kind gives the mob
// (Actor::isWalker, isSwimmer, canFly).
func (n *Navigation) Walker() bool  { return n.walker }
func (n *Navigation) Swimmer() bool { return n.swimmer }
func (n *Navigation) Flier() bool   { return n.flier }

// Path returns the current path, or nil.
func (n *Navigation) Path() *Path { return n.path }

// Done reports whether the navigation has nothing to do (NavigationComponent::isDone). A queued
// request counts as work.
func (n *Navigation) Done() bool {
	return n.pending == nil && (n.path == nil || n.path.Done())
}

func (n *Navigation) pathDone() bool { return n.path == nil || n.path.Done() }

// Pending reports whether a MoveTo is waiting for path budget.
func (n *Navigation) Pending() bool { return n.pending != nil }

// Speed returns the speed multiplier given to the move control.
func (n *Navigation) Speed() float64 { return n.speed }

// SetSpeed changes the speed multiplier of the current path.
func (n *Navigation) SetSpeed(s float64) { n.speed = s }

// SetEndPathRadius makes the last node count as reached within r blocks.
func (n *Navigation) SetEndPathRadius(r float64) { n.hasEndRadius, n.endRadiusSq = true, r*r }

// LastSearch returns the node expansions and distinct block reads of the last search.
func (n *Navigation) LastSearch() (expansions, lookups int) { return n.lastExpansions, n.lastLookups }

// finders are shared by all navigations, so a mob does not keep search memory between searches.
var finders = sync.Pool{New: func() any { return NewFinder() }}

func (n *Navigation) request(a Agent) Request {
	tr := n.Traits
	if ts, ok := a.(TraitSource); ok {
		tr = ts.PathTraits()
	}
	w, h := a.Size()
	return Request{
		Pos: a.Position(), Width: w, Height: h, InWater: a.InWater(), InLava: a.InLava(),
		Walker: n.walker, Swimmer: n.swimmer, Flier: n.flier, Options: n.Options, Traits: tr,
	}
}

func (r Request) maxDist() float64 {
	if r.Traits.FollowRange > 0 {
		return r.Traits.FollowRange
	}
	return 16
}

func (n *Navigation) traits(a Agent) Traits {
	if ts, ok := a.(TraitSource); ok {
		return ts.PathTraits()
	}
	return n.Traits
}

func (n *Navigation) budget(src BlockSource) *Budget {
	if n.NoBudget {
		return nil
	}
	if n.Budget != nil {
		return n.Budget
	}
	if ws, ok := src.(worldSource); ok {
		if w := ws.World(); w != nil {
			return WorldBudget(w)
		}
	}
	return nil
}

func (n *Navigation) tick(src BlockSource) int64 {
	if ts, ok := src.(tickSource); ok {
		return ts.CurrentTick()
	}
	return n.localTick
}

// canUpdatePath is the subclass canUpdatePath: paths are only made and followed while the mob
// stands on something or is in a liquid (riding is not modelled).
func (n *Navigation) canUpdatePath(a Agent) bool {
	liquid := a.InWater() || a.InLava()
	switch n.Kind {
	case KindClimb:
		return a.HorizontalCollision() || a.OnGround() || liquid
	case KindFly, KindHover:
		return liquid || a.OnGround() || n.Options.CanPathFromAir
	case KindSwim:
		return liquid
	}
	return a.OnGround() || liquid
}

// CreatePath finds a path to the block containing pos without following it (createPath(Vec3)).
// It counts against the budget but is never deferred.
func (n *Navigation) CreatePath(src BlockSource, a Agent, pos mgl64.Vec3) *Path {
	return n.createPath(src, a, pos, nil, false)
}

// CreatePathToEntity finds a path to t without following it (createPath(Actor)).
func (n *Navigation) CreatePathToEntity(src BlockSource, a Agent, t Target) *Path {
	return n.createPath(src, a, mgl64.Vec3{}, t, false)
}

func (n *Navigation) createPath(src BlockSource, a Agent, pos mgl64.Vec3, t Target, reserved bool) *Path {
	goal := pos
	if t != nil {
		goal = t.Position()
	}
	if n.Kind == KindClimb {
		n.breach, n.climbTarget = true, cube.PosFromVec3(goal)
	}
	if !n.canUpdatePath(a) {
		return nil
	}
	r := n.request(a)
	f := finders.Get().(*Finder)
	var p *Path
	if t != nil {
		p = f.FindPath(src, r, goal, r.maxDist())
	} else {
		p = f.FindPathToBlock(src, r, cube.PosFromVec3(goal), r.maxDist())
	}
	n.lastExpansions, n.lastLookups = f.Expansions, f.Lookups
	f.release()
	finders.Put(f)
	if b := n.budget(src); b != nil {
		if reserved {
			b.Spend(n.tick(src), n.lastExpansions)
		} else {
			b.Count(n.tick(src), n.lastExpansions)
		}
	}
	return p
}

// MoveTo paths to the block containing pos and follows the path at speed (moveTo(Vec3)). It
// returns false when no path was found. When the budget is used up the request is queued and
// MoveTo returns true; Pending reports it until a later Tick runs it.
func (n *Navigation) MoveTo(src BlockSource, a Agent, pos mgl64.Vec3, speed float64) bool {
	return n.moveTo(src, a, request{goal: pos, speed: speed})
}

// MoveToEntity paths to t and follows the path (moveTo(Actor)). Wall climbers with no path walk
// straight at the target instead (WallClimberPathNavigation::moveTo).
func (n *Navigation) MoveToEntity(src BlockSource, a Agent, t Target, speed float64) bool {
	return n.moveTo(src, a, request{entity: t, speed: speed})
}

func (n *Navigation) moveTo(src BlockSource, a Agent, req request) bool {
	if b := n.budget(src); b != nil && !b.TryStart(n.tick(src)) {
		n.pending = &req
		return true
	}
	n.pending = nil
	p := n.createPath(src, a, req.goal, req.entity, true)
	if req.entity != nil && n.Kind == KindClimb && p == nil {
		n.climbTarget = cube.PosFromVec3(req.entity.Position())
		n.breach = true
		n.speed = req.speed
		return true
	}
	return n.MoveAlong(src, a, p, req.speed)
}

// MoveAlong follows p (moveTo(unique_ptr<Path>)). An identical path keeps the current progress.
func (n *Navigation) MoveAlong(src BlockSource, a Agent, p *Path, speed float64) bool {
	if p == nil {
		return false
	}
	if !p.SameAs(n.path) {
		n.path = p
	}
	if n.Options.AvoidSun && isDay(src) {
		n.trimPathFromSun(src, a)
	}
	if n.path.Len() == 0 {
		return false
	}
	n.speed = speed
	n.updateLastStuckCheck(src, a)
	n.hasEndRadius = false
	return true
}

// Stop drops the path (PathNavigation::stop). Flying, hovering and generic navigation also clear
// the move control's wanted position, as their stop overrides do.
func (n *Navigation) Stop(a Agent) {
	n.path = nil
	n.pending = nil
	switch n.Kind {
	case KindFly, KindHover, KindGeneric, KindSwim:
		a.ClearWantedPosition()
	}
}

// Tick advances along the path and hands the next waypoint to the move control. Call once per
// mob tick.
func (n *Navigation) Tick(src BlockSource, a Agent) {
	n.localTick++
	if n.pending != nil {
		req := *n.pending
		if b := n.budget(src); b == nil || b.TryStart(n.tick(src)) {
			n.pending = nil
			p := n.createPath(src, a, req.goal, req.entity, true)
			if req.entity != nil && n.Kind == KindClimb && p == nil {
				n.climbTarget, n.breach, n.speed = cube.PosFromVec3(req.entity.Position()), true, req.speed
			} else {
				n.MoveAlong(src, a, p, req.speed)
			}
		}
	}
	switch n.Kind {
	case KindClimb:
		n.climbTick(src, a)
	case KindFly, KindHover:
		n.flyTick(src, a)
	case KindGeneric, KindSwim:
		if a.InWater() || n.Kind == KindSwim {
			n.waterTick(src, a)
		} else {
			n.baseTick(src, a)
		}
	default:
		n.baseTick(src, a)
	}
}

// baseTick is PathNavigation::tick.
func (n *Navigation) baseTick(src BlockSource, a Agent) {
	n.ticks++
	if n.pathDone() {
		return
	}
	if n.canUpdatePath(a) {
		n.updatePath(src, a)
	}
	if n.pathDone() {
		return
	}
	a.SetWantedPosition(n.groundTarget(src, a), n.speed)
}

// groundTarget is the waypoint correction of PathNavigation::tick: the node position lifted onto
// the top of whatever is in or below the node block.
func (n *Navigation) groundTarget(src BlockSource, a Agent) mgl64.Vec3 {
	w, _ := a.Size()
	cur := n.path.CurrentPos(w)
	target := cur
	if !n.flier && !onlyInAir(src, target, w) {
		target[1]--
	}
	target[1] = highestBlockHeight(src, target, w)

	mp := cube.PosFromVec3(a.Position())
	mb := describeBlock(src, mp)
	if mb.props&propHollow != 0 && mb.has {
		target[1] = mb.shape.Max().Y()
	}
	tr := n.traits(a)
	if cur[1] > float64(mp[1]) && ascendable(src, mb, mp, tr) {
		target[1]++
	}
	cp := cube.PosFromVec3(cur)
	if cb := describeBlock(src, cp); cb.partial() && !cb.topPartial(cp) {
		target[1] = cb.shape.Max().Y()
	}
	if bb := describeBlock(src, down(cp)); bb.has && bb.shape.Max().Y() > target[1] {
		target[1] = bb.shape.Max().Y()
	}
	return target
}

// updatePath is PathNavigation::updatePath: skip reached nodes, shortcut swim/fly nodes, and stop
// when stuck.
func (n *Navigation) updatePath(src BlockSource, a Agent) {
	p := n.path
	mobPos := n.tempMobPos(src, a)
	w, h := a.Size()
	idx, end := p.Index(), p.Len()
	nodeType := p.NodeType(idx)
	y := floor(mobPos[1])
	for i := idx; i < end; i++ {
		if p.NodePos(i)[1] != y {
			end = i
			break
		}
	}
	r := math.Max(w*0.5, 0.6)
	rsq := r * r
	tr := n.traits(a)
	doorBlocking := false
	for i := idx; i < end; {
		np := p.NodePos(i)
		if bi := describeBlock(src, np); bi.isDoor && (tr.DoorOpener || tr.DoorBreaker) && doorBlockingPath(a, bi, p, np, i) {
			doorBlocking = true
			break
		}
		pos := p.Pos(w, i)
		lim := rsq
		if n.hasEndRadius && i == p.Len()-1 {
			lim = n.endRadiusSq
		}
		i++
		if distSq(pos, mobPos) < lim {
			p.SetIndex(i)
		}
	}
	if !doorBlocking && (nodeType == NodeSwimmable || nodeType == NodeFlyable) {
		sx, sy, sz := mobScale(w, h)
		for j := end - 1; j >= p.Index(); j-- {
			if n.canMoveDirectly(src, a, mobPos, p.Pos(w, j), sx, sy, sz) {
				p.SetIndex(j)
				break
			}
		}
	}
	n.stuckCheck(src, a, mobPos, doorBlocking)
}

func (n *Navigation) stuckCheck(src BlockSource, a Agent, mobPos mgl64.Vec3, doorBlocking bool) {
	if n.ticks-n.lastStuckTick > n.StuckInterval {
		if distSq(n.lastStuckPos, mobPos) < n.TerminationThreshold*n.TerminationThreshold && !doorBlocking {
			n.Stop(a)
		}
		n.updateLastStuckCheck(src, a)
	}
}

func (n *Navigation) updateLastStuckCheck(src BlockSource, a Agent) {
	n.lastStuckTick = n.ticks
	n.lastStuckPos = n.tempMobPos(src, a)
}

// tempMobPos is getTempMobPos: the feet position with y at the surface the mob stands on
// (NavigationUtility::getSurfaceY lifts floating mobs to the water surface).
func (n *Navigation) tempMobPos(src BlockSource, a Agent) mgl64.Vec3 {
	pos := a.Position()
	switch n.Kind {
	case KindFly, KindHover, KindSwim:
		return pos
	case KindGeneric:
		if a.InWater() {
			return pos
		}
	}
	y := floor(pos[1])
	if a.InWater() && n.Options.CanFloat {
		x, z := floor(pos[0]), floor(pos[2])
		for i := 0; ; i++ {
			if !waterAt(src, cube.Pos{x, y, z}) {
				break
			}
			y++
			if i >= 16 {
				y = floor(pos[1])
				break
			}
		}
	}
	return mgl64.Vec3{pos[0], float64(y), pos[2]}
}

// climbTick is WallClimberPathNavigation::tick: climb while pushing against a wall, and walk straight
// at the target once the path is used up.
func (n *Navigation) climbTick(src BlockSource, a Agent) {
	if c, ok := a.(Climber); ok {
		// _isInsideBorderBlock only concerns Education Edition border blocks.
		c.SetClimbing(n.breach && a.HorizontalCollision())
	}
	if !(n.pathDone() && n.breach) {
		n.baseTick(src, a)
		return
	}
	w, h := a.Size()
	lim := w * w * 0.75
	pos := a.Position()
	t := n.climbTarget
	above := float64(t[1]) < pos[1] && distSqToBlockCentre(pos, cube.Pos{t[0], floor(pos[1]), t[2]}) < lim
	if distSqToBlockCentre(pos, t) < lim || above {
		n.breach = false
		return
	}
	cw := ceil(w)
	if canWalkDirectly(src, a, n.traits(a), pos, t.Vec3(), cw, 2*ceil(h), cw, true, n.flier) {
		a.SetWantedPosition(mgl64.Vec3{float64(t[0]) + 0.5, float64(t[1]), float64(t[2]) + 0.5}, n.speed)
		return
	}
	n.breach = false
}

// flyTick is FlyingPathNavigation::tick (HoverPathNavigation::tick is the same for pathing).
func (n *Navigation) flyTick(src BlockSource, a Agent) {
	n.ticks++
	if n.pathDone() {
		return
	}
	w, _ := a.Size()
	p := n.path
	if n.canUpdatePath(a) {
		mobPos := a.Position()
		idx, end := p.Index(), p.Len()
		y := floor(mobPos[1])
		for i := idx; i < end; i++ {
			if p.NodePos(i)[1] != y {
				end = i
				break
			}
		}
		rsq := w * w
		for i := idx; i < end; {
			pos := p.Pos(w, i)
			i++
			if distSq(pos, mobPos) < rsq {
				p.SetIndex(i)
			}
		}
		// NavigationUtility::canFlyDirectly is canMove(): always true for a mob that can move.
		if j := end - 1; j != p.Index()-1 && j >= 0 {
			p.SetIndex(j)
		}
		n.stuckCheck(src, a, mobPos, false)
	} else if p.Index() < p.Len() {
		if distSq(p.Pos(w, p.Index()), a.Position()) <= w*w {
			p.Next()
		}
	}
	if n.pathDone() {
		return
	}
	a.SetWantedPosition(n.path.CurrentPos(w), n.speed)
}

// waterTick is GenericPathNavigation::tick while in water (also used for navigation.swim).
func (n *Navigation) waterTick(src BlockSource, a Agent) {
	n.ticks++
	if n.pathDone() {
		return
	}
	if n.canUpdatePath(a) {
		n.updatePath(src, a)
	}
	if n.pathDone() {
		return
	}
	w, _ := a.Size()
	p := n.path
	switch p.NodeType(p.Index()) {
	case NodeSwimmable:
		mp, np := cube.PosFromVec3(n.tempMobPos(src, a)), cube.PosFromVec3(p.Pos(w, p.Index()))
		if !a.OnGround() && mp == np {
			p.Next()
		}
	case NodeBreachable:
		p.Next()
	default:
		if !n.pathDone() {
			a.SetWantedPosition(n.groundTarget(src, a), n.speed)
		}
		return
	}
	if !n.pathDone() {
		a.SetWantedPosition(p.CurrentPos(w), n.speed)
	}
}

// trimPathFromSun is NavigationUtility::trimPathFromSun: a mob in the shade cuts its path before
// the first node under open sky.
func (n *Navigation) trimPathFromSun(src BlockSource, a Agent) {
	ss, ok := src.(skySource)
	if !ok || n.path == nil {
		return
	}
	canSee := func(p cube.Pos) bool { return p[1] > ss.HighestLightBlocker(p[0], p[2]) }
	pos := a.Position()
	if canSee(cube.Pos{floor(pos[0]), floor(pos[1] + 0.05), floor(pos[2])}) {
		return
	}
	for i := 0; i < n.path.Len(); i++ {
		if canSee(n.path.NodePos(i)) {
			if i > 0 {
				n.path.SetSize(i - 1)
			} else {
				n.path.SetSize(0)
			}
			return
		}
	}
}

// isDay approximates the dimension's daytime check used before trimming paths from the sun.
func isDay(src BlockSource) bool {
	ws, ok := src.(worldSource)
	if !ok || ws.World() == nil {
		return true
	}
	t := ws.World().Time() % 24000
	return t < 12542 || t > 23460
}

// canMoveDirectly is NavigationUtility::canMoveDirectly.
func (n *Navigation) canMoveDirectly(src BlockSource, a Agent, from, to mgl64.Vec3, sx, sy, sz int) bool {
	if n.swimmer {
		if waterAt(src, cube.PosFromVec3(from)) {
			return waterAt(src, cube.PosFromVec3(to))
		}
	}
	if n.flier {
		return true
	}
	if n.walker {
		return canWalkDirectly(src, a, n.traits(a), from, to, sx, sy, sz, false, n.flier)
	}
	return false
}

// doorBlockingPath is NavigationUtility::isDoorBlockingPath.
func doorBlockingPath(a Agent, bi *blockInfo, p *Path, np cube.Pos, i int) bool {
	if i+1 >= p.Len() {
		return false
	}
	next := p.NodePos(i + 1)
	dir := direction(next[0]-np[0], next[2]-np[2])
	if bi.panel == dir {
		return true
	}
	if bi.panel == dir.Opposite() {
		w, h := a.Size()
		pos := a.Position()
		box := cube.Box(pos[0]-w/2, pos[1], pos[2]-w/2, pos[0]+w/2, pos[1]+h, pos[2]+w/2)
		cell := cube.Box(0, 0, 0, 1, 1, 1).Translate(np.Vec3())
		return !box.IntersectsWith(cell)
	}
	return false
}

// mobScale is NavigationUtility::getMobScale.
func mobScale(w, h float64) (int, int, int) {
	c := ceil(w)
	if c <= 1 {
		c = 2
	}
	s := c + c/2
	return s, floor(h) + 1, s
}

// canWalkDirectly is NavigationUtility::canWalkDirectly: a grid walk from from to to that checks
// canWalkOn for every column crossed.
func canWalkDirectly(src BlockSource, a Agent, tr Traits, from, to mgl64.Vec3, sx, sy, sz int, safeOnly, flier bool) bool {
	x, z := floor(from[0]), floor(from[2])
	dx, dz := to[0]-from[0], to[2]-from[2]
	l := dx*dx + dz*dz
	if l < 0.000001 {
		return false
	}
	inv := 1 / math.Sqrt(l)
	dx, dz = dx*inv, dz*inv
	y := floor(from[1])
	if !canWalkOn(src, a, tr, x, y, z, sx+2, sy, sz+2, from, dx, dz, safeOnly, flier) {
		return false
	}
	tx, tz := float64(x)-from[0], float64(z)-from[2]
	if dx >= 0 {
		tx++
	}
	if dz >= 0 {
		tz++
	}
	stepX, stepZ := 1, 1
	if dx < 0 {
		stepX = -1
	}
	if dz < 0 {
		stepZ = -1
	}
	tx /= dx
	tz /= dz
	ex, ez := floor(to[0]), floor(to[2])
	remX, remZ := ex-x, ez-z
	for {
		if stepX*remX <= 0 && stepZ*remZ <= 0 {
			return true
		}
		otx, otz := tx, tz
		if tx >= tz {
			tz += 1 / math.Abs(dz)
		} else {
			tx += 1 / math.Abs(dx)
		}
		if otz <= otx {
			z += stepZ
			remZ = ez - z
		} else {
			x += stepX
			remX = ex - x
		}
		if !canWalkOn(src, a, tr, x, y, z, sx, sy, sz, from, dx, dz, safeOnly, flier) {
			return false
		}
	}
}

// canWalkOn is NavigationUtility::canWalkOn: the blocks of a footprint ahead of from must be
// passable (or, with safeOnly, merely harmless), and the blocks under it must hold the mob.
func canWalkOn(src BlockSource, a Agent, tr Traits, x, y, z, sx, sy, sz int, from mgl64.Vec3, dx, dz float64, safeOnly, flier bool) bool {
	x0, z0 := x-sx/2, z-sz/2
	ahead := func(bx, bz int) bool {
		return (float64(bz)+0.5-from[2])*dz+(float64(bx)+0.5-from[0])*dx >= 0
	}
	f := &Finder{src: src, r: Request{Traits: tr}}
	corner := cube.Pos{x0, y, z0}
	for bx := x0; bx < x0+sx; bx++ {
		for by := y; by < y+sy; by++ {
			for bz := z0; bz < z0+sz; bz++ {
				if !ahead(bx, bz) {
					continue
				}
				p := cube.Pos{bx, by, bz}
				bi := f.info(p)
				if bi.air {
					continue
				}
				safe := bi.props&propCausesDamage == 0
				if bi.mat == matLava {
					safe = tr.FireImmune
				}
				ok := f.pathable(bi, corner, p)
				if safeOnly {
					ok = safe
				}
				if !ok {
					return false
				}
			}
		}
	}
	if flier {
		return true
	}
	for bx := x0; bx < x0+sx; bx++ {
		for bz := z0; bz < z0+sz; bz++ {
			if !ahead(bx, bz) {
				continue
			}
			below := f.info(cube.Pos{bx, y - 1, bz})
			if !safeOnly && below.air {
				return false
			}
			if below.props&propDoor != 0 || below.isDoor || !moveCondition(a, tr, below) {
				return false
			}
		}
	}
	return true
}

// moveCondition is NavigationUtility::moveCondition.
func moveCondition(a Agent, tr Traits, bi *blockInfo) bool {
	if !tr.FireImmune && bi.magma {
		return false
	}
	return (a.InWater() || bi.mat != matWater) && bi.mat != matLava
}

// ascendable is BlockClimberSystem::canBlockBeAscendedByJumping.
func ascendable(src BlockSource, bi *blockInfo, pos cube.Pos, tr Traits) bool {
	switch {
	case bi.powderSnow:
		return tr.ClimbPowderSnow
	case bi.scaffolding:
		b := describeBlock(src, down(pos))
		return !b.air && b.mat != matWater && tr.BlockClimber
	}
	return false
}

// onlyInAir is PathNavigation::_isPositionOnlyInAir.
func onlyInAir(src BlockSource, pos mgl64.Vec3, w float64) bool {
	h := w * 0.5
	y := int(pos[1])
	for x := floor(pos[0] - h); x <= floor(pos[0]+h); x++ {
		for z := floor(pos[2] - h); z <= floor(pos[2]+h); z++ {
			if !describeBlock(src, cube.Pos{x, y, z}).air {
				return false
			}
		}
	}
	return true
}

// highestBlockHeight is PathNavigation::_getHighestBlockHeight: the highest collision (or water
// surface) top in the footprint at pos, but at least pos.y. Doors and chests are ignored.
func highestBlockHeight(src BlockSource, pos mgl64.Vec3, w float64) float64 {
	h := w * 0.5
	top := pos[1]
	y := floor(pos[1])
	for x := floor(pos[0] - h); x <= floor(pos[0]+h); x++ {
		for z := floor(pos[2] - h); z <= floor(pos[2]+h); z++ {
			bi := describeBlock(src, cube.Pos{x, y, z})
			if bi.props&propDoor != 0 || bi.props&propWalkable != 0 && bi.name == "minecraft:chest" {
				continue
			}
			if bi.mat == matWater {
				top = math.Max(float64(y)+bi.waterSurface, top)
			}
			if bi.has {
				top = math.Max(bi.shape.Max().Y(), top)
			}
		}
	}
	return top
}

func distSqToBlockCentre(pos mgl64.Vec3, b cube.Pos) float64 {
	return distSq(pos, b.Vec3Centre())
}
