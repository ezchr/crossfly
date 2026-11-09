//go:build crossfly_full

package misc

import (
	"encoding/json"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/kit"
	"github.com/df-mc/dragonfly/server/world"
)

// moveTowardsTarget is minecraft:behavior.move_towards_target (iron golem,
// BDS MoveTowardsTargetGoal; source: java/paper MoveTowardsTargetGoal):
// with a target within within_radius (0 = any) it walks at
// speed_multiplier (1) to a random spot up to 16/7 blocks towards the
// target, and keeps going while the path lasts and the target stays in
// range.
type moveTowardsTarget struct {
	mob.GoalBase
	radius, speed float64
}

func newMoveTowardsTarget(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		WithinRadius    float64  `json:"within_radius"`
		SpeedMultiplier *float64 `json:"speed_multiplier"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	return &moveTowardsTarget{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)}, radius: v.WithinRadius, speed: kit.Or(v.SpeedMultiplier, 1)}, nil
}

func (g *moveTowardsTarget) target(m *mob.Mob) (world.Entity, bool) {
	t, ok := m.Target()
	if !ok || !kit.Alive(t) {
		return nil, false
	}
	if g.radius > 0 && kit.DistSq(t.Position(), m.Position()) > g.radius*g.radius {
		return nil, false
	}
	return t, true
}

// CanUse ...
func (g *moveTowardsTarget) CanUse(m *mob.Mob) bool {
	t, ok := g.target(m)
	if !ok {
		return false
	}
	// RandomPos::getPosTowards: a spot in the target's direction.
	d := t.Position().Sub(m.Position())
	if d.Len() > 16 {
		d = d.Normalize().Mul(16)
	}
	pos, ok := kit.Ground(m.Tx(), cube.PosFromVec3(m.Position().Add(d)), 7)
	if !ok {
		return false
	}
	return kit.MoveToBlock(m, pos, g.speed)
}

// CanContinueToUse ...
func (g *moveTowardsTarget) CanContinueToUse(m *mob.Mob) bool {
	_, ok := g.target(m)
	return ok && !m.Navigation().Done()
}

// Stop ...
func (g *moveTowardsTarget) Stop(m *mob.Mob) { kit.Stop(m) }

// AppendDebugInfo ...
func (g *moveTowardsTarget) AppendDebugInfo(b *strings.Builder) { b.WriteString("MoveTowardsTarget") }

// targetWhenPushed is minecraft:behavior.target_when_pushed (iron golem,
// BDS TargetWhenPushedGoal): a target goal. An entity pushing the mob (its
// box overlapping the mob's) that matches entity_types (filters with the
// pusher as "other") becomes the target with percent_chance (5) % per
// push.
type targetWhenPushed struct {
	mob.GoalBase
	chance  float64
	filters []mob.Filter
	picked  *world.EntityHandle
}

func newTargetWhenPushed(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		PercentChance *float64        `json:"percent_chance"`
		EntityTypes   json.RawMessage `json:"entity_types"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	g := &targetWhenPushed{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)}, chance: kit.Or(v.PercentChance, 5)}
	var list []struct {
		Filters json.RawMessage `json:"filters"`
	}
	if !kit.IsNull(v.EntityTypes) {
		if err := json.Unmarshal(v.EntityTypes, &list); err != nil {
			var one struct {
				Filters json.RawMessage `json:"filters"`
			}
			if err := json.Unmarshal(v.EntityTypes, &one); err != nil {
				return nil, err
			}
			list = append(list, one)
		}
	}
	for _, e := range list {
		f, err := kit.Filter(e.Filters)
		if err != nil {
			return nil, err
		}
		g.filters = append(g.filters, f)
	}
	return g, nil
}

// IsTargetGoal ...
func (*targetWhenPushed) IsTargetGoal() bool { return true }

// CanUse ...
func (g *targetWhenPushed) CanUse(m *mob.Mob) bool {
	g.picked = nil
	// Pushing entities touch the mob's box (entity push keeps them just
	// apart, so allow a small gap); positions are searched around it.
	box := m.BBox().Grow(0.1)
	b := box.Grow(2)
	for e := range m.Tx().EntitiesWithin(b) {
		if e.H() == m.H() || !kit.Alive(e) || !kit.BBox(e).IntersectsWith(box) {
			continue
		}
		if kit.IsPlayer(e) && kit.Creative(e) {
			continue
		}
		ctx := kit.OtherCtx(m, e)
		match := false
		for _, f := range g.filters {
			if f.Test(ctx) {
				match = true
				break
			}
		}
		if !match || m.Rand().Float64()*100 >= g.chance {
			continue
		}
		g.picked = e.H()
		return true
	}
	return false
}

// CanContinueToUse ...
func (g *targetWhenPushed) CanContinueToUse(*mob.Mob) bool { return false }

// Start ...
func (g *targetWhenPushed) Start(m *mob.Mob) {
	if e, ok := g.picked.Entity(m.Tx()); ok {
		m.SetTarget(e)
	}
}

// AppendDebugInfo ...
func (g *targetWhenPushed) AppendDebugInfo(b *strings.Builder) { b.WriteString("TargetWhenPushed") }

// holdGround is minecraft:behavior.hold_ground (patrolling pillagers, BDS
// HoldGroundGoal): with a target within min_radius (10) the mob stands
// still and faces it, fires within_radius_event and, with broadcast,
// gives the target to mobs within broadcast_range (0) that pass
// broadcast_filters (the mob as "other"; default: mobs of its type) and
// have none.
type holdGround struct {
	mob.GoalBase
	radius, bcRange float64
	broadcast       bool
	bcFilter        mob.Filter
	hasBcFilter     bool
	event           []kit.Trigger
}

func newHoldGround(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		MinRadius        *float64        `json:"min_radius"`
		Broadcast        bool            `json:"broadcast"`
		BroadcastRange   float64         `json:"broadcast_range"`
		BroadcastFilters json.RawMessage `json:"broadcast_filters"`
		Event            json.RawMessage `json:"within_radius_event"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	g := &holdGround{
		GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)},
		radius:   kit.Or(v.MinRadius, 10), bcRange: v.BroadcastRange, broadcast: v.Broadcast,
		hasBcFilter: !kit.IsNull(v.BroadcastFilters),
	}
	var err error
	if g.bcFilter, err = kit.Filter(v.BroadcastFilters); err != nil {
		return nil, err
	}
	if g.event, err = kit.ParseTriggers(v.Event); err != nil {
		return nil, err
	}
	return g, nil
}

func (g *holdGround) target(m *mob.Mob) (world.Entity, bool) {
	t, ok := m.Target()
	if !ok || !kit.Alive(t) || kit.DistSq(t.Position(), m.Position()) > g.radius*g.radius {
		return nil, false
	}
	return t, true
}

// CanUse ...
func (g *holdGround) CanUse(m *mob.Mob) bool {
	_, ok := g.target(m)
	return ok
}

// Start ...
func (g *holdGround) Start(m *mob.Mob) {
	t, _ := g.target(m)
	kit.Stop(m)
	kit.FireAll(m, g.event, kit.OtherCtx(m, t))
	if !g.broadcast || g.bcRange <= 0 {
		return
	}
	for _, e := range kit.Around(m, g.bcRange, g.bcRange) {
		om, ok := kit.Mob(e)
		if !ok || om.Dead() || kit.DistSq(om.Position(), m.Position()) > g.bcRange*g.bcRange {
			continue
		}
		if _, has := om.Target(); has {
			continue
		}
		if g.hasBcFilter {
			ctx := om.Ctx()
			ctx.Other = m
			if !g.bcFilter.Test(ctx) {
				continue
			}
		} else if om.Identifier() != m.Identifier() {
			continue
		}
		om.SetTarget(t)
	}
}

// Tick ...
func (g *holdGround) Tick(m *mob.Mob) {
	if t, ok := g.target(m); ok {
		kit.LookAtEntity(m, t)
		m.SetYaw(kit.YawTowards(m.Position(), t.Position()))
	}
}

// AppendDebugInfo ...
func (g *holdGround) AppendDebugInfo(b *strings.Builder) { b.WriteString("HoldGround") }

// followMob is minecraft:behavior.follow_mob (wild parrots, baby happy
// ghasts, BDS FollowMobGoal): a mob within search_range (0) that passes
// filters (default: any non-player mob not of the same type, not
// invisible; preferred_actor_type first) is followed at speed_multiplier
// (1), repathing every 10 ticks, until within stop_distance (2).
type followMob struct {
	mob.GoalBase
	rng, stop, speed float64
	filter           mob.Filter
	hasFilter        bool
	preferred        string
	target           *world.EntityHandle
	repath           int
}

func newFollowMob(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		SearchRange     float64         `json:"search_range"`
		StopDistance    *float64        `json:"stop_distance"`
		SpeedMultiplier *float64        `json:"speed_multiplier"`
		Filters         json.RawMessage `json:"filters"`
		Preferred       string          `json:"preferred_actor_type"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	g := &followMob{
		GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)},
		rng:      v.SearchRange, stop: kit.Or(v.StopDistance, 2), speed: kit.Or(v.SpeedMultiplier, 1),
		hasFilter: !kit.IsNull(v.Filters), preferred: v.Preferred,
	}
	var err error
	if g.filter, err = kit.Filter(v.Filters); err != nil {
		return nil, err
	}
	return g, nil
}

func (g *followMob) accepts(m *mob.Mob, e world.Entity) bool {
	if g.hasFilter {
		return g.filter.Test(kit.OtherCtx(m, e))
	}
	om, ok := kit.Mob(e)
	return ok && om.Identifier() != m.Identifier() && !om.Invisible()
}

// CanUse ...
func (g *followMob) CanUse(m *mob.Mob) bool {
	if g.rng <= 0 {
		return false
	}
	var first world.Entity
	for _, e := range kit.Around(m, g.rng, g.rng) {
		if !kit.Alive(e) || !g.accepts(m, e) {
			continue
		}
		if g.preferred != "" && typeName(e) == g.preferred {
			g.target = e.H()
			return true
		}
		if first == nil {
			first = e
		}
	}
	if first == nil {
		return false
	}
	g.target = first.H()
	return true
}

func typeName(e world.Entity) string {
	n := e.H().Type().EncodeEntity()
	return strings.TrimPrefix(n, "minecraft:")
}

// CanContinueToUse ...
func (g *followMob) CanContinueToUse(m *mob.Mob) bool {
	t, ok := g.target.Entity(m.Tx())
	if !ok || !kit.Alive(t) {
		return false
	}
	d := kit.DistSq(t.Position(), m.Position())
	return !m.Navigation().Done() || d > g.stop*g.stop
}

// Start ...
func (g *followMob) Start(m *mob.Mob) { g.repath = 0 }

// Tick ...
func (g *followMob) Tick(m *mob.Mob) {
	t, ok := g.target.Entity(m.Tx())
	if !ok {
		return
	}
	kit.LookAtEntity(m, t)
	if g.repath--; g.repath > 0 {
		return
	}
	g.repath = 10
	if kit.DistSq(t.Position(), m.Position()) <= g.stop*g.stop {
		kit.Stop(m)
		return
	}
	m.Navigation().MoveToEntity(m, t, g.speed)
}

// Stop ...
func (g *followMob) Stop(m *mob.Mob) {
	g.target = nil
	kit.Stop(m)
}

// AppendDebugInfo ...
func (g *followMob) AppendDebugInfo(b *strings.Builder) { b.WriteString("FollowMob") }

// lookAtTarget is minecraft:behavior.look_at_target (wither): the mob
// looks at its target for look_time (2..4 s) at a time.
type lookAtTarget struct {
	mob.GoalBase
	time  kit.Range
	timer int
}

func newLookAtTarget(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		LookTime json.RawMessage `json:"look_time"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	r, err := kit.ParseRange(v.LookTime, kit.Range{Min: 2, Max: 4})
	if err != nil {
		return nil, err
	}
	return &lookAtTarget{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagLook)}, time: r}, nil
}

// CanUse ...
func (g *lookAtTarget) CanUse(m *mob.Mob) bool {
	t, ok := m.Target()
	return ok && kit.Alive(t)
}

// CanContinueToUse ...
func (g *lookAtTarget) CanContinueToUse(m *mob.Mob) bool { return g.CanUse(m) && g.timer > 0 }

// Start ...
func (g *lookAtTarget) Start(m *mob.Mob) { g.timer = kit.RollTicks(m, g.time) }

// Tick ...
func (g *lookAtTarget) Tick(m *mob.Mob) {
	g.timer--
	if t, ok := m.Target(); ok {
		kit.LookAtEntity(m, t)
	}
}

// AppendDebugInfo ...
func (g *lookAtTarget) AppendDebugInfo(b *strings.Builder) { b.WriteString("LookAtTarget") }
