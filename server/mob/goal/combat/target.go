//go:build crossfly_full

package combat

import (
	"encoding/json"
	"math"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/world"
)

// descriptor is one entry of "entity_types" (BDS MobDescriptor, parsed in
// EntityTypes.cpp).
type descriptor struct {
	filter mob.Filter
	// maxDist (default 16) and maxHeight (-1: any) bound the target.
	maxDist, maxHeight float64
	walk, sprint       float64
	maxFlee            float64
	// mustSeeSet: the descriptor has "must_see", which then overrides the
	// goal's must_see and must_see_forget_duration (default 3 s).
	mustSeeSet, mustSee bool
	forget              int
	reevaluate          bool
	// cooldown: ticks before this descriptor may pick a target again.
	cooldown int
}

type rawDescriptor struct {
	Filters               json.RawMessage `json:"filters"`
	MaxDist               *float64        `json:"max_dist"`
	MaxHeight             *float64        `json:"max_height"`
	WalkSpeedMultiplier   *float64        `json:"walk_speed_multiplier"`
	SprintSpeedMultiplier *float64        `json:"sprint_speed_multiplier"`
	MaxFlee               *float64        `json:"max_flee"`
	MustSee               *bool           `json:"must_see"`
	MustSeeForget         *float64        `json:"must_see_forget_duration"`
	Reevaluate            bool            `json:"reevaluate_description"`
	Cooldown              float64         `json:"cooldown"`
}

// parseDescriptors reads "entity_types": one object or an array. defDist is
// the max_dist when the entry has none.
func parseDescriptors(raw json.RawMessage, defDist float64) ([]descriptor, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var list []rawDescriptor
	if err := json.Unmarshal(raw, &list); err != nil {
		var one rawDescriptor
		if err2 := json.Unmarshal(raw, &one); err2 != nil {
			return nil, err
		}
		list = []rawDescriptor{one}
	}
	out := make([]descriptor, 0, len(list))
	for _, r := range list {
		f, err := mob.CompileFilter(r.Filters)
		if err != nil {
			return nil, err
		}
		d := descriptor{filter: f, maxDist: defDist, maxHeight: -1, walk: 1, sprint: 1, maxFlee: 10, forget: 60, reevaluate: r.Reevaluate, cooldown: sense.Ticks(r.Cooldown)}
		if r.MaxDist != nil {
			d.maxDist = *r.MaxDist
		}
		if r.MaxHeight != nil {
			d.maxHeight = *r.MaxHeight
		}
		if r.WalkSpeedMultiplier != nil {
			d.walk = *r.WalkSpeedMultiplier
		}
		if r.SprintSpeedMultiplier != nil {
			d.sprint = *r.SprintSpeedMultiplier
		}
		if r.MaxFlee != nil {
			d.maxFlee = *r.MaxFlee
		}
		if r.MustSee != nil {
			d.mustSeeSet, d.mustSee = true, *r.MustSee
			if r.MustSeeForget != nil {
				d.forget = sense.Ticks(*r.MustSeeForget)
			}
		}
		out = append(out, d)
	}
	return out, nil
}

// targetBase is BDS TargetGoal: the shared target checks of the target
// selector goals (nearest_attackable_target, hurt_by_target, ...).
type targetBase struct {
	mob.GoalBase
	key         string
	descriptors []descriptor
	// Definition values.
	mustSee     bool // +66
	forgetTicks int  // +80
	mustReach   bool // +65
	attackOwner bool // +64
	persistTime int  // +84 (ticks)

	// State.
	mustSeeActive    bool // +67
	forgetActive     int  // +68
	persistCounting  bool // +72
	persistTicks     int  // +76
	reachCache       int  // +88: 0 unknown, 1 reachable, 2 not
	reachDelay       int  // +92
	unseen           int  // +96
	reevaluateActive bool // +104
}

// IsTargetGoal ...
func (*targetBase) IsTargetGoal() bool { return true }

// canAttack is TargetGoal::_canAttack(Mob*, Actor*, bool, bool, desc**).
func (g *targetBase) canAttack(m *mob.Mob, t world.Entity, allowInvulnerable, mustSee bool, desc **descriptor) bool {
	if t == nil || !sense.Alive(t) || t.H() == m.H() || m.Dead() || !hasFamilies(t) {
		return false
	}
	owner := m.Owner()
	if !g.attackOwner && owner != nil && owner == t.H() {
		return false
	}
	if !g.attackOwner && owner != nil {
		if o, ok := t.(interface{ Owner() *world.EntityHandle }); ok && o.Owner() == owner {
			return false
		}
	}
	if sense.IsPlayer(t) && !allowInvulnerable {
		if sense.Invulnerable(t) || sense.DifficultyID(m.Tx()) == 0 {
			return false
		}
	}
	return g.matchesTargetTypes(m, t, mustSee, desc)
}

// canAttackAndReach is TargetGoal::_canAttack(Actor*, bool, desc**).
func (g *targetBase) canAttackAndReach(m *mob.Mob, t world.Entity, allowInvulnerable bool, desc **descriptor) bool {
	if !g.canAttack(m, t, allowInvulnerable, g.mustSee, desc) {
		return false
	}
	return !g.mustReach || g.canReachAfterDelay(m, t)
}

// matchesTargetTypes is TargetGoal::_matchesTargetTypes.
func (g *targetBase) matchesTargetTypes(m *mob.Mob, t world.Entity, mustSee bool, desc **descriptor) bool {
	cur, _ := m.Target()
	isCurrent := cur != nil && cur.H() == t.H()
	alreadyTarget := isCurrent && g.unseen < g.forgetActive
	defaults := true
	if len(g.descriptors) > 0 {
		ctx := m.Ctx()
		ctx.Other = t
		ctx.Target = cur
		if sense.IsPlayer(t) {
			ctx.Player = t
		}
		matched := false
		best := 0.0
		distSq := m.Pos().Sub(t.Position()).LenSqr()
		for i := range g.descriptors {
			d := &g.descriptors[i]
			if distSq >= d.maxDist*d.maxDist {
				continue
			}
			if d.mustSee && !alreadyTarget && !sense.CanSee(m, t) {
				continue
			}
			if !d.filter.Test(ctx) {
				continue
			}
			matched = true
			if (g.reevaluateActive && !d.reevaluate) || best < d.maxDist {
				g.reevaluateActive = d.reevaluate
				best = d.maxDist
				if d.mustSeeSet {
					g.mustSeeActive = d.mustSee
					g.forgetActive = d.forget
					defaults = false
				}
				if desc != nil {
					*desc = d
				}
			}
		}
		if !matched {
			if isCurrent {
				g.persistCounting = true
				if g.persistTicks < g.persistTime {
					return true
				}
			}
			return false
		}
		g.persistCounting, g.persistTicks = false, 0
		if !defaults {
			return true
		}
	}
	if !mustSee || alreadyTarget || sense.CanSee(m, t) {
		g.forgetActive = g.forgetTicks
		g.mustSeeActive = mustSee
		return true
	}
	return false
}

// canReachAfterDelay is TargetGoal::_canReachAfterDelay: a path to t ending
// within 1.5 blocks of it, rechecked every 10-14 ticks.
func (g *targetBase) canReachAfterDelay(m *mob.Mob, t world.Entity) bool {
	g.reachDelay--
	if g.reachDelay <= 0 {
		g.reachCache = 0
	}
	if g.reachCache == 0 {
		g.reachDelay = 10 + m.Rand().IntN(5)
		ok := false
		if p := m.Navigation().CreatePathToEntity(m, t); p != nil {
			end, pos := p.End(), t.Position()
			dx, dz := float64(end[0])-math.Floor(pos[0]), float64(end[2])-math.Floor(pos[2])
			ok = dx*dx+dz*dz <= 2.25
		}
		g.reachCache = 2
		if ok {
			g.reachCache = 1
		}
	}
	return g.reachCache != 2
}

// withinRange is TargetGoal::_withinRange: the follow range attribute.
func (g *targetBase) withinRange(m *mob.Mob, t world.Entity) bool {
	r := m.Attr(mob.AttrFollowRange)
	if m.Pos().Sub(t.Position()).LenSqr() <= r*r {
		return true
	}
	m.ResetTarget()
	return false
}

// CanContinueToUse is TargetGoal::canContinueToUse.
func (g *targetBase) CanContinueToUse(m *mob.Mob) bool {
	t, ok := m.Target()
	if !ok {
		return false
	}
	if !sense.Alive(t) {
		m.ResetTarget()
		return false
	}
	if sense.IsPlayer(t) && sense.DifficultyID(m.Tx()) == 0 {
		m.ResetTarget()
		return false
	}
	if !g.attackOwner && m.Owner() != nil {
		if o, ok := t.(interface{ Owner() *world.EntityHandle }); ok && o.Owner() == m.Owner() {
			m.ResetTarget()
			return false
		}
	}
	if !g.withinRange(m, t) {
		return false
	}
	if g.mustSeeActive {
		if sense.CanSee(m, t) {
			g.unseen = 0
		} else if g.unseen++; g.unseen > g.forgetActive {
			m.ResetTarget()
			return false
		}
	}
	if sense.Invulnerable(t) {
		m.ResetTarget()
		return false
	}
	if g.reevaluateActive && !g.matchesTargetTypes(m, t, g.mustSeeActive, nil) {
		m.ResetTarget()
		return false
	}
	return true
}

// Start is TargetGoal::start.
func (g *targetBase) Start(*mob.Mob) {
	g.reachCache, g.reachDelay, g.unseen, g.reevaluateActive = 0, 0, 0, false
}

// Tick is TargetGoal::tick.
func (g *targetBase) Tick(*mob.Mob) {
	if g.persistCounting {
		g.persistTicks++
	}
}
