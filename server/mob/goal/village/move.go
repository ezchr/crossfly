//go:build crossfly_full

package village

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	ccore "github.com/df-mc/dragonfly/server/mob/component/core"
	vc "github.com/df-mc/dragonfly/server/mob/component/village"
	"github.com/df-mc/dragonfly/server/mob/nav"
	"github.com/go-gl/mathgl/mgl64"
)

// moveTowardsRestriction is BDS MoveTowardsRestrictionGoal and its home
// (minecraft:home) and dwelling (village bounds) variants: outside the
// restriction, walk to RandomPos::getPosTowards(16, 7, restriction).
type moveTowardsRestriction struct {
	mob.GoalBase
	speed    float64
	dwelling bool
	wanted   mgl64.Vec3
}

func newMoveTowardsRestriction(dwelling bool) mob.GoalFactory {
	return func(spec mob.GoalSpec) (mob.Goal, error) {
		j := struct {
			Speed *float64 `json:"speed_multiplier"`
		}{}
		if err := decode(spec, &j); err != nil {
			return nil, err
		}
		g := &moveTowardsRestriction{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)}, speed: 1, dwelling: dwelling}
		if j.Speed != nil {
			g.speed = *j.Speed
		}
		return g, nil
	}
}

func (g *moveTowardsRestriction) CanUse(m *mob.Mob) bool {
	if !hasNav(m) {
		return false
	}
	var target mgl64.Vec3
	if g.dwelling {
		if m.HasFamily("villager") && m.Meta().InLove {
			return false
		}
		v, ok := vc.VillageOf(m)
		if !ok || v.Within(m.Position(), 0) {
			return false
		}
		target = v.Center()
	} else {
		p, _, ok := vc.HomeRestriction(m)
		if !ok || vc.WithinHomeRestriction(m, m.Position()) {
			return false
		}
		target = p.Vec3Centre()
	}
	pos, ok := posTowards(m, 16, 7, target)
	if !ok {
		return false
	}
	g.wanted = pos
	return true
}

func (g *moveTowardsRestriction) Start(m *mob.Mob) {
	m.Navigation().MoveTo(m, centre(g.wanted), g.speed)
}

func (g *moveTowardsRestriction) CanContinueToUse(m *mob.Mob) bool { return !m.Navigation().Done() }

func (g *moveTowardsRestriction) Stop(m *mob.Mob) { m.Navigation().Stop(m) }

// openDoorGoal is minecraft:behavior.open_door (BDS OpenDoorGoal, a
// DoorInteractGoal): when the path's next node is a closed wooden door
// within reach, open it, and with close_door_after close it 20 ticks later.
type openDoorGoal struct {
	mob.GoalBase
	closeAfter bool
	door       cube.Pos
	timer      int
}

func newOpenDoor(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Close *bool `json:"close_door_after"`
	}{}
	if err := decode(spec, &j); err != nil {
		return nil, err
	}
	g := &openDoorGoal{GoalBase: mob.GoalBase{Control: spec.FlagsOr(0)}, closeAfter: true}
	if j.Close != nil {
		g.closeAfter = *j.Close
	}
	return g, nil
}

// findDoor is DoorInteractGoal::canUse: a wooden door on one of the next
// two path nodes within 1.5 blocks.
func findDoor(m *mob.Mob) (cube.Pos, bool) {
	p := m.Navigation().Path()
	if p == nil || m.Navigation().Done() {
		return cube.Pos{}, false
	}
	pos := m.Position()
	for i := p.Index(); i < min(p.Len(), p.Index()+2); i++ {
		n := p.Node(i)
		for _, c := range []cube.Pos{n, n.Side(cube.FaceUp)} {
			if vc.IsWoodDoor(m.Tx(), c) && distSq(c.Vec3Centre(), pos) < 2.25*2 {
				return c, true
			}
		}
	}
	return cube.Pos{}, false
}

func (g *openDoorGoal) CanUse(m *mob.Mob) bool {
	d, ok := findDoor(m)
	if !ok {
		return false
	}
	g.door = d
	return true
}

func (g *openDoorGoal) CanContinueToUse(m *mob.Mob) bool {
	return g.closeAfter && g.timer > 0 && vc.IsWoodDoor(m.Tx(), g.door)
}

func (g *openDoorGoal) Start(m *mob.Mob) {
	g.timer = 20
	vc.SetDoor(m.Tx(), g.door, true)
}

func (g *openDoorGoal) Tick(*mob.Mob) { g.timer-- }

func (g *openDoorGoal) Stop(m *mob.Mob) {
	if g.closeAfter {
		vc.SetDoor(m.Tx(), g.door, false)
	}
}

// restrictOpenDoor is minecraft:behavior.restrict_open_door (BDS
// RestrictOpenDoorGoal): at night, indoors, the mob may not open or pass
// doors.
type restrictOpenDoor struct {
	mob.GoalBase
	open, pass bool
}

func newRestrictOpenDoor(spec mob.GoalSpec) (mob.Goal, error) {
	return &restrictOpenDoor{GoalBase: mob.GoalBase{Control: spec.FlagsOr(0)}}, nil
}

func (g *restrictOpenDoor) CanUse(m *mob.Mob) bool {
	return hasNav(m) && !isDay(m.Tx()) && !canSeeSky(m.Tx(), blockPos(m.Position()))
}

func (g *restrictOpenDoor) Start(m *mob.Mob) {
	if n, ok := nav.NavigationOf(m); ok {
		g.open, g.pass = n.Options.CanOpenDoors, n.Options.CanPassDoors
		n.Options.CanOpenDoors, n.Options.CanPassDoors = false, false
	}
}

func (g *restrictOpenDoor) Stop(m *mob.Mob) {
	if n, ok := nav.NavigationOf(m); ok {
		n.Options.CanOpenDoors, n.Options.CanPassDoors = true, true
	}
}

// exposed reports whether pos is in the rain under the open sky.
func exposed(m *mob.Mob, pos cube.Pos) bool {
	return raining(m.Tx(), pos) && canSeeSky(m.Tx(), pos)
}

// moveIndoors is minecraft:behavior.move_indoors (BDS MoveIndoorsGoal):
// villagers in the rain walk to their bed (or the village's closest one),
// then stop once they are under a roof.
type moveIndoors struct {
	mob.GoalBase
	speed   float64
	timeout int
	wait    int
	bed     cube.Pos
	failed  bool
}

func newMoveIndoors(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Speed   *float64 `json:"speed_multiplier"`
		Timeout *float64 `json:"timeout_cooldown"`
	}{}
	if err := decode(spec, &j); err != nil {
		return nil, err
	}
	g := &moveIndoors{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)}, speed: 0.8, timeout: 160}
	if j.Speed != nil {
		g.speed = *j.Speed
	}
	if j.Timeout != nil {
		g.timeout = int(*j.Timeout * 20)
	}
	return g, nil
}

func (g *moveIndoors) CanUse(m *mob.Mob) bool {
	if g.failed {
		if g.wait++; g.wait < g.timeout {
			return false
		}
		g.failed, g.wait = false, 0
	}
	if !hasNav(m) || m.Meta().InLove || m.Tx().World().Dimension().WaterEvaporates() {
		return false
	}
	pos := blockPos(m.Position())
	if !exposed(m, pos) {
		return false
	}
	v, ok := vc.VillageOf(m)
	if !ok {
		return false
	}
	p, ok := v.OwnedPOI(m.H(), vc.POIBed)
	if !ok {
		if p, ok = v.ClosestPOI(vc.POIBed, m.Position()); !ok {
			return false
		}
	}
	g.bed = p.Pos
	return true
}

func (g *moveIndoors) path(m *mob.Mob) {
	pos := m.Position()
	if exposed(m, blockPos(pos)) {
		target := g.bed.Vec3Centre()
		if distSq(target, pos) > 256 {
			if t, ok := posTowards(m, 14, 3, target); ok {
				target = centre(t)
			}
		}
		if !m.Navigation().MoveTo(m, target, g.speed) {
			g.failed = true
		}
	}
}

func (g *moveIndoors) Start(m *mob.Mob) { g.path(m) }

func (g *moveIndoors) CanContinueToUse(m *mob.Mob) bool {
	return !g.failed && (exposed(m, blockPos(m.Position())) || !m.Navigation().Done())
}

func (g *moveIndoors) Tick(m *mob.Mob) {
	pos := blockPos(m.Position())
	if !exposed(m, pos) {
		m.Navigation().Stop(m)
		return
	}
	if m.Navigation().Done() {
		g.path(m)
	}
}

func (g *moveIndoors) Stop(m *mob.Mob) { m.Navigation().Stop(m) }

// moveOutdoors is minecraft:behavior.move_outdoors (BDS MoveOutdoorsGoal):
// walk to a random spot within search_range that sees the sky.
type moveOutdoors struct {
	mob.GoalBase
	speed            float64
	rng, height, cnt int
	radius           float64
	timeout          int
	target           mgl64.Vec3
	next             int64
	wait             int
}

func newMoveOutdoors(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Speed   *float64 `json:"speed_multiplier"`
		Range   *int     `json:"search_range"`
		Height  *int     `json:"search_height"`
		Count   *int     `json:"search_count"`
		Radius  *float64 `json:"goal_radius"`
		Timeout *float64 `json:"timeout_cooldown"`
	}{}
	if err := decode(spec, &j); err != nil {
		return nil, err
	}
	g := &moveOutdoors{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)}, speed: 0.5, rng: 15, height: 5, cnt: 10, radius: 2, timeout: 160}
	if j.Speed != nil {
		g.speed = *j.Speed
	}
	if j.Range != nil {
		g.rng = *j.Range
	}
	if j.Height != nil {
		g.height = *j.Height
	}
	if j.Count != nil {
		g.cnt = *j.Count
	}
	if j.Radius != nil {
		g.radius = *j.Radius
	}
	if j.Timeout != nil {
		g.timeout = int(*j.Timeout * 20)
	}
	return g, nil
}

func (g *moveOutdoors) valid(m *mob.Mob, p cube.Pos) bool {
	return canSeeSky(m.Tx(), p.Side(cube.FaceUp))
}

func (g *moveOutdoors) find(m *mob.Mob) bool {
	for range max(1, g.cnt) {
		p, ok := randomPos(m, g.rng, g.height)
		if ok && g.valid(m, blockPos(p)) {
			g.target = centre(p)
			return true
		}
	}
	return false
}

func (g *moveOutdoors) CanUse(m *mob.Mob) bool {
	if g.wait > 0 {
		g.wait--
		return false
	}
	if !hasNav(m) || canSeeSky(m.Tx(), blockPos(m.Position())) {
		return false
	}
	if !g.find(m) {
		g.wait = g.timeout
		return false
	}
	return true
}

func (g *moveOutdoors) Start(m *mob.Mob) {
	m.Navigation().MoveTo(m, g.target, g.speed)
	g.next = m.Ticks() + 100 + int64(m.Rand().IntN(100))
}

func (g *moveOutdoors) CanContinueToUse(m *mob.Mob) bool {
	return !m.Navigation().Done() || distSq(m.Position(), g.target) > g.radius*g.radius
}

func (g *moveOutdoors) Tick(m *mob.Mob) {
	if m.Navigation().Done() && m.Ticks() >= g.next {
		if !g.find(m) {
			return
		}
		m.Navigation().MoveTo(m, g.target, g.speed)
		g.next = m.Ticks() + 100 + int64(m.Rand().IntN(100))
	}
}

func (g *moveOutdoors) Stop(m *mob.Mob) { m.Navigation().Stop(m) }

// exploreOutskirts is minecraft:behavior.explore_outskirts (BDS
// ExploreOutskirtsGoal): walk to a point explore_dist outside the village
// bounds, wait there, wander by next_xz/next_y.
type exploreOutskirts struct {
	mob.GoalBase
	nextXZ, nextY        int
	minWait, maxWait     int
	maxTravel            int
	speed, dist          float64
	minPerimeter         float64
	minDistSq            float64
	ratio                float64
	boundary             mgl64.Vec3
	target               mgl64.Vec3
	traveling, failed    bool
	travelT, waitT, wait int
}

func newExploreOutskirts(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		NextXZ    *int        `json:"next_xz"`
		NextY     *int        `json:"next_y"`
		MinWait   *float64    `json:"min_wait_time"`
		MaxWait   *float64    `json:"max_wait_time"`
		MaxTravel *float64    `json:"max_travel_time"`
		Speed     *float64    `json:"speed_multiplier"`
		Dist      *float64    `json:"explore_dist"`
		MinPer    *float64    `json:"min_perimeter"`
		MinDist   *float64    `json:"min_dist_from_target"`
		Ratio     *float64    `json:"timer_ratio"`
		Boundary  *[3]float64 `json:"dist_from_boundary"`
	}{}
	if err := decode(spec, &j); err != nil {
		return nil, err
	}
	g := &exploreOutskirts{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, nextXZ: 5, nextY: 3, minWait: 60, maxWait: 0,
		maxTravel: 1200, speed: 1, dist: 5, minPerimeter: 1, minDistSq: 2.25, ratio: 2, boundary: mgl64.Vec3{5, 0, 5}}
	set := func(p *float64, dst *int, mul float64) {
		if p != nil {
			*dst = int(*p * mul)
		}
	}
	if j.NextXZ != nil {
		g.nextXZ = *j.NextXZ
	}
	if j.NextY != nil {
		g.nextY = *j.NextY
	}
	set(j.MinWait, &g.minWait, 20)
	set(j.MaxWait, &g.maxWait, 20)
	set(j.MaxTravel, &g.maxTravel, 20)
	if j.Speed != nil {
		g.speed = *j.Speed
	}
	if j.Dist != nil {
		g.dist = *j.Dist
	}
	if j.MinPer != nil {
		g.minPerimeter = *j.MinPer
	}
	if j.MinDist != nil {
		g.minDistSq = *j.MinDist
	}
	if j.Ratio != nil && *j.Ratio > 0 {
		g.ratio = *j.Ratio
	}
	if j.Boundary != nil {
		g.boundary = mgl64.Vec3(*j.Boundary)
	}
	return g, nil
}

func (g *exploreOutskirts) CanUse(m *mob.Mob) bool {
	v, ok := vc.VillageOf(m)
	if !hasNav(m) || !ok {
		return false
	}
	b := v.Bounds()
	mn, mx := b.Min().Add(g.boundary), b.Max().Sub(g.boundary)
	pos := m.Position()
	if pos[0] > mn[0] && pos[0] < mx[0] && pos[2] > mn[2] && pos[2] < mx[2] {
		return false
	}
	w, d := b.Max()[0]-b.Min()[0], b.Max()[2]-b.Min()[2]
	per := 2*w + 2*d
	if per < g.minPerimeter {
		return false
	}
	t := m.Rand().Float64() * per
	bm, bx := b.Min(), b.Max()
	var p mgl64.Vec3
	switch {
	case t < w:
		p = mgl64.Vec3{bm[0] + t, pos[1], bm[2]}
	case t < w+d:
		p = mgl64.Vec3{bx[0], pos[1], bm[2] + t - w}
	case t < 2*w+d:
		p = mgl64.Vec3{bx[0] - (t - w - d), pos[1], bx[2]}
	default:
		p = mgl64.Vec3{bm[0], pos[1], bx[2] - (t - 2*w - d)}
	}
	c := v.Center()
	dir := mgl64.Vec3{p[0] - c[0], 0, p[2] - c[2]}
	if dir.Len() > 0 {
		p = p.Add(dir.Normalize().Mul(g.dist))
	}
	g.target = topSolid(m.Tx(), p[0], p[1], p[2])
	return true
}

func (g *exploreOutskirts) Start(m *mob.Mob) {
	g.traveling, g.failed, g.travelT, g.waitT = true, false, 0, 0
	if !m.Navigation().MoveTo(m, g.target, g.speed) {
		g.failed = true
	}
}

func (g *exploreOutskirts) Tick(m *mob.Mob) {
	if g.traveling {
		if m.Navigation().Done() {
			if distSq(m.Position(), g.target) >= g.minDistSq {
				if g.travelT < g.maxTravel && !m.Navigation().MoveTo(m, g.target, g.speed) {
					g.failed = true
					return
				}
			} else {
				g.traveling = false
				g.wait = randInt(m, g.minWait, g.maxWait)
			}
		}
		g.travelT++
		return
	}
	if g.waitT++; g.waitT > g.wait && m.Navigation().Done() {
		if p, ok := randomPos(m, g.nextXZ, g.nextY); ok {
			m.Navigation().MoveTo(m, centre(p), g.speed)
		}
		g.wait = randInt(m, int(float64(g.minWait)/g.ratio), int(float64(g.maxWait)/g.ratio))
	}
}

func randInt(m *mob.Mob, lo, hi int) int {
	if hi <= lo {
		return lo
	}
	return lo + m.Rand().IntN(hi-lo)
}

func (g *exploreOutskirts) CanContinueToUse(*mob.Mob) bool {
	if g.failed {
		return false
	}
	if g.traveling {
		return g.travelT < g.maxTravel
	}
	return g.waitT < g.maxWait
}

func (g *exploreOutskirts) Stop(m *mob.Mob) {
	m.Navigation().Stop(m)
	g.traveling, g.travelT, g.waitT = false, 0, 0
}

// goHome is minecraft:behavior.go_home (BDS GoHomeGoal): with chance
// 1/interval walk to the home position (if within follow range); fire
// on_home when reached, on_failed when the path cannot get there.
type goHome struct {
	mob.GoalBase
	speed        float64
	interval     int
	radiusSq     float64
	repathRadius float64
	onHome       []ccore.Trigger
	onFailed     []ccore.Trigger
	lastEnd      cube.Pos
	hasEnd       bool
}

func newGoHome(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Speed    *float64    `json:"speed_multiplier"`
		Interval *int        `json:"interval"`
		Radius   *float64    `json:"goal_radius"`
		Repath   *float64    `json:"calculate_new_path_radius"`
		OnHome   triggerList `json:"on_home"`
		OnFailed triggerList `json:"on_failed"`
	}{}
	if err := decode(spec, &j); err != nil {
		return nil, err
	}
	g := &goHome{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, speed: 1, interval: 120, radiusSq: 0.25, repathRadius: 2,
		onHome: j.OnHome, onFailed: j.OnFailed}
	if j.Speed != nil {
		g.speed = *j.Speed
	}
	if j.Interval != nil {
		g.interval = *j.Interval
	}
	if j.Radius != nil {
		g.radiusSq = *j.Radius * *j.Radius
	}
	if j.Repath != nil {
		g.repathRadius = *j.Repath
	}
	return g, nil
}

func (g *goHome) fire(m *mob.Mob, ts []ccore.Trigger) {
	home, _ := m.Home()
	for _, t := range ts {
		ctx := m.Ctx()
		ctx.Block, ctx.BlockPos, ctx.HasBlock = m.Tx().Block(home), home, true
		t.Fire(m, ctx)
	}
}

func (g *goHome) CanUse(m *mob.Mob) bool {
	home, ok := m.Home()
	if !hasNav(m) || !ok {
		return false
	}
	if g.interval > 0 && m.Rand().IntN(g.interval) != 0 {
		return false
	}
	fr := m.Attr(mob.AttrFollowRange)
	if distSq(m.Position(), home.Vec3Centre()) <= fr*fr {
		return true
	}
	g.fire(m, g.onFailed)
	return false
}

func (g *goHome) reached(m *mob.Mob) bool {
	home, ok := m.Home()
	return ok && distSq(m.Position(), home.Vec3Centre()) <= g.radiusSq+1
}

func (g *goHome) CanContinueToUse(m *mob.Mob) bool {
	_, ok := m.Home()
	return ok && !g.reached(m)
}

func (g *goHome) Start(m *mob.Mob) {
	home, _ := m.Home()
	m.Navigation().MoveTo(m, home.Vec3Middle(), g.speed)
	g.hasEnd = false
}

func (g *goHome) Tick(m *mob.Mob) {
	home, ok := m.Home()
	if !ok {
		return
	}
	n := m.Navigation()
	closeToDone := n.Path() != nil && n.Path().Len() > 0 && distSq(m.Position(), n.Path().End().Vec3Middle()) < g.repathRadius*g.repathRadius
	if !n.Done() && !closeToDone {
		return
	}
	p := n.Path()
	if p == nil || p.Len() == 0 {
		if !n.MoveTo(m, home.Vec3Middle(), g.speed) {
			g.fire(m, g.onFailed)
		}
		return
	}
	if end := p.End(); end != home {
		if g.hasEnd && end == g.lastEnd {
			g.fire(m, g.onFailed)
			return
		}
		g.lastEnd, g.hasEnd = end, true
		n.MoveTo(m, home.Vec3Middle(), g.speed)
	}
}

func (g *goHome) Stop(m *mob.Mob) {
	m.Navigation().Stop(m)
	if g.reached(m) {
		g.fire(m, g.onHome)
	}
}

// triggerList is one trigger or a list.
type triggerList []ccore.Trigger

func (l *triggerList) UnmarshalJSON(b []byte) error {
	var arr []ccore.Trigger
	if err := jsonUnmarshal(b, &arr); err == nil {
		*l = arr
		return nil
	}
	var one ccore.Trigger
	if err := jsonUnmarshal(b, &one); err != nil {
		return err
	}
	*l = []ccore.Trigger{one}
	return nil
}

var _ = math.Pi
