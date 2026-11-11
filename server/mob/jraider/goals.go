//go:build crossfly_full

package jraider

import (
	"math"
	"slices"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/path"
	"github.com/df-mc/dragonfly/server/mob/jpoi"
	"github.com/df-mc/dragonfly/server/mob/jraid"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// The goals of Raider and PatrollingMonster. They read the raider through *base, whose Raider a raid may replace
// while the mob is created.

// bottomCentre is Vec3.atBottomCenterOf.
func bottomCentre(p cube.Pos) mgl64.Vec3 {
	return mgl64.Vec3{float64(p[0]) + 0.5, float64(p[1]), float64(p[2]) + 0.5}
}

func closerToCenterThan(p cube.Pos, v mgl64.Vec3, d float64) bool {
	return p.Vec3Centre().Sub(v).LenSqr() < d*d
}

// pathfindToRaid is PathfindToRaidGoal.
type pathfindToRaid struct {
	javamob.GoalBase
	b           *base
	recruitTick int
}

func newPathfindToRaid(b *base) *pathfindToRaid {
	return &pathfindToRaid{GoalBase: javamob.GoalBase{Control: javamob.FlagMove}, b: b}
}

func (g *pathfindToRaid) active(m *javamob.Mob) bool {
	st := &g.b.State
	return st.HasActiveRaid() && !st.Raid().IsOver() && !jraid.IsVillage(levelOf(m.Tx()), cube.PosFromVec3(m.Position()))
}

func (g *pathfindToRaid) CanUse(m *javamob.Mob) bool {
	if _, ok := m.Target(); ok {
		return false
	}
	return g.active(m)
}

func (g *pathfindToRaid) CanContinueToUse(m *javamob.Mob) bool { return g.active(m) }

func (g *pathfindToRaid) Tick(m *javamob.Mob) {
	st := &g.b.State
	if !st.HasActiveRaid() {
		return
	}
	raid := st.Raid()
	if m.TickCount() > g.recruitTick {
		g.recruitTick = m.TickCount() + 20
		recruitNearby(m, raid)
	}
	if m.Navigation().Done() {
		if p, ok := m.LandRandomPosTowards(15, 4, bottomCentre(raid.Center())); ok {
			m.Navigation().MoveTo(m, p, 1, 1)
		}
	}
}

// recruitNearby is PathfindToRaidGoal.recruitNearby: raiders within 16 blocks without an active raid join it.
func recruitNearby(m *javamob.Mob, raid *jraid.Raid) {
	if !raid.IsActive() {
		return
	}
	l := levelOf(m.Tx())
	for e := range m.Tx().EntitiesWithin(m.BBox().Grow(16)) {
		r, ok := raiderOf(e)
		if !ok || r.HasActiveRaid() || !jraid.CanJoinRaid(r) {
			continue
		}
		raid.JoinRaid(l, raid.GroupsSpawned(), r, nil, true)
	}
}

// celebration is Raider.RaiderCelebration.
type celebration struct {
	javamob.GoalBase
	b     *base
	sound string
}

func newCelebration(b *base, sound string) *celebration {
	return &celebration{GoalBase: javamob.GoalBase{Control: javamob.FlagMove}, b: b, sound: sound}
}

func (g *celebration) CanUse(m *javamob.Mob) bool {
	if _, ok := m.Target(); ok || m.Dead() {
		return false
	}
	raid := g.b.State.Raid()
	return raid != nil && raid.IsLoss()
}

func (g *celebration) Start(m *javamob.Mob) {
	g.b.Celebrate(true)
	g.b.apply(m)
}

func (g *celebration) Stop(m *javamob.Mob) {
	g.b.Celebrate(false)
	g.b.apply(m)
}

func (g *celebration) Tick(m *javamob.Mob) {
	if m.Rand().IntN(javamob.AdjustedTickDelay(g, 100)) == 0 {
		m.PlaySound(g.sound)
	}
	if !m.Riding() && m.Rand().IntN(javamob.AdjustedTickDelay(g, 50)) == 0 {
		m.JumpControl().Jump()
	}
}

// holdGroundAttack is Raider.HoldGroundAttackGoal: a patrolling raider that sees a target calls its patrol within
// 8 blocks to it and holds its ground until the target is within hostileRadius.
type holdGroundAttack struct {
	javamob.GoalBase
	b                *base
	hostileRadiusSqr float64
}

func newHoldGroundAttack(b *base, radius float64) *holdGroundAttack {
	return &holdGroundAttack{GoalBase: javamob.GoalBase{Control: javamob.FlagMove | javamob.FlagLook}, b: b, hostileRadiusSqr: radius * radius}
}

func (g *holdGroundAttack) CanUse(m *javamob.Mob) bool {
	if g.b.State.Raid() != nil || !g.b.Patrolling || m.Aggressive() {
		return false
	}
	if _, ok := m.Target(); !ok {
		return false
	}
	if by, ok := m.LastHurtByMob(); ok && isPlayer(by) {
		return false
	}
	return true
}

// nearbyRaiders is getNearbyEntities(Raider.class, forNonCombat().range(8).ignoreLineOfSight(), mob, box.inflate(8)).
func nearbyRaiders(m *javamob.Mob) []*javamob.Mob {
	var out []*javamob.Mob
	for e := range m.Tx().EntitiesWithin(m.BBox().Grow(8)) {
		if e.H() == m.H() {
			continue
		}
		if _, ok := raiderOf(e); !ok {
			continue
		}
		o, _ := javamob.Of(e)
		if o.Dead() || o.Position().Sub(m.Position()).Len() > 8 {
			continue
		}
		out = append(out, o)
	}
	return out
}

func (g *holdGroundAttack) Start(m *javamob.Mob) {
	m.Navigation().Stop()
	t, ok := m.Target()
	for _, o := range nearbyRaiders(m) {
		if ok {
			o.SetTarget(t)
		} else {
			o.SetTarget(nil)
		}
	}
}

func (g *holdGroundAttack) Stop(m *javamob.Mob) {
	t, ok := m.Target()
	if !ok {
		return
	}
	for _, o := range nearbyRaiders(m) {
		o.SetTarget(t)
		o.SetAggressive(true)
	}
	m.SetAggressive(true)
}

func (g *holdGroundAttack) RequiresUpdateEveryTick() bool { return true }

func (g *holdGroundAttack) Tick(m *javamob.Mob) {
	t, ok := m.Target()
	if !ok {
		return
	}
	if t.Position().Sub(m.Position()).LenSqr() > g.hostileRadiusSqr {
		m.LookControl().SetLookAtEntity(t, 30, 30)
		if m.Rand().IntN(50) == 0 {
			m.PlaySound("ambient")
		}
		return
	}
	m.SetAggressive(true)
}

// longDistancePatrol is PatrollingMonster.LongDistancePatrolGoal.
type longDistancePatrol struct {
	javamob.GoalBase
	b                  *base
	speed, leaderSpeed float64
	cooldownUntil      int64
}

func newLongDistancePatrol(b *base, speed, leaderSpeed float64) *longDistancePatrol {
	return &longDistancePatrol{GoalBase: javamob.GoalBase{Control: javamob.FlagMove}, b: b, speed: speed, leaderSpeed: leaderSpeed, cooldownUntil: -1}
}

func (g *longDistancePatrol) CanUse(m *javamob.Mob) bool {
	if m.Tx().CurrentTick() < g.cooldownUntil {
		return false
	}
	_, hasTarget := m.Target()
	return g.b.Patrolling && !hasTarget && g.b.PatrolTarget != nil
}

func (g *longDistancePatrol) Tick(m *javamob.Mob) {
	st := &g.b.State
	nav := m.Navigation()
	if !nav.Done() {
		return
	}
	companions := patrolCompanions(m)
	switch {
	case st.Patrolling && len(companions) == 0:
		st.Patrolling = false
	case st.PatrolLeader && closerToCenterThan(*st.PatrolTarget, m.Position(), 10):
		st.FindPatrolTarget(m.Rand(), cube.PosFromVec3(m.Position()))
	default:
		target := bottomCentre(*st.PatrolTarget)
		self := m.Position()
		d := self.Sub(target)
		// distance.yRot(90): rotation of the vector about the y axis by 90 degrees.
		c, s := math.Cos(float64(float32(90*math.Pi/180))), math.Sin(float64(float32(90*math.Pi/180)))
		rot := mgl64.Vec3{d[0]*c + d[2]*s, d[1], d[2]*c - d[0]*s}
		target = rot.Mul(0.4).Add(target)
		move := target.Sub(self)
		if move.Len() > 1e-4 {
			move = move.Normalize()
		} else {
			move = mgl64.Vec3{}
		}
		move = move.Mul(10).Add(self)
		pt := cube.PosFromVec3(move)
		pt[1] = motionBlockingNoLeaves(m.Tx(), pt[0], pt[2])
		speed := g.speed
		if st.PatrolLeader {
			speed = g.leaderSpeed
		}
		if !nav.MoveTo(m, pt.Vec3(), 1, speed) {
			g.moveRandomly(m)
			g.cooldownUntil = m.Tx().CurrentTick() + 200
		} else if st.PatrolLeader {
			for _, c := range companions {
				c.SetPatrolTarget(pt)
			}
		}
	}
}

func (g *longDistancePatrol) moveRandomly(m *javamob.Mob) {
	r := m.Rand()
	p := cube.PosFromVec3(m.Position()).Add(cube.Pos{-8 + r.IntN(16), 0, -8 + r.IntN(16)})
	p[1] = motionBlockingNoLeaves(m.Tx(), p[0], p[2])
	m.Navigation().MoveTo(m, p.Vec3(), 1, g.speed)
}

// patrolCompanions is LongDistancePatrolGoal.findPatrolCompanions: patrolling monsters within 16 blocks that can
// join a patrol.
func patrolCompanions(m *javamob.Mob) []*Raider {
	var out []*Raider
	for e := range m.Tx().EntitiesWithin(m.BBox().Grow(16)) {
		if e.H() == m.H() {
			continue
		}
		if r, ok := raiderOf(e); ok && r.CanJoinPatrol() {
			out = append(out, r)
		}
	}
	return out
}

// moveThroughVillage is Raider.RaiderMoveThroughVillageGoal.
type moveThroughVillage struct {
	javamob.GoalBase
	b        *base
	speed    float64
	distance int
	poi      cube.Pos
	visited  []cube.Pos
	stuck    bool
}

func newMoveThroughVillage(b *base, speed float64, distance int) *moveThroughVillage {
	return &moveThroughVillage{GoalBase: javamob.GoalBase{Control: javamob.FlagMove}, b: b, speed: speed, distance: distance}
}

func (g *moveThroughVillage) CanUse(m *javamob.Mob) bool {
	if len(g.visited) > 2 {
		g.visited = g.visited[1:]
	}
	st := &g.b.State
	if !st.HasActiveRaid() || st.Raid().IsOver() {
		return false
	}
	tx := m.Tx()
	p, ok := jpoi.For(tx).Random(tx, jpoi.Is(jpoi.Home), func(p cube.Pos) bool { return !slices.Contains(g.visited, p) },
		jpoi.AnyOccupancy, cube.PosFromVec3(m.Position()), 48, m.Rand().IntN)
	if !ok {
		return false
	}
	g.poi = p
	_, hasTarget := m.Target()
	return !hasTarget
}

func (g *moveThroughVillage) CanContinueToUse(m *javamob.Mob) bool {
	if m.Navigation().Done() {
		return false
	}
	_, hasTarget := m.Target()
	return !hasTarget && !closerToCenterThan(g.poi, m.Position(), m.Width()+float64(g.distance)) && !g.stuck
}

func (g *moveThroughVillage) Stop(m *javamob.Mob) {
	if closerToCenterThan(g.poi, m.Position(), float64(g.distance)) {
		g.visited = append(g.visited, g.poi)
	}
}

func (g *moveThroughVillage) Start(m *javamob.Mob) {
	m.SetNoActionTime(0)
	m.Navigation().MoveTo(m, g.poi.Vec3(), 1, g.speed)
	g.stuck = false
}

func (g *moveThroughVillage) Tick(m *javamob.Mob) {
	if !m.Navigation().Done() {
		return
	}
	target := bottomCentre(g.poi)
	next, ok := m.LandRandomPosTowards(16, 7, target)
	if !ok {
		next, ok = m.LandRandomPosTowards(8, 7, target)
	}
	if !ok {
		g.stuck = true
		return
	}
	m.Navigation().MoveTo(m, next, 1, g.speed)
}

// obtainBanner is Raider.ObtainRaidLeaderBannerGoal: a raider of a wave without a captain walks to an ominous
// banner on the ground and puts it on.
type obtainBanner struct {
	javamob.GoalBase
	b           *base
	unreachable map[*world.EntityHandle]int64
	path        *path.Path
	banner      *world.EntityHandle
}

func newObtainBanner(b *base) *obtainBanner {
	return &obtainBanner{GoalBase: javamob.GoalBase{Control: javamob.FlagMove}, b: b, unreachable: map[*world.EntityHandle]int64{}}
}

func (g *obtainBanner) cannot(m *javamob.Mob) bool {
	return !jraid.CanPickUpBanner(levelOf(m.Tx()), g.b.Raider, wearsOminousBanner(g.b.Raider))
}

func (g *obtainBanner) CanUse(m *javamob.Mob) bool {
	if g.cannot(m) {
		return false
	}
	tx := m.Tx()
	now := tx.CurrentTick()
	fr := m.FollowRange()
	cache := map[*world.EntityHandle]int64{}
	for e := range tx.EntitiesWithin(m.BBox().GrowVec3(mgl64.Vec3{fr, 8, fr})) {
		it, ok := e.(*entity.Ent)
		if !ok {
			continue
		}
		ib, ok := it.Behaviour().(*entity.ItemBehaviour)
		if !ok || !isOminousBanner(ib.Item()) {
			continue
		}
		if until, ok := g.unreachable[e.H()]; ok && now < until {
			cache[e.H()] = until
			continue
		}
		if p := m.Navigation().CreatePathToEntity(m, e, 1); p != nil && p.CanReach() {
			g.path, g.banner = p, e.H()
			return true
		}
		cache[e.H()] = now + 600
	}
	g.unreachable = cache
	return false
}

func (g *obtainBanner) CanContinueToUse(m *javamob.Mob) bool {
	if g.banner == nil || g.path == nil {
		return false
	}
	if _, ok := g.banner.Entity(m.Tx()); !ok {
		return false
	}
	return !g.path.Done() && !g.cannot(m)
}

func (g *obtainBanner) Start(m *javamob.Mob) { m.Navigation().MoveAlong(m, g.path, 1.15) }
func (g *obtainBanner) Stop(*javamob.Mob)    { g.path, g.banner = nil, nil }

func (g *obtainBanner) Tick(m *javamob.Mob) {
	if g.banner == nil {
		return
	}
	e, ok := g.banner.Entity(m.Tx())
	if !ok || e.Position().Sub(m.Position()).LenSqr() >= 1.414*1.414 {
		return
	}
	pickUpBanner(m, g.b, e)
}

// pickUpBanner is the banner part of Raider.pickUpItem.
func pickUpBanner(m *javamob.Mob, b *base, e world.Entity) {
	levelOf(m.Tx())
	if !jraid.PickUpBanner(b.Raider) {
		return
	}
	// PickUpBanner put the banner on (SetCaptainBanner).
	b.apply(m)
	m.Tx().RemoveEntity(e)
	if c, ok := e.(interface{ Close() error }); ok {
		_ = c.Close()
	}
}
