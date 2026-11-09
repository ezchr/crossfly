//go:build crossfly_full

package combat

// Goals of the misc package (server/mob/goal/misc) that build on this
// package's target and melee bases: nearest_prioritized_attackable_target,
// delayed_attack and stomp_attack. They register from their own init so the
// rest of the package is untouched.

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

func init() {
	mob.RegisterGoal("minecraft:behavior.nearest_prioritized_attackable_target", newNearestPrioritized)
	mob.RegisterGoal("minecraft:behavior.delayed_attack", newDelayedAttack)
	mob.RegisterGoal("minecraft:behavior.stomp_attack", newStompAttack)
}

// nearestPrioritized is minecraft:behavior.nearest_prioritized_attackable_target
// (BDS NearestPrioritizedAttackableTargetGoal::_findTarget): like
// nearest_attackable_target, but of the candidates (nearest first) the one
// whose entity_types entry has the lowest "priority" wins; on equal
// priority the nearest. Foxes prefer rabbits and chickens (or fish, arctic
// foxes), witches players over raiders.
type nearestPrioritized struct {
	*nearestAttackableTarget
	prio map[*descriptor]int
}

func newNearestPrioritized(spec mob.GoalSpec) (mob.Goal, error) {
	g, err := newNearestAttackableTarget(spec)
	if err != nil {
		return nil, err
	}
	nat := g.(*nearestAttackableTarget)
	var v struct {
		EntityTypes json.RawMessage `json:"entity_types"`
	}
	if err := spec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	var list []struct {
		Priority int `json:"priority"`
	}
	if err := json.Unmarshal(v.EntityTypes, &list); err != nil {
		var one struct {
			Priority int `json:"priority"`
		}
		if json.Unmarshal(v.EntityTypes, &one) == nil {
			list = append(list, one)
		}
	}
	p := &nearestPrioritized{nearestAttackableTarget: nat, prio: map[*descriptor]int{}}
	for i := range nat.descriptors {
		if i < len(list) {
			p.prio[&nat.descriptors[i]] = list[i].Priority
		}
	}
	return p, nil
}

// findTarget is NearestPrioritizedAttackableTargetGoal::_findTarget.
func (g *nearestPrioritized) findTarget(m *mob.Mob) (world.Entity, *descriptor) {
	n := g.nearestAttackableTarget
	radius := 0.0
	for _, d := range n.descriptors {
		radius = math.Max(radius, d.maxDist)
	}
	if len(n.descriptors) == 0 {
		radius = m.Attr(mob.AttrFollowRange)
	}
	h := radius
	if n.searchHeight > 0 {
		h = n.searchHeight
	}
	pos := m.Pos()
	type cand struct {
		e    world.Entity
		dist float64
	}
	var cands []cand
	for e := range m.Tx().EntitiesWithin(cube.Box(pos[0]-radius, pos[1]-h, pos[2]-radius, pos[0]+radius, pos[1]+h, pos[2]+radius)) {
		if e.H() != m.H() {
			cands = append(cands, cand{e, e.Position().Sub(pos).LenSqr()})
		}
	}
	slices.SortStableFunc(cands, func(a, b cand) int { return compareFloat(a.dist, b.dist) })
	var best world.Entity
	var bestD *descriptor
	bestPrio := math.MaxInt32
	for _, c := range cands {
		var d *descriptor
		if !n.canAttack(m, c.e, false, n.mustSee, &d) {
			continue
		}
		if n.mustReach && !n.canReachAfterDelay(m, c.e) {
			continue
		}
		if !hasFamilies(c.e) || d == nil {
			continue
		}
		if n.inCooldown(m, d) || !n.isTargetVisible(m, c.e, d.maxDist, d.maxHeight) {
			continue
		}
		if p := g.prio[d]; p < bestPrio {
			best, bestD, bestPrio = c.e, d, p
		}
	}
	if best != nil {
		n.markPersistent(m)
	}
	return best, bestD
}

// selectTarget is _selectTarget with the prioritised search.
func (g *nearestPrioritized) selectTarget(m *mob.Mob) bool {
	n := g.nearestAttackableTarget
	e, d := g.findTarget(m)
	n.selected, n.selectedFor = nil, d
	if e != nil {
		n.selected = e.H()
	}
	if d != nil {
		n.cooldowns[d] = m.Ticks() + int64(d.cooldown)
	}
	return n.selected != nil
}

// CanUse ...
func (g *nearestPrioritized) CanUse(m *mob.Mob) bool {
	return g.canStartSearching(m) && g.selectTarget(m)
}

// CanContinueToUse is NearestAttackableTargetGoal::canContinueToUse with the
// prioritised reselection.
func (g *nearestPrioritized) CanContinueToUse(m *mob.Mob) bool {
	n := g.nearestAttackableTarget
	if !n.targetBase.CanContinueToUse(m) {
		return false
	}
	if n.selected != nil && n.selectedFor != nil {
		e, ok := n.selected.Entity(m.Tx())
		if !ok || !n.isTargetVisible(m, e, n.selectedFor.maxDist, n.selectedFor.maxHeight) {
			m.ResetTarget()
			return false
		}
	}
	if n.reselectTargets {
		if g.selectTarget(m) {
			if e, ok := n.selected.Entity(m.Tx()); ok {
				m.SetTarget(e)
			}
		} else if !n.mustSeeActive || n.unseen > n.forgetActive {
			m.ResetTarget()
			return false
		}
	}
	return true
}

// AppendDebugInfo ...
func (g *nearestPrioritized) AppendDebugInfo(b *strings.Builder) {
	b.WriteString("NearestPrioritizedAttackableTarget")
}

// delayedAttack is minecraft:behavior.delayed_attack (ravager, BDS
// DelayedAttackGoal on MeleeAttackGoal): when the target is in reach the mob
// stops and starts a swing of attack_duration (0.75 s, 15 ticks) with the
// delayed_attack actor flag on; the hit lands hit_delay_pct (0.5) into the
// swing if the target is still in reach and seen.
type delayedAttack struct {
	meleeAttack
	duration, hitAt int
	swing           int // ticks into the swing, 0 none
}

func newDelayedAttack(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		rawMelee
		AttackDuration *float64 `json:"attack_duration"`
		HitDelayPct    *float64 `json:"hit_delay_pct"`
	}
	if err := spec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	b, err := newMeleeBase(spec, v.rawMelee)
	if err != nil {
		return nil, err
	}
	g := &delayedAttack{meleeAttack: meleeAttack{meleeBase: b, reachMultiplier: or(v.ReachMultiplier, 2)}}
	g.duration = max(sense.Ticks(or(v.AttackDuration, 0.75)), 1)
	g.hitAt = int(float64(g.duration) * math.Max(0, math.Min(1, or(v.HitDelayPct, 0.5))))
	return g, nil
}

func (g *delayedAttack) inReach(m *mob.Mob, t world.Entity) (bool, float64) {
	tb, mb := sense.BBox(t), m.BBox()
	tpos := t.Position()
	tpos[1] = tb.Min()[1]
	reach := g.reachSq(m, t)
	horiz := sense.HorizontalDistSq(tpos, m.Pos())
	dist3 := tpos.Sub(m.Pos()).LenSqr()
	return (horiz <= reach && tb.Max()[1] > mb.Min()[1] && tb.Min()[1] < mb.Max()[1]) || dist3 <= reach, dist3
}

// Tick is DelayedAttackGoal::tick / _manageAttackTime.
func (g *delayedAttack) Tick(m *mob.Mob) {
	t, ok := g.lockedTarget(m)
	if !ok {
		return
	}
	g.cooldown = max(g.cooldown-1, 0)
	reach, dist3 := g.inReach(m, t)
	los := sense.LineOfSight(m.Tx(), m.EyePosition(), sense.EyePos(t))
	if g.swing > 0 {
		m.LookControl().SetLookAtEntity(t, g.xMaxRot, g.yMaxHead)
		g.swing++
		if g.swing-1 == g.hitAt {
			g.attackTarget(m, t, reach, los)
		}
		if g.swing > g.duration {
			g.swing = 0
			m.SetActorFlag(protocol.EntityDataFlagDelayedAttack, false)
		}
		return
	}
	canAttack := reach && g.cooldown <= 0
	g.moveToTarget(m, t, canAttack, dist3)
	if canAttack && los && sense.WithinFOV(m, sense.Center(t), g.fov) {
		g.swing = 1
		g.cooldown = g.cooldownTime
		m.Navigation().Stop(m)
		m.SetActorFlag(protocol.EntityDataFlagDelayedAttack, true)
		m.PlaySound("attack.strong")
		if g.hitAt == 0 {
			g.swing++
			g.attackTarget(m, t, reach, los)
		}
	}
}

// Stop ...
func (g *delayedAttack) Stop(m *mob.Mob) {
	g.meleeBase.Stop(m)
	g.swing = 0
	m.SetActorFlag(protocol.EntityDataFlagDelayedAttack, false)
}

// AppendDebugInfo ...
func (g *delayedAttack) AppendDebugInfo(b *strings.Builder) { b.WriteString("DelayedAttack") }

// stompAttack is minecraft:behavior.stomp_attack (polar bear, BDS
// StompAttackGoal on MeleeAttackGoal): the stomp hits the target within
// the melee reach scaled by stomp_range_multiplier (2); a target only
// within no_damage_range_multiplier (2) times that range makes the bear
// rear up (the standing actor flag, from 10 ticks before the attack is
// ready) and restarts the cooldown instead (source: java/paper
// PolarBear.PolarBearMeleeAttackGoal).
type stompAttack struct {
	meleeAttack
	stompMult, noDamageMult float64
}

func newStompAttack(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		rawMelee
		StompRange    *float64 `json:"stomp_range_multiplier"`
		NoDamageRange *float64 `json:"no_damage_range_multiplier"`
	}
	if err := spec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	b, err := newMeleeBase(spec, v.rawMelee)
	if err != nil {
		return nil, err
	}
	return &stompAttack{
		meleeAttack: meleeAttack{meleeBase: b, reachMultiplier: or(v.ReachMultiplier, 2)},
		stompMult:   or(v.StompRange, 2), noDamageMult: or(v.NoDamageRange, 2),
	}, nil
}

// stompReachSq is the stomp's damage range squared: the melee reach with
// the width scaled by stomp_range_multiplier / reach_multiplier.
func (g *stompAttack) stompReachSq(m *mob.Mob, t world.Entity) float64 {
	w := math.Max(sense.Width(m), 0.5) * g.stompMult
	return w*w + sense.Width(t)
}

// Tick ...
func (g *stompAttack) Tick(m *mob.Mob) {
	t, ok := g.lockedTarget(m)
	if !ok {
		return
	}
	g.cooldown = max(g.cooldown-1, 0)
	tpos := t.Position()
	tpos[1] = sense.BBox(t).Min()[1]
	dist3 := tpos.Sub(m.Pos()).LenSqr()
	stomp := g.stompReachSq(m, t)
	noDamage := stomp * g.noDamageMult * g.noDamageMult
	los := sense.LineOfSight(m.Tx(), m.EyePosition(), sense.EyePos(t))
	canAttack := dist3 <= stomp && g.cooldown <= 0
	g.moveToTarget(m, t, canAttack, dist3)
	switch {
	case dist3 <= stomp:
		if g.cooldown <= 0 {
			m.SetActorFlag(protocol.EntityDataFlagStanding, false)
		}
		g.attackTarget(m, t, canAttack, los)
	case dist3 <= noDamage:
		if g.cooldown <= 0 {
			g.cooldown = g.cooldownTime
			m.SetActorFlag(protocol.EntityDataFlagStanding, false)
		}
		if g.cooldown <= 10 && !m.ActorFlag(protocol.EntityDataFlagStanding) {
			m.SetActorFlag(protocol.EntityDataFlagStanding, true)
			m.PlaySound("warn")
		}
	default:
		g.cooldown = g.cooldownTime
		m.SetActorFlag(protocol.EntityDataFlagStanding, false)
	}
}

// Stop ...
func (g *stompAttack) Stop(m *mob.Mob) {
	g.meleeBase.Stop(m)
	m.SetActorFlag(protocol.EntityDataFlagStanding, false)
}

// AppendDebugInfo ...
func (g *stompAttack) AppendDebugInfo(b *strings.Builder) { b.WriteString("StompAttack") }
