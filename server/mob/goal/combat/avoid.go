//go:build crossfly_full

package combat

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// avoidMobType is minecraft:behavior.avoid_mob_type (BDS AvoidMobTypeGoal):
// run from the nearest matching mob to a random spot further away, walking,
// and sprinting once it is within 7 blocks.
type avoidMobType struct {
	mob.GoalBase
	descriptors      []descriptor
	probeXZ, probeY  int     // +104, +108
	maxDist, maxFlee float64 // +112, +116
	probPerStrength  float64 // +120
	walk, sprint     float64 // +132, +136
	ignoreVisibility bool    // +64
	onEscape         sense.Trigger

	// outnumbered[i]: entity_types[i] has check_if_outnumbered (descriptor
	// +86): a mob with group_size only avoids a match whose group is larger
	// than its own.
	outnumbered   []bool
	removeTarget  bool // +140
	sound         string
	soundInterval [2]float64 // +252
	nextSound     int64      // +312

	avoid    *world.EntityHandle
	desc     *descriptor
	wanted   mgl64.Vec3
	path     mob.Path
	sprinted bool

	cands []cand
	fctx  mob.FilterContext
}

func newAvoidMobType(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		EntityTypes            json.RawMessage `json:"entity_types"`
		MaxDist                *float64        `json:"max_dist"`
		MaxFlee                *float64        `json:"max_flee"`
		ProbabilityPerStrength *float64        `json:"probability_per_strength"`
		WalkSpeedMultiplier    *float64        `json:"walk_speed_multiplier"`
		SprintSpeedMultiplier  *float64        `json:"sprint_speed_multiplier"`
		IgnoreVisibility       bool            `json:"ignore_visibilty"`
		IgnoreVisibility2      bool            `json:"ignore_visibility"`
		OnEscape               json.RawMessage `json:"on_escape_event"`
		RemoveTarget           bool            `json:"remove_target"`
		AvoidMobSound          string          `json:"avoid_mob_sound"`
		SoundInterval          *struct {
			Min      *float64 `json:"min"`
			Max      *float64 `json:"max"`
			RangeMin *float64 `json:"range_min"`
			RangeMax *float64 `json:"range_max"`
		} `json:"sound_interval"`
	}
	if err := spec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	g := &avoidMobType{
		GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)},
		probeXZ:  16, probeY: 7,
		maxDist: or(v.MaxDist, 3), maxFlee: or(v.MaxFlee, 10),
		probPerStrength: or(v.ProbabilityPerStrength, 1),
		walk:            or(v.WalkSpeedMultiplier, 1), sprint: or(v.SprintSpeedMultiplier, 1),
		ignoreVisibility: v.IgnoreVisibility || v.IgnoreVisibility2,
		removeTarget:     v.RemoveTarget,
		sound:            v.AvoidMobSound,
		soundInterval:    [2]float64{}, // RandomValueBounds default 0
	}
	if si := v.SoundInterval; si != nil {
		lo, hi := si.Min, si.Max
		if si.RangeMin != nil {
			lo = si.RangeMin
		}
		if si.RangeMax != nil {
			hi = si.RangeMax
		}
		g.soundInterval[0] = or(lo, g.soundInterval[0])
		g.soundInterval[1] = or(hi, g.soundInterval[0])
	}
	d, err := parseDescriptors(v.EntityTypes, g.maxDist)
	if err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	for i := range d {
		raw := descriptorRaw(v.EntityTypes, i)
		if raw.WalkSpeedMultiplier == nil {
			d[i].walk = g.walk
		}
		if raw.SprintSpeedMultiplier == nil {
			d[i].sprint = g.sprint
		}
		if raw.MaxFlee == nil {
			d[i].maxFlee = g.maxFlee
		}
	}
	g.descriptors = d
	g.outnumbered = outnumberedFlags(v.EntityTypes, len(d))
	if g.onEscape, err = sense.ParseTrigger(v.OnEscape); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	return g, nil
}

// descriptorRaw re-reads entry i of entity_types to see which fields it set.
func descriptorRaw(raw json.RawMessage, i int) rawDescriptor {
	var list []rawDescriptor
	if json.Unmarshal(raw, &list) != nil {
		var one rawDescriptor
		_ = json.Unmarshal(raw, &one)
		list = []rawDescriptor{one}
	}
	if i < len(list) {
		return list[i]
	}
	return rawDescriptor{}
}

// outnumberedFlags reads check_if_outnumbered of each entity_types entry.
func outnumberedFlags(raw json.RawMessage, n int) []bool {
	type entry struct {
		Outnumbered bool `json:"check_if_outnumbered"`
	}
	var list []entry
	if json.Unmarshal(raw, &list) != nil {
		var one entry
		_ = json.Unmarshal(raw, &one)
		list = []entry{one}
	}
	out := make([]bool, n)
	any := false
	for i := range min(n, len(list)) {
		out[i] = list[i].Outnumbered
		any = any || out[i]
	}
	if !any {
		return nil
	}
	return out
}

// groupSizer is minecraft:group_size (package goal/misc/hostile).
type groupSizer interface{ Size() int }

// groupSize returns e's group_size count, and false when e has none.
func groupSize(e world.Entity) (int, bool) {
	om, ok := mob.Of(e)
	if !ok {
		return 0, false
	}
	c, ok := om.Component("minecraft:group_size")
	if !ok {
		return 0, false
	}
	gs, ok := c.(groupSizer)
	if !ok {
		return 0, false
	}
	return gs.Size(), true
}

// outnumberedBy is the check_if_outnumbered test of findNearestEntityToAvoid:
// a mob without group_size always avoids; one with it avoids e only when
// its group is smaller than e's (1 when e has no group_size).
func outnumberedBy(m *mob.Mob, e world.Entity) bool {
	own, ok := groupSize(m)
	if !ok {
		return true
	}
	other, ok := groupSize(e)
	if !ok {
		other = 1
	}
	return own < other
}

// nearest is AvoidMobTypeGoal::findNearestEntityToAvoid: the nearest living
// mob or player matching an entry within dist (the entry's max_dist when
// dist is 0, its max_flee when flee is set).
func (g *avoidMobType) nearest(m *mob.Mob, flee bool) (world.Entity, *descriptor) {
	if len(g.descriptors) == 0 {
		return nil, nil
	}
	pos := m.Pos()
	// One search over the largest box, then each entry in order over the
	// entities in its own box: the same entities in the same order as one
	// search per entry, and the checks that do not depend on the entry are
	// made once per entity.
	scan := 0.0
	for i := range g.descriptors {
		scan = math.Max(scan, math.Abs(g.searchRadius(&g.descriptors[i], flee)))
	}
	cands := g.cands[:0]
	for e := range m.Tx().EntitiesWithin(cube.Box(pos[0]-scan, pos[1]-scan, pos[2]-scan, pos[0]+scan, pos[1]+scan, pos[2]+scan)) {
		if e.H() == m.H() || !sense.Alive(e) || !hasFamilies(e) || sense.Invulnerable(e) || sense.Invisible(e) {
			continue
		}
		cands = append(cands, cand{e: e, dist: e.Position().Sub(pos).LenSqr()})
	}
	g.cands = cands
	defer clear(cands)
	if len(cands) == 0 {
		return nil, nil
	}
	ctx := &g.fctx
	*ctx = mob.FilterContext{Tx: m.Tx(), Self: m}
	defer func() { *ctx = mob.FilterContext{} }()
	if t, ok := m.Target(); ok {
		ctx.Target = t
	}
	for i := range g.descriptors {
		d := &g.descriptors[i]
		r := g.searchRadius(d, flee)
		box := cube.Box(pos[0]-r, pos[1]-r, pos[2]-r, pos[0]+r, pos[1]+r, pos[2]+r)
		var best world.Entity
		bestD := math.MaxFloat64
		for _, c := range cands {
			if c.dist >= bestD || c.dist > r*r || !box.Vec3Within(c.e.Position()) {
				continue
			}
			ctx.Other = c.e
			if !d.filter.Test(ctx) {
				continue
			}
			if g.outnumbered != nil && g.outnumbered[i] && !outnumberedBy(m, c.e) {
				continue
			}
			best, bestD = c.e, c.dist
		}
		if best != nil {
			return best, d
		}
	}
	return nil, nil
}

// searchRadius is how far nearest looks for d.
func (g *avoidMobType) searchRadius(d *descriptor, flee bool) float64 {
	if flee {
		return d.maxFlee
	}
	return d.maxDist
}

// CanUse is AvoidMobTypeGoal::canUse.
func (g *avoidMobType) CanUse(m *mob.Mob) bool {
	e, d := g.nearest(m, false)
	return g.canFindAvoidPath(m, e, d)
}

// canFindAvoidPath is AvoidMobTypeGoal::canFindAvoidPath.
func (g *avoidMobType) canFindAvoidPath(m *mob.Mob, e world.Entity, d *descriptor) bool {
	if e == nil {
		return false
	}
	g.avoid, g.desc = e.H(), d
	if !g.ignoreVisibility && !sense.CanSee(m, e) {
		return false
	}
	p, ok := PosAway(m, e.Position(), g.probeXZ, g.probeY)
	if !ok {
		return false
	}
	if p.Sub(e.Position()).LenSqr() < m.Pos().Sub(e.Position()).LenSqr() {
		return false
	}
	g.wanted = p
	g.path = m.Navigation().CreatePath(m, p)
	return g.path != nil
}

// PosAway returns a random standable position up to xz/y blocks away from
// m in the half facing away from from (BDS RandomPos::getPosAvoid, 10
// tries).
func PosAway(m *mob.Mob, from mgl64.Vec3, xz, y int) (mgl64.Vec3, bool) {
	tx, r := m.Tx(), m.Rand()
	pos := m.Pos()
	away := pos.Sub(from)
	away[1] = 0
	for i := 0; i < 10; i++ {
		off := mgl64.Vec3{float64(r.IntN(2*xz+1) - xz), float64(r.IntN(2*y+1) - y), float64(r.IntN(2*xz+1) - xz)}
		if off[0]*away[0]+off[2]*away[2] < 0 {
			continue
		}
		p := cube.PosFromVec3(pos.Add(off))
		if standable(tx, p) {
			return mgl64.Vec3{float64(p[0]) + 0.5, float64(p[1]), float64(p[2]) + 0.5}, true
		}
	}
	return mgl64.Vec3{}, false
}

// standable reports whether a mob could stand at p: solid below, two free
// blocks.
func standable(tx *world.Tx, p cube.Pos) bool {
	below := tx.Block(p.Side(cube.FaceDown))
	if len(below.Model().BBox(p.Side(cube.FaceDown), tx)) == 0 {
		return false
	}
	return len(tx.Block(p).Model().BBox(p, tx)) == 0 && len(tx.Block(p.Side(cube.FaceUp)).Model().BBox(p.Side(cube.FaceUp), tx)) == 0
}

// CanContinueToUse is AvoidMobTypeGoal::canContinueToUse.
func (g *avoidMobType) CanContinueToUse(m *mob.Mob) bool {
	e, d := g.nearest(m, true)
	if e == nil {
		return false
	}
	if m.Navigation().Done() {
		return g.canFindAvoidPath(m, e, d)
	}
	return true
}

// Start is AvoidMobTypeGoal::start: drop the target with remove_target,
// move along the path and set the avoiding_mobs flag.
func (g *avoidMobType) Start(m *mob.Mob) {
	g.sprinted = false
	if g.removeTarget {
		m.ResetTarget()
	}
	m.SetActorFlag(protocol.EntityDataFlagAvoidingMobs, true)
	if g.path != nil {
		m.Navigation().MoveAlong(m, g.path, g.speed(m))
		g.path = nil
	}
}

func (g *avoidMobType) speed(m *mob.Mob) float64 {
	if g.desc == nil {
		return g.walk
	}
	if e, ok := g.avoid.Entity(m.Tx()); ok && m.Pos().Sub(e.Position()).LenSqr() < 49 {
		return g.desc.sprint
	}
	return g.desc.walk
}

// Tick sprints within 7 blocks of the avoided mob, walks further away, and
// plays avoid_mob_sound every sound_interval.
func (g *avoidMobType) Tick(m *mob.Mob) {
	if g.avoid == nil {
		return
	}
	if _, ok := g.avoid.Entity(m.Tx()); !ok {
		return
	}
	m.Navigation().SetSpeed(g.speed(m))
	if g.nextSound < m.Ticks() {
		if g.sound != "" {
			m.PlaySound(g.sound)
		}
		lo, hi := g.soundInterval[0], g.soundInterval[1]
		f := lo
		if hi > lo {
			f += m.Rand().Float64() * (hi - lo)
		}
		g.nextSound = m.Ticks() + int64(f*20)
	}
}

// Stop is AvoidMobTypeGoal::stop: on_escape_event fires only when nothing
// to avoid is left within max_dist.
func (g *avoidMobType) Stop(m *mob.Mob) {
	if !g.onEscape.Empty() {
		if e, _ := g.nearest(m, false); e == nil {
			g.onEscape.Fire(m, nil)
		}
	}
	g.avoid, g.desc = nil, nil
	m.SetActorFlag(protocol.EntityDataFlagAvoidingMobs, false)
}

// AppendDebugInfo ...
func (g *avoidMobType) AppendDebugInfo(b *strings.Builder) { b.WriteString("AvoidMobType") }
