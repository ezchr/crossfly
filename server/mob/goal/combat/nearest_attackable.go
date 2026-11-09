//go:build crossfly_full

package combat

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/world"
)

// nearestAttackableTarget is minecraft:behavior.nearest_attackable_target
// (BDS NearestAttackableTargetGoal on TargetGoal).
type nearestAttackableTarget struct {
	targetBase
	attackInterval  int     // +224, 0: use scanInterval
	scanInterval    int     // +228
	setPersistent   bool    // +232
	searchHeight    float64 // +236
	invisibleMult   float64 // +240
	sneakMult       float64 // +244
	reselectTargets bool    // +152

	selected    *world.EntityHandle // +136
	selectedFor *descriptor         // +144
	cooldowns   map[*descriptor]int64
}

func newNearestAttackableTarget(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		EntityTypes            json.RawMessage `json:"entity_types"`
		MustSee                bool            `json:"must_see"`
		MustSeeForgetDuration  *float64        `json:"must_see_forget_duration"`
		MustReach              bool            `json:"must_reach"`
		AttackOwner            bool            `json:"attack_owner"`
		PersistTime            float64         `json:"persist_time"`
		AttackInterval         json.RawMessage `json:"attack_interval"`
		AttackIntervalMin      *float64        `json:"attack_interval_min"`
		ScanInterval           *int            `json:"scan_interval"`
		SetPersistent          bool            `json:"set_persistent"`
		TargetSearchHeight     *float64        `json:"target_search_height"`
		TargetInvisibleMult    *float64        `json:"target_invisible_multiplier"`
		TargetSneakVisibleMult *float64        `json:"target_sneak_visibility_multiplier"`
		ReselectTargets        bool            `json:"reselect_targets"`
		WithinRadius           float64         `json:"within_radius"`
	}
	if err := spec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	d, err := parseDescriptors(v.EntityTypes, 16)
	if err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	g := &nearestAttackableTarget{
		targetBase: targetBase{
			// BDS target goals use flag 1, which only competes with other
			// target goals (the selector compares flags within a kind).
			GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)}, key: spec.Key, descriptors: d,
			mustSee: v.MustSee, forgetTicks: 60, mustReach: v.MustReach, attackOwner: v.AttackOwner,
			persistTime: sense.Ticks(v.PersistTime),
		},
		scanInterval: 10, setPersistent: v.SetPersistent, searchHeight: -1, invisibleMult: 0.7, sneakMult: 0.8,
		reselectTargets: v.ReselectTargets, cooldowns: map[*descriptor]int64{},
	}
	if v.MustSeeForgetDuration != nil {
		g.forgetTicks = sense.Ticks(*v.MustSeeForgetDuration)
	}
	// attack_interval is the 1/N chance per tick of a search (BDS
	// Random::nextInt(attack_interval) == 0); a range is rolled per goal.
	if r, err := sense.ParseRange(v.AttackInterval, sense.Range{}); err == nil {
		g.attackInterval = int(math.Round(r.Max))
	}
	if v.AttackIntervalMin != nil && g.attackInterval == 0 {
		g.attackInterval = int(*v.AttackIntervalMin)
	}
	if v.ScanInterval != nil && *v.ScanInterval > 0 {
		g.scanInterval = *v.ScanInterval
	}
	if v.TargetSearchHeight != nil {
		g.searchHeight = *v.TargetSearchHeight
	}
	if v.TargetInvisibleMult != nil {
		g.invisibleMult = *v.TargetInvisibleMult
	}
	if v.TargetSneakVisibleMult != nil {
		g.sneakMult = *v.TargetSneakVisibleMult
	}
	return g, nil
}

// canStartSearching is NearestAttackableTargetGoal::_canStartSearching.
func (g *nearestAttackableTarget) canStartSearching(m *mob.Mob) bool {
	if g.attackInterval > 0 {
		return m.Rand().IntN(g.attackInterval) == 0
	}
	id := m.H().UUID()
	return (uint64(m.Ticks())+binary.LittleEndian.Uint64(id[:8]))%uint64(g.scanInterval) == 0
}

// CanUse ...
func (g *nearestAttackableTarget) CanUse(m *mob.Mob) bool {
	return g.canStartSearching(m) && g.selectTarget(m)
}

// selectTarget is NearestAttackableTargetGoal::_selectTarget.
func (g *nearestAttackableTarget) selectTarget(m *mob.Mob) bool {
	e, d := g.findTarget(m)
	g.selected, g.selectedFor = nil, d
	if e != nil {
		g.selected = e.H()
	}
	if d != nil {
		g.cooldowns[d] = m.Ticks() + int64(d.cooldown)
	}
	return g.selected != nil
}

// findTarget is NearestAttackableTargetGoal::_findTarget: the nearest
// entity (sorted by distance) that can be attacked and is visible.
func (g *nearestAttackableTarget) findTarget(m *mob.Mob) (world.Entity, *descriptor) {
	radius := 0.0
	for _, d := range g.descriptors {
		radius = math.Max(radius, d.maxDist)
	}
	if len(g.descriptors) == 0 {
		radius = m.Attr(mob.AttrFollowRange)
	}
	h := radius
	if g.searchHeight > 0 {
		h = g.searchHeight
	}
	pos := m.Pos()
	box := cube.Box(pos[0]-radius, pos[1]-h, pos[2]-radius, pos[0]+radius, pos[1]+h, pos[2]+radius)
	type cand struct {
		e    world.Entity
		dist float64
	}
	var cands []cand
	for e := range m.Tx().EntitiesWithin(box) {
		if e.H() == m.H() {
			continue
		}
		cands = append(cands, cand{e, e.Position().Sub(pos).LenSqr()})
	}
	slices.SortStableFunc(cands, func(a, b cand) int { return compareFloat(a.dist, b.dist) })
	for _, c := range cands {
		var d *descriptor
		if !g.canAttack(m, c.e, false, g.mustSee, &d) {
			continue
		}
		if g.mustReach && !g.canReachAfterDelay(m, c.e) {
			continue
		}
		if !hasFamilies(c.e) {
			continue
		}
		if d == nil {
			if len(g.descriptors) == 0 {
				g.markPersistent(m)
				return c.e, nil
			}
			continue
		}
		if g.inCooldown(m, d) || !g.isTargetVisible(m, c.e, d.maxDist, d.maxHeight) {
			continue
		}
		g.markPersistent(m)
		return c.e, d
	}
	return nil, nil
}

func (g *nearestAttackableTarget) markPersistent(m *mob.Mob) {
	if g.setPersistent {
		m.SetPersistent(true)
	}
}

func (g *nearestAttackableTarget) inCooldown(m *mob.Mob, d *descriptor) bool {
	if d.cooldown <= 0 {
		return false
	}
	t, ok := g.cooldowns[d]
	return ok && m.Ticks() < t
}

// isTargetVisible is NearestAttackableTargetGoal::isTargetVisible: distance
// and height limits, shrunk for sneaking and invisible targets.
func (g *nearestAttackableTarget) isTargetVisible(m *mob.Mob, t world.Entity, maxDist, maxHeight float64) bool {
	distSq := t.Position().Sub(m.Pos()).LenSqr()
	dy := math.Abs(t.Position()[1] - m.Pos()[1])
	mult := 1.0
	if sense.Sneaking(t) {
		mult = g.sneakMult
	}
	if sense.Invisible(t) {
		mult *= math.Max(sense.ArmourCover(t), 0.1) * g.invisibleMult
	}
	if maxDist >= 0 && distSq > (mult*maxDist)*(mult*maxDist) {
		return false
	}
	if maxHeight < 0 {
		return true
	}
	return mult*maxHeight >= dy
}

// CanContinueToUse is NearestAttackableTargetGoal::canContinueToUse.
func (g *nearestAttackableTarget) CanContinueToUse(m *mob.Mob) bool {
	if !g.targetBase.CanContinueToUse(m) {
		return false
	}
	if g.selected != nil && g.selectedFor != nil {
		e, ok := g.selected.Entity(m.Tx())
		if !ok || !g.isTargetVisible(m, e, g.selectedFor.maxDist, g.selectedFor.maxHeight) {
			m.ResetTarget()
			return false
		}
	}
	if g.reselectTargets {
		if g.selectTarget(m) {
			if e, ok := g.selected.Entity(m.Tx()); ok {
				m.SetTarget(e)
			}
		} else if !g.mustSeeActive || g.unseen > g.forgetActive {
			m.ResetTarget()
			return false
		}
	}
	return true
}

// Start ...
func (g *nearestAttackableTarget) Start(m *mob.Mob) {
	g.targetBase.Start(m)
	if g.selected == nil {
		return
	}
	if e, ok := g.selected.Entity(m.Tx()); ok {
		m.SetTarget(e)
	}
}

// AppendDebugInfo ...
func (g *nearestAttackableTarget) AppendDebugInfo(b *strings.Builder) {
	b.WriteString("NearestAttackableTarget")
}

// hasFamilies reports whether e has a type family (BDS requires the
// FamilyTypeDefinition: mobs and players, not items or projectiles).
func hasFamilies(e world.Entity) bool {
	_, ok := mob.Of(e)
	return ok || sense.IsPlayer(e)
}

func compareFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
