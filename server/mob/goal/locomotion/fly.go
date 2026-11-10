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
	vc "github.com/df-mc/dragonfly/server/mob/component/village"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// floatWander is minecraft:behavior.float_wander (BDS FloatWanderGoal, ghast,
// bat and happy ghast): drift towards a random point.
//
//	xz_dist 10, y_dist 7 (>= 1), y_offset 0, must_reach false,
//	random_reselect false, float_duration [0, 0] s,
//	float_wander_has_move_control true: flags move (the 26.60
//	FloatWanderGoalDefinition defaults; ghast and bat turn it off, so a
//	steering rider's player_ride_tamed keeps a happy ghast from wandering).
//
// Every tick the mob's velocity gets (target - pos)/max(dist, 0.1) x its
// movement speed. A new target is picked when the current one is closer than
// 1 block, beyond the follow range, not air, (must_reach) not reachable in a
// straight line, or (random_reselect) every float_duration seconds. As the
// 26.60 FloatWanderGoal::tick picks it:
//
//   - use_home_position_restriction (default true): one RandomPos::getPos
//     candidate (block offsets in +-xz, +-y, no y_offset), pulled towards
//     the home and kept within its restriction radius (minecraft:home; the
//     happy ghast's 32 or 64); the mob's own position when the candidate is
//     outside. Without it: pos + random in +-xz, +-y, + y_offset.
//   - navigate_around_surface: up to 64 more picks until the target is air
//     with a block within surface_y_dist above or below or surface_xz_dist
//     along x or z (8, 8); failing that, a target above the highest block of
//     its column is mirrored below the mob.
//   - canReach steps the box along the line; additional_collision_buffer
//     grows it by 1 at the last step, and liquids block the way unless
//     allow_navigating_through_liquids.
type floatWander struct {
	mob.GoalBase
	xz, y, yOff     float64
	mustReach       bool
	randomReselect  bool
	duration        floatRange
	useHome         bool
	aroundSurface   bool
	surfXZ, surfY   int
	buffer, liquids bool
	target          mgl64.Vec3
	timer           int
	init            bool
}

func newFloatWander(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		XZ             *int       `json:"xz_dist"`
		Y              *int       `json:"y_dist"`
		YOffset        float64    `json:"y_offset"`
		MustReach      bool       `json:"must_reach"`
		RandomReselect bool       `json:"random_reselect"`
		Duration       floatRange `json:"float_duration"`
		MoveControl    *bool      `json:"float_wander_has_move_control"`
		UseHome        *bool      `json:"use_home_position_restriction"`
		AroundSurface  bool       `json:"navigate_around_surface"`
		SurfaceXZ      *int       `json:"surface_xz_dist"`
		SurfaceY       *int       `json:"surface_y_dist"`
		Buffer         bool       `json:"additional_collision_buffer"`
		Liquids        bool       `json:"allow_navigating_through_liquids"`
	}{}
	if err := decodeGoal(spec, &j); err != nil {
		return nil, err
	}
	g := &floatWander{GoalBase: mob.GoalBase{Control: spec.FlagsOr(0)}, xz: 10, y: 7, yOff: j.YOffset, mustReach: j.MustReach,
		randomReselect: j.RandomReselect, duration: j.Duration, useHome: j.UseHome == nil || *j.UseHome,
		aroundSurface: j.AroundSurface, surfXZ: 8, surfY: 8, buffer: j.Buffer, liquids: j.Liquids}
	if j.MoveControl == nil || *j.MoveControl {
		g.Control = mob.FlagMove
	}
	if j.XZ != nil {
		g.xz = float64(max(*j.XZ, 1))
	}
	if j.Y != nil {
		g.y = float64(max(*j.Y, 1))
	}
	if j.SurfaceXZ != nil {
		g.surfXZ = *j.SurfaceXZ
	}
	if j.SurfaceY != nil {
		g.surfY = *j.SurfaceY
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
	g.target = g.pick(m, pos)
	if g.aroundSurface {
		g.target = g.pickNearSurface(m, pos, g.target)
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

// pick is FloatWanderGoal's target pick: with use_home_position_restriction one RandomPos::getPos candidate
// (or the mob's position when there is none), else a random offset within xz, y and y_offset.
func (g *floatWander) pick(m *mob.Mob, pos mgl64.Vec3) mgl64.Vec3 {
	if g.useHome {
		if c, ok := homeRandomPos(m, int(g.xz), int(g.y)); ok {
			return vec(c)
		}
		return pos
	}
	r := m.Rand()
	return mgl64.Vec3{
		pos[0] + (r.Float64()*2-1)*g.xz,
		pos[1] + (r.Float64()*2-1)*g.y + g.yOff,
		pos[2] + (r.Float64()*2-1)*g.xz,
	}
}

// pickNearSurface is navigate_around_surface: picks again, up to 64 times, until the target is air near a
// block (surfaceNear); the last pick is then taken, mirrored below the mob if it is above the highest block
// of its column.
func (g *floatWander) pickNearSurface(m *mob.Mob, pos, target mgl64.Vec3) mgl64.Vec3 {
	if g.surfXZ <= 0 || g.surfY <= 0 {
		return target
	}
	tx := m.Tx()
	for i := 0; i < 64; i++ {
		t := cube.PosFromVec3(target)
		if airAt(tx, t) && g.surfaceNear(tx, t) {
			return target
		}
		target = g.pick(m, pos)
	}
	t := cube.PosFromVec3(target)
	if h := tx.HighestBlock(t[0], t[2]); h > tx.Range()[0] && target[1] > float64(h) {
		target[1] = pos[1] - math.Abs(pos[1]-target[1])
	}
	return target
}

// surfaceNear reports whether a block other than air lies within surface_y_dist - 1 above or below t or
// within surface_xz_dist - 1 along x or z.
func (g *floatWander) surfaceNear(tx *world.Tx, t cube.Pos) bool {
	solid := func(p cube.Pos) bool { return !airAt(tx, p) }
	for i := 1; i < g.surfY; i++ {
		if solid(t.Add(cube.Pos{0, i, 0})) || solid(t.Add(cube.Pos{0, -i, 0})) {
			return true
		}
	}
	for i := 1; i < g.surfXZ; i++ {
		if solid(t.Add(cube.Pos{0, 0, -i})) || solid(t.Add(cube.Pos{i, 0, 0})) ||
			solid(t.Add(cube.Pos{0, 0, i})) || solid(t.Add(cube.Pos{-i, 0, 0})) {
			return true
		}
	}
	return false
}

// homeRandomPos is RandomPos::getPos with one try (generateRandomPos): a block offset in +-xz, +-y from the
// mob's block, moved up to xz/2 - 1 more towards the home, snapped to the ground unless the mob flies, swims
// or is in a liquid, and refused outside the home restriction when the restriction reaches that far
// ((xz + radius)^2 > home distance^2 + 4).
func homeRandomPos(m *mob.Mob, xz, y int) (cube.Pos, bool) {
	if xz < 1 {
		return cube.Pos{}, false
	}
	pos := m.Position()
	base := cube.PosFromVec3(pos)
	home, radius, hasHome := vc.HomeRestriction(m)
	restrict := false
	if hasHome && radius >= 0 {
		d := home.Sub(base)
		restrict = float64((xz+radius)*(xz+radius)) > float64(d[0]*d[0]+d[1]*d[1]+d[2]*d[2])+4
	}
	r := m.Rand()
	dx := r.IntN(2*xz+1) - xz
	dz := r.IntN(2*xz+1) - xz
	dy := r.IntN(2*y+1) - y
	if hasHome && xz > 1 {
		k := xz/2 - 1
		if float64(home[0]) >= pos[0] {
			dx += r.IntN(k + 1)
		} else {
			dx -= r.IntN(k + 1)
		}
		if float64(home[2]) >= pos[2] {
			dz += r.IntN(k + 1)
		} else {
			dz -= r.IntN(k + 1)
		}
	}
	c := base.Add(cube.Pos{dx, dy, dz})
	if !(canFlyFlag(m) || isSwimmer(m) || m.Physics().InWater || m.Physics().InLava) {
		var ok bool
		if c, ok = snapToGround(m, c, base[1]-y, base[1]+y); !ok {
			return cube.Pos{}, false
		}
	}
	if restrict && !vc.WithinHomeRestriction(m, c.Vec3Centre()) {
		return cube.Pos{}, false
	}
	return c, true
}

func (g *floatWander) apply(m *mob.Mob, d mgl64.Vec3, dist float64) {
	s := m.Speed()
	m.SetVelocity(m.Vel().Add(d.Mul(s / math.Max(dist, 0.1))))
}

// canReach is FloatWanderGoal::canReach: the box moved along the line in
// unit steps never collides, nor (unless allow_navigating_through_liquids)
// touches a liquid; with additional_collision_buffer the box of the step
// dist - 1 is 1 block larger on every side.
func (g *floatWander) canReach(m *mob.Mob, target mgl64.Vec3, dist float64) bool {
	if dist <= 1 {
		return true
	}
	step := target.Sub(m.Position()).Mul(1 / dist)
	box := m.BBox()
	tx := m.Tx()
	for i := 1.0; i < dist; i++ {
		box = box.Translate(step)
		check := box
		if g.buffer && i == dist-1 {
			check = box.Grow(1)
		}
		if boxBlocked(tx, check, !g.liquids) {
			return false
		}
	}
	return true
}

// boxCollides reports whether any block box intersects box.
func boxCollides(tx *world.Tx, box cube.BBox) bool { return boxBlocked(tx, box, false) }

// boxBlocked reports whether any block box intersects box, or, if liquids, any liquid lies in the blocks
// the box touches.
func boxBlocked(tx *world.Tx, box cube.BBox, liquids bool) bool {
	min, max := box.Min(), box.Max()
	for x := int(math.Floor(min[0])); x <= int(math.Floor(max[0])); x++ {
		for y := int(math.Floor(min[1])); y <= int(math.Floor(max[1])); y++ {
			for z := int(math.Floor(min[2])); z <= int(math.Floor(max[2])); z++ {
				p := cube.Pos{x, y, z}
				if !inRange(tx, p) {
					continue
				}
				if liquids {
					if _, ok := tx.Liquid(p); ok {
						return true
					}
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
	if m.HasComponent("minecraft:navigation.float") {
		// FloatNavigation moves the mob only by its velocity (an adult happy ghast has no move control):
		// push it towards the player as float_wander pushes it towards its target.
		d := target.Sub(m.Position())
		m.SetVelocity(m.Vel().Add(d.Mul(m.Speed() * g.speed / math.Max(d.Len(), 0.1))))
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
