//go:build crossfly_full

package locomotion

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/goal/core"
	"github.com/df-mc/dragonfly/server/mob/physics"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// swimWithEntity is minecraft:behavior.swim_with_entity (BDS
// SwimWithEntityGoal, dolphins swimming with players).
//
//	success_rate 0.1, chance_to_stop 0.0333, state_check_interval 0.5 s,
//	catch_up_threshold 12, match_direction_threshold 2, catch_up_multiplier
//	2.5, speed_multiplier 1.5, search_range 20, stop_distance 5,
//	entity_types. Flags move|look.
//
// It picks a random swimming entity in range. Every state_check_interval it
// paths to a spot beside the entity (3 blocks to the side it is on): 2
// blocks ahead at catch_up_multiplier until within match_direction_threshold,
// then 6 ahead at speed_multiplier until further than catch_up_threshold.
// An entity that stops swimming makes it stop with chance_to_stop a tick.
type swimWithEntity struct {
	mob.GoalBase
	rate, stopChance                 float64
	interval                         int
	catchUpThreshold, matchThreshold float64
	catchUpMul, speedMul             float64
	searchRange, stopDistance        float64
	filters                          []mob.Filter

	target    world.Entity
	countdown int
	matching  bool
}

func newSwimWithEntity(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Rate       *float64          `json:"success_rate"`
		StopChance *float64          `json:"chance_to_stop"`
		Interval   *float64          `json:"state_check_interval"`
		CatchUpThr *float64          `json:"catch_up_threshold"`
		MatchThr   *float64          `json:"match_direction_threshold"`
		CatchUpMul *float64          `json:"catch_up_multiplier"`
		Speed      *float64          `json:"speed_multiplier"`
		Range      *float64          `json:"search_range"`
		Stop       *float64          `json:"stop_distance"`
		Types      []json.RawMessage `json:"entity_types"`
	}{}
	if err := decodeGoal(spec, &j); err != nil {
		return nil, err
	}
	g := &swimWithEntity{
		GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)},
		rate:     0.1, stopChance: 0.0333333, interval: 10, catchUpThreshold: 12, matchThreshold: 2,
		catchUpMul: 2.5, speedMul: 1.5, searchRange: 20, stopDistance: 5,
	}
	set := func(dst *float64, v *float64) {
		if v != nil {
			*dst = *v
		}
	}
	set(&g.rate, j.Rate)
	set(&g.stopChance, j.StopChance)
	set(&g.catchUpThreshold, j.CatchUpThr)
	set(&g.matchThreshold, j.MatchThr)
	set(&g.catchUpMul, j.CatchUpMul)
	set(&g.speedMul, j.Speed)
	set(&g.searchRange, j.Range)
	set(&g.stopDistance, j.Stop)
	if j.Interval != nil {
		g.interval = int(*j.Interval * 20)
	}
	filters, err := entityTypeFilters(j.Types)
	if err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	g.filters = filters
	return g, nil
}

// entityTypeFilters compiles the filters of an entity_types list (one
// object, or an array of {"filters": ...}).
func entityTypeFilters(list []json.RawMessage) ([]mob.Filter, error) {
	var out []mob.Filter
	for _, raw := range list {
		var e struct {
			Filters json.RawMessage `json:"filters"`
		}
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, err
		}
		f, err := mob.CompileFilter(e.Filters)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

func matchesAny(m *mob.Mob, filters []mob.Filter, e world.Entity) bool {
	if len(filters) == 0 {
		return true
	}
	ctx := m.Ctx()
	ctx.Other = e
	for _, f := range filters {
		if f.Test(ctx) {
			return true
		}
	}
	return false
}

// swimming reports whether e swims (Actor::isSwimming): players with the
// swimming pose, or entities that report Swimming.
func swimming(e world.Entity) bool {
	s, ok := e.(interface{ Swimming() bool })
	return ok && s.Swimming()
}

func (g *swimWithEntity) CanUse(m *mob.Mob) bool {
	if !canSwim(m) || !hasNavigation(m) || m.Rand().Float64() >= g.rate {
		return false
	}
	box := m.BBox().Grow(g.searchRange)
	var cand []world.Entity
	for e := range m.Tx().EntitiesWithin(box) {
		if e.H() == m.H() || !alive(m.Tx(), e) || !swimming(e) || !matchesAny(m, g.filters, e) {
			continue
		}
		cand = append(cand, e)
	}
	if len(cand) == 0 {
		return false
	}
	g.target = cand[m.Rand().IntN(len(cand))]
	return true
}

func (g *swimWithEntity) CanContinueToUse(m *mob.Mob) bool {
	if !canSwim(m) || !alive(m.Tx(), g.target) {
		return false
	}
	t, _ := g.target.H().Entity(m.Tx())
	if !swimming(t) && m.Rand().Float64() < g.stopChance {
		return false
	}
	return !m.Navigation().Done() || m.Position().Sub(t.Position()).LenSqr() > g.stopDistance*g.stopDistance
}

func (g *swimWithEntity) Start(*mob.Mob) { g.countdown, g.matching = 0, false }

func (g *swimWithEntity) Stop(m *mob.Mob) {
	g.target = nil
	m.Navigation().Stop(m)
}

func (g *swimWithEntity) Tick(m *mob.Mob) {
	t, ok := g.target.H().Entity(m.Tx())
	if !ok {
		return
	}
	m.LookControl().SetLookAt(t.Position(), 30, 30)
	if g.countdown--; g.countdown > 0 {
		return
	}
	g.countdown = g.interval
	var vel mgl64.Vec3
	if v, ok := t.(interface{ Velocity() mgl64.Vec3 }); ok {
		vel = v.Velocity()
	}
	dir := normalize(mgl64.Vec3{vel[0], 0, vel[2]})
	if dir == (mgl64.Vec3{}) {
		dir = normalize(mgl64.Vec3{-math.Sin(mgl64.DegToRad(t.Rotation().Yaw())), 0, math.Cos(mgl64.DegToRad(t.Rotation().Yaw()))})
	}
	side := normalize(mgl64.Vec3{-dir[2], 0, dir[0]})
	s := side.Dot(normalize(m.Position().Sub(t.Position())))
	if math.Abs(s) <= 1.1920929e-7 {
		s = 0.2
	}
	base := cube.PosFromVec3(t.Position())
	p := vec(base).Add(mgl64.Vec3{0.5, 0.5, 0.5}).Add(side.Mul(s * 3))
	p[1] = math.Floor(p[1]) - 1
	var speed float64
	if !g.matching {
		p = p.Add(floorVec(dir.Mul(2)))
		speed = g.catchUpMul
		if m.Position().Sub(p).Len() < g.matchThreshold {
			g.matching, g.countdown = true, 0
		}
	} else {
		p = p.Add(floorVec(dir.Mul(6)))
		speed = g.speedMul
		if m.Position().Sub(t.Position()).Len() > g.catchUpThreshold {
			g.matching, g.countdown = false, 0
		}
	}
	m.Navigation().MoveTo(m, p, speed)
}

func floorVec(v mgl64.Vec3) mgl64.Vec3 {
	return mgl64.Vec3{math.Floor(v[0]), math.Floor(v[1]), math.Floor(v[2])}
}

func (g *swimWithEntity) AppendDebugInfo(b *strings.Builder) {
	fmt.Fprintf(b, "SwimWithEntity matching=%v", g.matching)
}

// randomBreach is minecraft:behavior.random_breach (BDS RandomBreachingGoal):
// now and then swim to the air just above the water surface and jump out.
//
//	speed_multiplier 1, xz_dist 10, y_dist 7, interval 120, cooldown_time
//	0 s. Flags move|look.
//
// BDS paths to the spot; the navigation then sets the move control's breach
// flag and DolphinMoveControl leaps (0.4 forward, the jump power up). Here the
// goal does the leap once the mob is at the surface next to the spot.
type randomBreach struct {
	mob.GoalBase
	speed             float64
	xz, y, interval   int
	cooldownTicks     int
	cooldown, retries int
	wanted            mgl64.Vec3
	leapt             bool
}

func newRandomBreach(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Speed    *float64 `json:"speed_multiplier"`
		XZ       *int     `json:"xz_dist"`
		Y        *int     `json:"y_dist"`
		Interval *int     `json:"interval"`
		Cooldown *float64 `json:"cooldown_time"`
	}{}
	if err := decodeGoal(spec, &j); err != nil {
		return nil, err
	}
	g := &randomBreach{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, speed: 1, xz: 10, y: 7, interval: 120}
	if j.Speed != nil {
		g.speed = *j.Speed
	}
	if j.XZ != nil {
		g.xz = max(*j.XZ, 1)
	}
	if j.Y != nil {
		g.y = max(*j.Y, 1)
	}
	if j.Interval != nil {
		g.interval = *j.Interval
	}
	if j.Cooldown != nil {
		g.cooldownTicks = int(*j.Cooldown * 20)
	}
	return g, nil
}

func (g *randomBreach) CanUse(m *mob.Mob) bool {
	if !m.Physics().InWater {
		return false
	}
	if g.cooldown > 0 {
		g.cooldown--
		return false
	}
	if !hasNavigation(m) || m.Riding() {
		return false
	}
	if g.interval > 0 && m.Rand().IntN(g.interval) != 0 {
		return false
	}
	return g.setWanted(m)
}

func (g *randomBreach) setWanted(m *mob.Mob) bool {
	tx := m.Tx()
	pos, ok := core.RandomPos(m, g.xz, g.y, 0, nil, 10)
	if !ok {
		return false
	}
	p := cube.PosFromVec3(pos)
	if !waterAt(tx, p) {
		return false
	}
	for waterAt(tx, p) {
		if p[1]++; p[1] >= tx.Range()[1] {
			return false
		}
	}
	if !airAt(tx, p) {
		return false
	}
	if fr := m.Attr(mob.AttrFollowRange); fr > 0 && blockCenter(p).Sub(m.Position()).Len() > fr {
		return false
	}
	g.wanted = blockCenter(p)
	return true
}

func (g *randomBreach) Start(m *mob.Mob) {
	g.retries, g.leapt = 0, false
	m.Navigation().MoveTo(m, g.wanted, g.speed)
}

func (g *randomBreach) CanContinueToUse(m *mob.Mob) bool {
	if g.leapt || !m.Physics().InWater {
		return false
	}
	if !m.Navigation().Done() {
		return true
	}
	if cube.PosFromVec3(m.Position()) != cube.PosFromVec3(g.wanted) {
		if g.retries++; g.retries <= 3 {
			m.Navigation().MoveTo(m, g.wanted, g.speed)
			return true
		}
	}
	return false
}

func (g *randomBreach) Tick(m *mob.Mob) {
	pos := m.Position()
	d := g.wanted.Sub(pos)
	if g.leapt || !m.Physics().InWater || d[0]*d[0]+d[2]*d[2] > 2.25 {
		return
	}
	// The surface: the block over the mob's head is air.
	head := cube.PosFromVec3(pos.Add(mgl64.Vec3{0, m.BBox().Height() + 0.5, 0}))
	if !airAt(m.Tx(), head) {
		return
	}
	// DolphinMoveControl::_setupBreach: posDelta.y = 0, then the jump, and
	// 0.4 forward along the facing.
	yaw := mgl64.DegToRad(m.Yaw())
	dir := mgl64.Vec3{-math.Sin(yaw), 0, math.Cos(yaw)}
	if h := math.Hypot(d[0], d[2]); h > 0.1 {
		dir = mgl64.Vec3{d[0] / h, 0, d[2] / h}
	}
	power := physics.DefaultJumpPower
	if jp, ok := m.JumpControl().(interface{ JumpPower() float64 }); ok {
		power = jp.JumpPower()
	}
	v := m.Vel()
	m.SetVelocity(mgl64.Vec3{v[0] + dir[0]*0.4, power, v[2] + dir[2]*0.4})
	m.SetYaw(yawOf(dir))
	g.leapt = true
}

func (g *randomBreach) Stop(m *mob.Mob) {
	if g.leapt || m.Navigation().Done() {
		g.cooldown = g.cooldownTicks
	}
	if !g.leapt {
		m.Navigation().Stop(m)
	}
}

func (g *randomBreach) AppendDebugInfo(b *strings.Builder) {
	fmt.Fprintf(b, "RandomBreaching to %.1f %.1f %.1f", g.wanted[0], g.wanted[1], g.wanted[2])
}

// TreasureLocator finds the nearest underwater treasure structure (shipwreck
// or ocean ruin) from pos for find_underwater_treasure (BDS
// WorldGenerator::findNearestStructureFeature). Dragonfly generates no
// structures, so it is nil by default and the goal never finds one; a server
// with its own structures can set it.
var TreasureLocator func(tx *world.Tx, pos cube.Pos) (cube.Pos, bool)

// briber is what find_underwater_treasure needs from minecraft:bribeable.
type briber interface {
	Bribed() bool
	ResetBribe()
}

func bribeOf(m *mob.Mob) (briber, bool) {
	c, ok := m.Component("minecraft:bribeable")
	if !ok {
		return nil, false
	}
	b, ok := c.(briber)
	return b, ok
}

// findUnderwaterTreasure is minecraft:behavior.find_underwater_treasure (BDS
// FindUnderwaterTreasureGoal): a bribed dolphin leads the way to the nearest
// shipwreck or ocean ruin (TreasureLocator), every 10 ticks pathing up to half
// its follow range towards it, turning aside when stuck; it gives up after
// travelling stop_distance blocks (default 2) or within 15 blocks. Flags move.
//
//	speed_multiplier 1, search_range 0 (a findable chest that close to the
//	structure is the target instead), stop_distance 2.
type findUnderwaterTreasure struct {
	mob.GoalBase
	speed       float64
	searchRange int
	stopDist    int

	target               cube.Pos
	hasTarget            bool
	lastPos              mgl64.Vec3
	travelled, countdown int
	stuck, stuckTotal    int
}

func newFindUnderwaterTreasure(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Speed *float64 `json:"speed_multiplier"`
		Range *int     `json:"search_range"`
		Stop  *float64 `json:"stop_distance"`
	}{}
	if err := decodeGoal(spec, &j); err != nil {
		return nil, err
	}
	g := &findUnderwaterTreasure{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)}, speed: 1, stopDist: 2}
	if j.Speed != nil {
		g.speed = *j.Speed
	}
	if j.Range != nil {
		g.searchRange = *j.Range
	}
	if j.Stop != nil {
		g.stopDist = int(*j.Stop)
	}
	return g, nil
}

func (g *findUnderwaterTreasure) CanUse(m *mob.Mob) bool {
	b, ok := bribeOf(m)
	return ok && b.Bribed() && hasNavigation(m)
}

func (g *findUnderwaterTreasure) CanContinueToUse(m *mob.Mob) bool {
	return g.CanUse(m) && g.hasTarget
}

func (g *findUnderwaterTreasure) Start(m *mob.Mob) {
	g.hasTarget = false
	if TreasureLocator != nil {
		g.target, g.hasTarget = TreasureLocator(m.Tx(), cube.PosFromVec3(m.Position()))
	}
	g.countdown, g.travelled, g.stuck, g.stuckTotal = 10, 0, 0, 0
	g.lastPos = m.Position()
	if g.hasTarget && !m.Navigation().Done() {
		m.Navigation().Stop(m)
	}
}

func (g *findUnderwaterTreasure) Stop(m *mob.Mob) {
	g.hasTarget = false
	if b, ok := bribeOf(m); ok {
		b.ResetBribe()
	}
}

func (g *findUnderwaterTreasure) Tick(m *mob.Mob) {
	b, ok := bribeOf(m)
	if !g.hasTarget || !ok || !b.Bribed() {
		return
	}
	if g.countdown--; g.countdown > 0 {
		return
	}
	g.countdown = 10
	pos := m.Position()
	t := blockCenter(g.target)
	if t.Sub(pos).LenSqr() < 225 || g.stuckTotal > 3 {
		g.hasTarget = false
		b.ResetBribe()
		return
	}
	moved := pos.Sub(g.lastPos).Len()
	if moved >= 1 {
		g.stuck = 0
	} else {
		g.stuck++
		g.stuckTotal++
	}
	g.travelled += int(moved)
	g.lastPos = pos
	if g.travelled > g.stopDist {
		b.ResetBribe()
		return
	}
	// BDS broadcasts ActorEvent TreasureHunt (72) here: client particles.
	d := vec(g.target.Sub(cube.PosFromVec3(pos)))
	fr := m.Attr(mob.AttrFollowRange)
	var dest mgl64.Vec3
	switch {
	case d.LenSqr() <= fr:
		dest = t
	case g.stuck <= 1:
		dest = pos.Add(normalize(d).Mul(fr * 0.5))
	default:
		f := dominantFace(d)
		if f != cube.FaceUp && f != cube.FaceDown {
			if m.Rand().IntN(2) == 1 {
				f = f.Direction().RotateRight().Face()
			} else {
				f = f.Direction().RotateLeft().Face()
			}
		} else {
			f = f.Opposite()
		}
		dest = vec(cube.PosFromVec3(pos).Add(facePos(f, 10)))
	}
	m.Navigation().MoveTo(m, dest, g.speed)
}

// dominantFace is Facing::fromVec3: the face d points to most.
func dominantFace(d mgl64.Vec3) cube.Face {
	ax, ay, az := math.Abs(d[0]), math.Abs(d[1]), math.Abs(d[2])
	switch {
	case ay >= ax && ay >= az:
		if d[1] > 0 {
			return cube.FaceUp
		}
		return cube.FaceDown
	case ax >= az:
		if d[0] > 0 {
			return cube.FaceEast
		}
		return cube.FaceWest
	}
	if d[2] > 0 {
		return cube.FaceSouth
	}
	return cube.FaceNorth
}

func facePos(f cube.Face, n int) cube.Pos {
	o := cube.Pos{}.Side(f)
	return cube.Pos{o[0] * n, o[1] * n, o[2] * n}
}

func (g *findUnderwaterTreasure) AppendDebugInfo(b *strings.Builder) {
	fmt.Fprintf(b, "FindUnderwaterTreasure %v (%v)", g.target, g.hasTarget)
}
