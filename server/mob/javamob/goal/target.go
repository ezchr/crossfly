//go:build crossfly_full

package goal

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Target is TargetGoal: the shared part of the target goals. It keeps the target while it is valid, in follow
// range and (MustSee) seen within UnseenMemoryTicks.
type Target struct {
	javamob.GoalBase
	MustSee, MustReach bool
	UnseenMemoryTicks  int

	reachCache, reachCacheTime, unseenTicks int
	targetMob                               *world.EntityHandle
}

func newTarget(mustSee, mustReach bool) Target {
	return Target{GoalBase: javamob.GoalBase{Control: javamob.FlagTarget}, MustSee: mustSee, MustReach: mustReach, UnseenMemoryTicks: 60}
}

func (g *Target) CanContinueToUse(m *javamob.Mob) bool {
	t, ok := m.Target()
	if !ok && g.targetMob != nil {
		t, ok = g.targetMob.Entity(m.Tx())
	}
	if !ok || !alive(t) {
		return false
	}
	within := m.FollowRange()
	if t.Position().Sub(m.Position()).LenSqr() > within*within {
		return false
	}
	if g.MustSee {
		if m.Sensing().HasLineOfSight(m, t) {
			g.unseenTicks = 0
		} else if g.unseenTicks++; g.unseenTicks > javamob.ReducedTickDelay(g.UnseenMemoryTicks) {
			return false
		}
	}
	m.SetTarget(t)
	_, ok = m.Target()
	return ok
}

func (g *Target) Start(*javamob.Mob) { g.reachCache, g.reachCacheTime, g.unseenTicks = 0, 0, 0 }

func (g *Target) Stop(m *javamob.Mob) {
	m.SetTarget(nil)
	g.targetMob = nil
}

// canAttack is TargetGoal.canAttack.
func (g *Target) canAttack(m *javamob.Mob, t world.Entity, c javamob.TargetingConditions) bool {
	if t == nil || !c.Test(m, t) || !m.WithinHome(cube.PosFromVec3(t.Position())) {
		return false
	}
	if g.MustReach {
		if g.reachCacheTime--; g.reachCacheTime <= 0 {
			g.reachCache = 0
		}
		if g.reachCache == 0 {
			g.reachCache = 2
			if g.canReach(m, t) {
				g.reachCache = 1
			}
		}
		if g.reachCache == 2 {
			return false
		}
	}
	return true
}

func (g *Target) canReach(m *javamob.Mob, t world.Entity) bool {
	g.reachCacheTime = javamob.ReducedTickDelay(10 + m.Rand().IntN(5))
	p := m.Navigation().CreatePathToEntity(m, t, 0)
	if p == nil || p.EndNode() == nil {
		return false
	}
	end := p.EndNode()
	tp := cube.PosFromVec3(t.Position())
	xx, zz := end.X-tp[0], end.Z-tp[2]
	return float64(xx*xx+zz*zz) <= 2.25
}

// NearestAttackableTarget is NearestAttackableTargetGoal: once in RandomInterval ticks on average, target the
// nearest matching entity in follow range (players by default).
type NearestAttackableTarget struct {
	Target
	// Match picks the target type; nil means players.
	Match          func(world.Entity) bool
	RandomInterval int
	// Selector is the extra TargetingConditions selector, or nil.
	Selector func(e world.Entity, tx *world.Tx) bool
	target   *world.EntityHandle
}

// NewNearestAttackableTarget returns NearestAttackableTargetGoal(mob, type, 10, mustSee, mustReach, nil); match nil
// targets players.
func NewNearestAttackableTarget(match func(world.Entity) bool, mustSee, mustReach bool) *NearestAttackableTarget {
	return &NearestAttackableTarget{Target: newTarget(mustSee, mustReach), Match: match, RandomInterval: javamob.ReducedTickDelay(10)}
}

func (g *NearestAttackableTarget) conditions(m *javamob.Mob) javamob.TargetingConditions {
	c := javamob.ForCombat(m.FollowRange())
	c.Selector = g.Selector
	return c
}

func (g *NearestAttackableTarget) CanUse(m *javamob.Mob) bool {
	if g.RandomInterval > 0 && m.Rand().IntN(g.RandomInterval) != 0 {
		return false
	}
	g.FindTarget(m)
	return g.target != nil
}

// FindTarget is findTarget.
func (g *NearestAttackableTarget) FindTarget(m *javamob.Mob) {
	g.target = nil
	c := g.conditions(m)
	if g.Match == nil {
		if p, ok := m.NearestPlayer(c); ok {
			g.target = p.H()
		}
		return
	}
	d := m.FollowRange()
	box := m.BBox().GrowVec3(mgl64.Vec3{d, d, d})
	if e, ok := m.NearestEntity(box, g.Match, c); ok {
		g.target = e.H()
	}
}

func (g *NearestAttackableTarget) Start(m *javamob.Mob) {
	if g.target != nil {
		if e, ok := g.target.Entity(m.Tx()); ok {
			m.SetTarget(e)
		}
	}
	g.Target.Start(m)
}

// IsPlayer matches players (Player.class).
func IsPlayer(e world.Entity) bool { _, ok := e.(*player.Player); return ok }

// IsType returns a match for entities of the identifiers ("minecraft:iron_golem").
func IsType(ids ...string) func(world.Entity) bool {
	return func(e world.Entity) bool {
		id := e.H().Type().EncodeEntity()
		for _, want := range ids {
			if id == want {
				return true
			}
		}
		return false
	}
}

// HurtByTarget is HurtByTargetGoal: target whoever hurt the mob last, and with AlertOthers make the mobs of the same
// type around without a target target it too.
type HurtByTarget struct {
	Target
	// IgnoreDamageFrom are the identifiers whose attacks are not answered.
	IgnoreDamageFrom []string
	// AlertOthers is setAlertOthers; AlertIgnore the identifiers not alerted.
	AlertOthers bool
	AlertIgnore []string
	// SameType reports whether e is of the mob's own type for alerting (its class); nil compares identifiers.
	SameType func(m *javamob.Mob, e world.Entity) bool
	// AlertOther, if set, replaces alertOther (setTarget).
	AlertOther func(m, other *javamob.Mob, target world.Entity)
	timestamp  int
}

// NewHurtByTarget returns HurtByTargetGoal(mob, ignoreDamageFrom...).
func NewHurtByTarget(ignore ...string) *HurtByTarget {
	return &HurtByTarget{Target: newTarget(true, false), IgnoreDamageFrom: ignore}
}

// SetAlertOthers is setAlertOthers(except...).
func (g *HurtByTarget) SetAlertOthers(except ...string) *HurtByTarget {
	g.AlertOthers, g.AlertIgnore = true, except
	return g
}

var hurtByTargeting = javamob.TargetingConditions{Combat: true, IgnoreLineOfSight: true, IgnoreInvisibility: true}

func (g *HurtByTarget) CanUse(m *javamob.Mob) bool {
	ts := m.LastHurtByMobTimestamp()
	by, ok := m.LastHurtByMob()
	if ts == g.timestamp || !ok {
		return false
	}
	id := by.H().Type().EncodeEntity()
	for _, ig := range g.IgnoreDamageFrom {
		if id == ig {
			return false
		}
	}
	return g.canAttack(m, by, hurtByTargeting)
}

func (g *HurtByTarget) Start(m *javamob.Mob) {
	by, _ := m.LastHurtByMob()
	m.SetTarget(by)
	if t, ok := m.Target(); ok {
		g.targetMob = t.H()
	}
	g.timestamp = m.LastHurtByMobTimestamp()
	g.UnseenMemoryTicks = 300
	if g.AlertOthers {
		g.alert(m, by)
	}
	g.Target.Start(m)
}

func (g *HurtByTarget) alert(m *javamob.Mob, by world.Entity) {
	if by == nil {
		return
	}
	d := m.FollowRange()
	p := m.Position()
	// AABB.unitCubeFromLowerCorner(position).inflate(d, 10, d).
	for e := range m.Tx().EntitiesWithin(cube.Box(p[0]-d, p[1]-10, p[2]-d, p[0]+1+d, p[1]+11, p[2]+1+d)) {
		o, ok := javamob.Of(e)
		if !ok || o.H() == m.H() {
			continue
		}
		if g.SameType != nil {
			if !g.SameType(m, e) {
				continue
			}
		} else if e.H().Type() != m.H().Type() {
			continue
		}
		if _, has := o.Mob.Target(); has {
			continue
		}
		ignored := false
		id := e.H().Type().EncodeEntity()
		for _, ig := range g.AlertIgnore {
			if id == ig {
				ignored = true
			}
		}
		if ignored {
			continue
		}
		if g.AlertOther != nil {
			g.AlertOther(m, o, by)
		} else {
			o.SetTarget(by)
		}
	}
}
