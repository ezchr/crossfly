//go:build crossfly_full

package locomotion

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// floatWander is minecraft:behavior.float_wander (BDS FloatWanderGoal, ghast
// and bat): drift towards a random point.
//
//	xz_dist 10, y_dist 7 (>= 1), y_offset 0, must_reach false,
//	random_reselect false, float_duration [0, 0] s. Flags none.
//
// Every tick the mob's velocity gets (target - pos)/max(dist, 0.1) x its
// movement speed. A new target (pos + random in +-xz, +-y, + y_offset) is
// picked when the current one is closer than 1 block, beyond the follow range,
// not air, (must_reach) not reachable in a straight line, or (random_reselect)
// every float_duration seconds. The 1.26 keys navigate_around_surface,
// surface_xz_dist, surface_y_dist and use_home_position_restriction (happy
// ghast) are not modelled.
type floatWander struct {
	mob.GoalBase
	xz, y, yOff    float64
	mustReach      bool
	randomReselect bool
	duration       floatRange
	target         mgl64.Vec3
	timer          int
	init           bool
}

func newFloatWander(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		XZ             *int       `json:"xz_dist"`
		Y              *int       `json:"y_dist"`
		YOffset        float64    `json:"y_offset"`
		MustReach      bool       `json:"must_reach"`
		RandomReselect bool       `json:"random_reselect"`
		Duration       floatRange `json:"float_duration"`
	}{}
	if err := decodeGoal(spec, &j); err != nil {
		return nil, err
	}
	g := &floatWander{GoalBase: mob.GoalBase{Control: spec.FlagsOr(0)}, xz: 10, y: 7, yOff: j.YOffset, mustReach: j.MustReach, randomReselect: j.RandomReselect, duration: j.Duration}
	if j.XZ != nil {
		g.xz = float64(max(*j.XZ, 1))
	}
	if j.Y != nil {
		g.y = float64(max(*j.Y, 1))
	}
	return g, nil
}

func (g *floatWander) CanUse(m *mob.Mob) bool { return hasNavigation(m) && !resting(m) }

func (g *floatWander) Tick(m *mob.Mob) {
	tx := m.Tx()
	pos := m.Position()
	if !g.init {
		g.target, g.init = pos, true
	}
	d := g.target.Sub(pos)
	dist := d.Len()
	ok := airAt(tx, cube.PosFromVec3(g.target))
	maxR := g.xz + g.y
	if fr := m.Attr(mob.AttrFollowRange); fr > 0 {
		maxR = fr
	}
	if dist < 1 || dist > maxR {
		ok = false
	}
	if g.randomReselect {
		old := g.timer
		g.timer--
		if old <= 0 {
			ok = false
			g.timer = int(g.duration.roll(m) * 20)
		}
	}
	if ok && (!g.mustReach || g.canReach(m, g.target, dist)) {
		g.apply(m, d, dist)
		return
	}
	r := m.Rand()
	g.target = mgl64.Vec3{
		pos[0] + (r.Float64()*2-1)*g.xz,
		pos[1] + (r.Float64()*2-1)*g.y + g.yOff,
		pos[2] + (r.Float64()*2-1)*g.xz,
	}
	d = g.target.Sub(pos)
	dist = d.Len()
	valid := true
	if g.mustReach && !g.canReach(m, g.target, dist) {
		valid = false
		g.target = pos
	}
	if valid && airAt(tx, cube.PosFromVec3(g.target)) {
		g.apply(m, d, dist)
	}
}

func (g *floatWander) apply(m *mob.Mob, d mgl64.Vec3, dist float64) {
	s := m.Speed()
	m.SetVelocity(m.Vel().Add(d.Mul(s / math.Max(dist, 0.1))))
}

// canReach is FloatWanderGoal::canReach: the box moved along the line in
// unit steps never collides.
func (g *floatWander) canReach(m *mob.Mob, target mgl64.Vec3, dist float64) bool {
	if dist <= 1 {
		return true
	}
	step := target.Sub(m.Position()).Mul(1 / dist)
	box := m.BBox()
	tx := m.Tx()
	for i := 1.0; i < dist; i++ {
		box = box.Translate(step)
		if boxCollides(tx, box) {
			return false
		}
	}
	return true
}

// boxCollides reports whether any block box intersects box.
func boxCollides(tx *world.Tx, box cube.BBox) bool {
	min, max := box.Min(), box.Max()
	for x := int(math.Floor(min[0])); x <= int(math.Floor(max[0])); x++ {
		for y := int(math.Floor(min[1])); y <= int(math.Floor(max[1])); y++ {
			for z := int(math.Floor(min[2])); z <= int(math.Floor(max[2])); z++ {
				p := cube.Pos{x, y, z}
				if !inRange(tx, p) {
					continue
				}
				b := tx.Block(p)
				if _, ok := b.(block.Air); ok {
					continue
				}
				for _, bb := range b.Model().BBox(p, tx) {
					if bb.Translate(vec(p)).IntersectsWith(box) {
						return true
					}
				}
			}
		}
	}
	return false
}

func (g *floatWander) AppendDebugInfo(b *strings.Builder) {
	fmt.Fprintf(b, "FloatWander to %.1f %.1f %.1f", g.target[0], g.target[1], g.target[2])
}

// randomHover is minecraft:behavior.random_hover (BDS RandomHoverGoal, bee,
// allay): with a 1 in interval chance a tick, fly to a random spot ahead of
// the mob, hover_height blocks above the ground (or anywhere in the air when
// there is no such spot).
//
//	speed_multiplier 1, interval 120, xz_dist 10, y_dist 7 (>= 1), y_offset 0,
//	hover_height [0, 0] (min 0..100, max min..110). Flags move.
type randomHover struct {
	mob.GoalBase
	speed       float64
	interval    int
	xz, y, yOff int
	hover       floatRange
	path        mob.Path
}

func newRandomHover(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Speed    *float64   `json:"speed_multiplier"`
		Interval *int       `json:"interval"`
		XZ       *int       `json:"xz_dist"`
		Y        *int       `json:"y_dist"`
		YOffset  float64    `json:"y_offset"`
		Hover    floatRange `json:"hover_height"`
	}{}
	if err := decodeGoal(spec, &j); err != nil {
		return nil, err
	}
	g := &randomHover{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)}, speed: 1, interval: 120, xz: 10, y: 7, yOff: int(j.YOffset), hover: j.Hover}
	if j.Speed != nil {
		g.speed = *j.Speed
	}
	if j.Interval != nil {
		g.interval = *j.Interval
	}
	if j.XZ != nil {
		g.xz = max(*j.XZ, 1)
	}
	if j.Y != nil {
		g.y = max(*j.Y, 1)
	}
	g.hover.Min = mgl64.Clamp(g.hover.Min, 0, 100)
	g.hover.Max = mgl64.Clamp(g.hover.Max, g.hover.Min, 110)
	return g, nil
}

func (g *randomHover) CanUse(m *mob.Mob) bool {
	if !hasNavigation(m) || resting(m) {
		return false
	}
	if g.interval > 0 && m.Rand().IntN(g.interval) != 0 {
		return false
	}
	return g.computeTarget(m)
}

func (g *randomHover) computeTarget(m *mob.Mob) bool {
	view := viewVector(m)
	q := posQuery{xz: g.xz, y: g.y, yOff: g.yOff, view: &view, hover: g.hover, tries: 10}
	if pos, ok := randomPos(m, q); ok {
		if p := m.Navigation().CreatePath(m, blockCenter(pos)); p != nil {
			g.path = p
			return true
		}
	}
	q.fly = true
	g.path = nil
	if pos, ok := randomPos(m, q); ok {
		g.path = m.Navigation().CreatePath(m, blockCenter(pos))
	}
	return g.path != nil
}

func (g *randomHover) move(m *mob.Mob) {
	if g.path == nil || !m.Navigation().MoveAlong(m, g.path, g.speed) {
		g.path = nil
	}
}

func (g *randomHover) Start(m *mob.Mob) { g.move(m) }

func (g *randomHover) Tick(m *mob.Mob) {
	if m.Navigation().Done() {
		g.computeTarget(m)
		g.move(m)
	}
}

func (g *randomHover) CanContinueToUse(m *mob.Mob) bool {
	return hasNavigation(m) && !resting(m) && g.path != nil
}

func (g *randomHover) Stop(m *mob.Mob) {
	g.path = nil
	m.Navigation().Stop(m)
}

func (g *randomHover) AppendDebugInfo(b *strings.Builder) { b.WriteString("RandomHover") }

// randomFly is minecraft:behavior.random_fly (BDS RandomFlyingGoal, parrot):
// a random stroll for fliers, 1 in 120 ticks while the mob has been active
// for less than 100 ticks; with can_land_on_trees (default) it usually picks
// a free spot on leaves or logs within 3 blocks sideways and 6 up or down.
//
//	speed_multiplier 1, xz_dist 10, y_dist 7, can_land_on_trees true. Flags
//	move|look.
type randomFly struct {
	mob.GoalBase
	speed  float64
	xz, y  int
	yOff   int
	trees  bool
	wanted mgl64.Vec3
	done   bool
}

func newRandomFly(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Speed   *float64 `json:"speed_multiplier"`
		XZ      *int     `json:"xz_dist"`
		Y       *int     `json:"y_dist"`
		YOffset float64  `json:"y_offset"`
		Trees   *bool    `json:"can_land_on_trees"`
	}{}
	if err := decodeGoal(spec, &j); err != nil {
		return nil, err
	}
	g := &randomFly{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, speed: 1, xz: 10, y: 7, trees: true, yOff: int(j.YOffset)}
	if j.Speed != nil {
		g.speed = *j.Speed
	}
	if j.XZ != nil {
		g.xz = max(*j.XZ, 1)
	}
	if j.Y != nil {
		g.y = max(*j.Y, 1)
	}
	if j.Trees != nil {
		g.trees = *j.Trees
	}
	return g, nil
}

func (g *randomFly) CanUse(m *mob.Mob) bool {
	if !hasNavigation(m) || m.InactivityTicks() >= 100 || m.Rand().IntN(120) != 0 {
		return false
	}
	return g.setWanted(m)
}

func (g *randomFly) setWanted(m *mob.Mob) bool {
	var pos cube.Pos
	found := false
	if m.Physics().InWater || isOverWater(m) {
		pos, found = randomPos(m, posQuery{xz: g.xz, y: g.y, fly: canFlyFlag(m), swim: isSwimmer(m), tries: 10})
	}
	if g.trees && m.Rand().Float64() >= 0.001 {
		pos, found = g.treePos(m)
	}
	if !found {
		pos, found = randomPos(m, posQuery{xz: g.xz, y: g.y, yOff: g.yOff, fly: canFlyFlag(m), swim: isSwimmer(m), tries: 10})
		if !found {
			return false
		}
	}
	g.wanted = blockCenter(pos)
	return true
}

// treePos is RandomFlyingGoal::_getTreePos: the first free two-block space
// on leaves or logs around the mob.
func (g *randomFly) treePos(m *mob.Mob) (cube.Pos, bool) {
	tx := m.Tx()
	b := cube.PosFromVec3(m.Position())
	for x := b[0] - 3; x < b[0]+3; x++ {
		for z := b[2] - 3; z < b[2]+3; z++ {
			for y := b[1] - 6; y < b[1]+6; y++ {
				p := cube.Pos{x, y, z}
				if p == b || !inRange(tx, p) {
					continue
				}
				switch tx.Block(p.Side(cube.FaceDown)).(type) {
				case block.Leaves, block.Log, block.Wood:
				default:
					continue
				}
				if airAt(tx, p) && airAt(tx, p.Side(cube.FaceUp)) {
					return p, true
				}
			}
		}
	}
	return cube.Pos{}, false
}

func (g *randomFly) Start(m *mob.Mob) {
	if !m.Navigation().MoveTo(m, g.wanted, g.speed) {
		g.done = true
	}
}

func (g *randomFly) Tick(m *mob.Mob)                    { g.done = m.Navigation().Done() }
func (g *randomFly) CanContinueToUse(*mob.Mob) bool     { return !g.done }
func (g *randomFly) Stop(m *mob.Mob)                    { m.Navigation().Stop(m); g.done = false }
func (g *randomFly) AppendDebugInfo(b *strings.Builder) { b.WriteString("RandomFlying") }

// floatTempt is minecraft:behavior.float_tempt (happy ghast, 1.26; not in
// BDS 1.21.3, so it follows TemptGoal with the vertical movement of a flier):
// a player within within_radius (16) holding one of items draws the mob,
// which flies at speed_multiplier towards the player's eyes until within
// stop_distance (default 1.5 + width/2), looking at them. on_tempt_start and
// on_tempt_end fire as it starts and stops; after it ends it waits 100 ticks.
type floatTempt struct {
	mob.GoalBase
	speed, radius, stop float64
	items               []string
	vertical            bool
	onStart, onEnd      sense.Trigger
	player              *world.EntityHandle
	cooldown            int
}

func newFloatTempt(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Speed    *float64 `json:"speed_multiplier"`
		Radius   *float64 `json:"within_radius"`
		Stop     *float64 `json:"stop_distance"`
		Items    []string `json:"items"`
		Vertical *bool    `json:"can_tempt_vertically"`
	}{}
	if err := decodeGoal(spec, &j); err != nil {
		return nil, err
	}
	raw := struct {
		OnStart json.RawMessage `json:"on_tempt_start"`
		OnEnd   json.RawMessage `json:"on_tempt_end"`
	}{}
	_ = spec.Decode(&raw)
	g := &floatTempt{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, speed: 1, radius: 16, stop: -1, vertical: true}
	if j.Speed != nil {
		g.speed = *j.Speed
	}
	if j.Radius != nil {
		g.radius = *j.Radius
	}
	if j.Stop != nil {
		g.stop = *j.Stop
	}
	if j.Vertical != nil {
		g.vertical = *j.Vertical
	}
	for _, it := range j.Items {
		g.items = append(g.items, normaliseItem(it))
	}
	var err error
	if g.onStart, err = sense.ParseTrigger(raw.OnStart); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	if g.onEnd, err = sense.ParseTrigger(raw.OnEnd); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	return g, nil
}

func normaliseItem(s string) string {
	if !strings.Contains(s, ":") {
		s = "minecraft:" + s
	}
	return s
}

func (g *floatTempt) tempts(p *player.Player) bool {
	main, off := p.HeldItems()
	for _, s := range [2]item.Stack{main, off} {
		if s.Empty() {
			continue
		}
		name, _ := s.Item().EncodeItem()
		if slices.Contains(g.items, name) {
			return true
		}
	}
	return false
}

func (g *floatTempt) find(m *mob.Mob) (*player.Player, bool) {
	tx := m.Tx()
	best, bestD := (*player.Player)(nil), g.radius*g.radius
	for e := range tx.EntitiesWithin(m.BBox().Grow(g.radius)) {
		p, ok := e.(*player.Player)
		if !ok || !p.GameMode().Visible() || p.Dead() || !g.tempts(p) {
			continue
		}
		if d := p.Position().Sub(m.Position()).LenSqr(); d <= bestD {
			best, bestD = p, d
		}
	}
	return best, best != nil
}

func (g *floatTempt) CanUse(m *mob.Mob) bool {
	if g.cooldown > 0 {
		g.cooldown--
		return false
	}
	if !hasNavigation(m) {
		return false
	}
	p, ok := g.find(m)
	if ok {
		g.player = p.H()
	}
	return ok
}

func (g *floatTempt) CanContinueToUse(m *mob.Mob) bool {
	p, ok := g.find(m)
	if ok {
		g.player = p.H()
	}
	return ok
}

func (g *floatTempt) Start(m *mob.Mob) {
	if p, ok := g.player.Entity(m.Tx()); ok && !g.onStart.Empty() {
		ctx := m.Ctx()
		ctx.Other, ctx.Player = p, p
		g.onStart.Fire(m, ctx)
	}
}

func (g *floatTempt) stopDistance(m *mob.Mob) float64 {
	if g.stop >= 0 {
		return g.stop
	}
	return m.BBox().Width()/2 + 1.5
}

func (g *floatTempt) Tick(m *mob.Mob) {
	e, ok := g.player.Entity(m.Tx())
	if !ok {
		return
	}
	p := e.(*player.Player)
	eyes := sense.EyePos(p)
	m.LookControl().SetLookAt(eyes, 30, 40)
	target := eyes
	if !g.vertical {
		target[1] = m.Position()[1]
	}
	if target.Sub(m.Position()).Len() <= g.stopDistance(m) {
		m.Navigation().Stop(m)
		return
	}
	if !m.Navigation().MoveTo(m, target, g.speed) {
		m.MoveControl().SetWantedPosition(target, g.speed)
	}
}

func (g *floatTempt) Stop(m *mob.Mob) {
	m.Navigation().Stop(m)
	g.cooldown = 100
	if e, ok := g.player.Entity(m.Tx()); ok && !g.onEnd.Empty() {
		ctx := m.Ctx()
		ctx.Other, ctx.Player = e, e
		g.onEnd.Fire(m, ctx)
	} else if !g.onEnd.Empty() {
		g.onEnd.Fire(m, m.Ctx())
	}
	g.player = nil
}

func (g *floatTempt) AppendDebugInfo(b *strings.Builder) { b.WriteString("FloatTempt") }
