//go:build crossfly_full

package animal

import (
	"encoding/json"
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	cmisc "github.com/df-mc/dragonfly/server/mob/component/misc"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/kit"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// croak is minecraft:behavior.croak (BDS CroakGoal, defaults from
// CroakGoal::Definition): every interval seconds, while filters pass, the
// frog croaks for duration seconds (actor flag croaking).
//
//	duration 4.5..4.5 s, interval 10..20 s.
type croak struct {
	mob.GoalBase
	debugName
	duration, interval kit.Range
	filter             mob.Filter
	next, ends         int64
	init               bool
}

func newCroak(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Duration json.RawMessage `json:"duration"`
		Interval json.RawMessage `json:"interval"`
		Filters  json.RawMessage `json:"filters"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	g := &croak{GoalBase: mob.GoalBase{Control: spec.FlagsOr(0)}, debugName: "CroakGoal"}
	var err error
	if g.duration, err = kit.ParseRange(v.Duration, kit.Range{Min: 4.5, Max: 4.5}); err != nil {
		return nil, err
	}
	if g.interval, err = kit.ParseRange(v.Interval, kit.Range{Min: 10, Max: 20}); err != nil {
		return nil, err
	}
	if g.filter, err = kit.Filter(v.Filters); err != nil {
		return nil, err
	}
	return g, nil
}

func (g *croak) params() map[string]float64 {
	return map[string]float64{"duration_min": g.duration.Min, "duration_max": g.duration.Max, "interval_min": g.interval.Min, "interval_max": g.interval.Max}
}

// CanUse ...
func (g *croak) CanUse(m *mob.Mob) bool {
	if !g.init {
		g.init, g.next = true, m.Ticks()+int64(kit.RollTicks(m, g.interval))
	}
	return m.Ticks() >= g.next && g.filter.Test(m.Ctx())
}

// CanContinueToUse ...
func (g *croak) CanContinueToUse(m *mob.Mob) bool {
	return m.Ticks() < g.ends && g.filter.Test(m.Ctx())
}

// Start ...
func (g *croak) Start(m *mob.Mob) {
	g.ends = m.Ticks() + int64(kit.RollTicks(m, g.duration))
	kit.Flag(m, protocol.EntityDataFlagCroaking, true)
}

// Stop ...
func (g *croak) Stop(m *mob.Mob) {
	kit.Flag(m, protocol.EntityDataFlagCroaking, false)
	g.next = m.Ticks() + int64(kit.RollTicks(m, g.interval))
}

// eatMob is minecraft:behavior.eat_mob (BDS EatMobGoal, defaults from
// EatMobGoal::Definition; control flags move|look): the frog runs to its
// target at run_speed; within reach_mob_distance it shoots its tongue
// (eat_mob_sound, actor flag digest_mob... BDS "eat mob"), pulls the target
// in with pull_in_force for eat_animation_time, then the target is removed
// and loot_table drops where it was.
//
//	run_speed 1, eat_animation_time 1 s, pull_in_force 1,
//	reach_mob_distance 1.
type eatMob struct {
	mob.GoalBase
	debugName
	speed, pull, reach float64
	animTime           int
	sound, table       string

	target *world.EntityHandle
	eating int
	done   bool
}

func newEatMob(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		RunSpeed *float64 `json:"run_speed"`
		AnimTime *float64 `json:"eat_animation_time"`
		Pull     *float64 `json:"pull_in_force"`
		Reach    *float64 `json:"reach_mob_distance"`
		Sound    string   `json:"eat_mob_sound"`
		Loot     string   `json:"loot_table"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	return &eatMob{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, debugName: "EatMobGoal",
		speed: kit.Or(v.RunSpeed, 1), animTime: kit.Ticks(kit.Or(v.AnimTime, 1)), pull: kit.Or(v.Pull, 1), reach: kit.Or(v.Reach, 1),
		sound: v.Sound, table: v.Loot}, nil
}

func (g *eatMob) params() map[string]float64 {
	return map[string]float64{"run_speed": g.speed, "eat_animation_ticks": float64(g.animTime), "pull_in_force": g.pull, "reach_mob_distance": g.reach}
}

// CanUse ...
func (g *eatMob) CanUse(m *mob.Mob) bool {
	t, ok := m.Target()
	return ok && kit.Alive(t) && !m.Physics().InWater
}

// CanContinueToUse ...
func (g *eatMob) CanContinueToUse(m *mob.Mob) bool {
	if g.done {
		return false
	}
	if g.eating > 0 {
		return true
	}
	t, ok := m.Target()
	return ok && kit.Alive(t) && t.H() == g.target
}

// Start ...
func (g *eatMob) Start(m *mob.Mob) {
	t, _ := m.Target()
	g.target, g.eating, g.done = t.H(), 0, false
	m.Navigation().MoveToEntity(m, t, g.speed)
}

// Tick ...
func (g *eatMob) Tick(m *mob.Mob) {
	t, ok := g.target.Entity(m.Tx())
	if !ok || !kit.Alive(t) {
		g.done = true
		return
	}
	kit.LookAtEntity(m, t)
	if g.eating == 0 {
		reach := g.reach + kit.Width(m)/2 + kit.Width(t)/2
		if kit.DistSq(t.Position(), m.Position()) > reach*reach {
			if m.Ticks()%10 == 0 {
				m.Navigation().MoveToEntity(m, t, g.speed)
			}
			return
		}
		kit.Stop(m)
		kit.Sound(m, g.sound)
		kit.Flag(m, protocol.EntityDataFlagDigestMob, true)
		g.eating = 1
		return
	}
	g.eating++
	// Pull the target towards the mouth.
	if l, ok := t.(interface{ SetVelocity(mgl64.Vec3) }); ok {
		d := m.EyePosition().Sub(t.Position())
		if d.Len() > 0.5 {
			l.SetVelocity(d.Normalize().Mul(0.25 * g.pull))
		}
	}
	if g.eating < g.animTime {
		return
	}
	pos := t.Position()
	stacks := rollLoot(m, g.table)
	if tm, ok := kit.Mob(t); ok {
		_ = tm.Close()
	} else {
		_ = t.Close()
	}
	dropAt(m.Tx(), m, pos, stacks)
	m.ResetTarget()
	g.done = true
}

// Stop ...
func (g *eatMob) Stop(m *mob.Mob) {
	kit.Flag(m, protocol.EntityDataFlagDigestMob, false)
	g.target, g.eating = nil, 0
	kit.Stop(m)
}

// jumpToBlock is minecraft:behavior.jump_to_block (BDS JumpToBlockGoal,
// defaults from JumpToBlockGoal::Definition; control flags move|look; frogs
// and goats): after a cooldown the mob picks a standable block within
// search_width / search_height at least minimum_distance away (a
// preferred_blocks one with preferred_blocks_chance, never on
// forbidden_blocks), whose walking path would be at least
// minimum_path_length long, and jumps there on an arc whose launch speed is
// at most max_velocity (actor flag jump_goal while airborne).
//
//	search_width 8, search_height 10, minimum_path_length 5,
//	minimum_distance 2, scale_factor 0.7, max_velocity 1.5,
//	cooldown_range 10..20 s, preferred_blocks_chance 1.
type jumpToBlock struct {
	mob.GoalBase
	debugName
	width, height, minPath int
	minDist, scale, maxVel float64
	cooldown               kit.Range
	preferred, forbidden   kit.BlockList
	prefChance             float64

	next   int64
	init   bool
	target cube.Pos
	vel    mgl64.Vec3
	state  int // 0 pending, 1 airborne, 2 done
	ticks  int
}

func newJumpToBlock(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Width      *int            `json:"search_width"`
		Height     *int            `json:"search_height"`
		MinPath    *int            `json:"minimum_path_length"`
		MinDist    *float64        `json:"minimum_distance"`
		Scale      *float64        `json:"scale_factor"`
		MaxVel     *float64        `json:"max_velocity"`
		Cooldown   json.RawMessage `json:"cooldown_range"`
		Preferred  json.RawMessage `json:"preferred_blocks"`
		PrefChance *float64        `json:"preferred_blocks_chance"`
		Forbidden  json.RawMessage `json:"forbidden_blocks"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	g := &jumpToBlock{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, debugName: "JumpToBlockGoal",
		width: kit.Or(v.Width, 8), height: kit.Or(v.Height, 10), minPath: kit.Or(v.MinPath, 5), minDist: kit.Or(v.MinDist, 2),
		scale: kit.Or(v.Scale, 0.7), maxVel: kit.Or(v.MaxVel, 1.5), prefChance: kit.Or(v.PrefChance, 1)}
	var err error
	if g.cooldown, err = kit.ParseRange(v.Cooldown, kit.Range{Min: 10, Max: 20}); err != nil {
		return nil, err
	}
	if g.preferred, err = kit.ParseBlockList(v.Preferred); err != nil {
		return nil, err
	}
	if g.forbidden, err = kit.ParseBlockList(v.Forbidden); err != nil {
		return nil, err
	}
	return g, nil
}

func (g *jumpToBlock) params() map[string]float64 {
	return map[string]float64{"search_width": float64(g.width), "search_height": float64(g.height), "minimum_path_length": float64(g.minPath),
		"minimum_distance": g.minDist, "scale_factor": g.scale, "max_velocity": g.maxVel, "cooldown_min": g.cooldown.Min, "cooldown_max": g.cooldown.Max,
		"preferred_blocks_chance": g.prefChance}
}

// Mob air physics the arc is simulated with (package mob/physics): gravity
// 0.08 then vertical drag 0.98, horizontal drag 0.91 a tick in the air.
const (
	jumpGravity = 0.08
	jumpVDrag   = 0.98
	jumpHDrag   = 0.91
)

// reach simulates a jump of launch speed v at angle th and returns the
// horizontal distance covered when the mob comes back down to height dy.
func reach(v, th, dy float64) float64 {
	h, vy, x, y := v*math.Cos(th), v*math.Sin(th), 0.0, 0.0
	for i := 0; i < 200; i++ {
		x += h
		y += vy
		h *= jumpHDrag
		vy = (vy - jumpGravity) * jumpVDrag
		if vy < 0 && y <= dy {
			return x
		}
	}
	return x
}

// launch returns the launch velocity reaching dst from src on the lowest arc
// of the angles 65..85 (source: java/paper LongJumpToRandomPos) within
// max_velocity, simulating the mob's air physics.
func (g *jumpToBlock) launch(src, dst mgl64.Vec3) (mgl64.Vec3, bool) {
	d := dst.Sub(src)
	hd := math.Hypot(d[0], d[2])
	if hd < 1e-3 {
		return mgl64.Vec3{}, false
	}
	dir := mgl64.Vec3{d[0] / hd, 0, d[2] / hd}
	for _, deg := range []float64{65, 70, 75, 80, 85} {
		th := mgl64.DegToRad(deg)
		if reach(g.maxVel, th, d[1]) < hd {
			continue
		}
		lo, hi := 0.0, g.maxVel
		for i := 0; i < 30; i++ {
			mid := (lo + hi) / 2
			if reach(mid, th, d[1]) < hd {
				lo = mid
			} else {
				hi = mid
			}
		}
		return dir.Mul(hi * math.Cos(th)).Add(mgl64.Vec3{0, hi * math.Sin(th), 0}), true
	}
	return mgl64.Vec3{}, false
}

// pick finds a jump target.
func (g *jumpToBlock) pick(m *mob.Mob) bool {
	tx := m.Tx()
	base := cube.PosFromVec3(m.Position())
	wantPreferred := len(g.preferred) > 0 && m.Rand().Float64() < g.prefChance
	var best []cube.Pos
	var any []cube.Pos
	for i := 0; i < 20; i++ {
		p := base.Add(cube.Pos{m.Rand().IntN(2*g.width+1) - g.width, m.Rand().IntN(2*g.height+1) - g.height, m.Rand().IntN(2*g.width+1) - g.width})
		q, ok := kit.Ground(tx, p, g.height)
		if !ok {
			continue
		}
		under := tx.Block(q.Side(cube.FaceDown))
		if g.forbidden.Has(under) || g.forbidden.Has(tx.Block(q)) {
			continue
		}
		if _, ok := tx.Liquid(q); ok {
			continue
		}
		c := mgl64.Vec3{float64(q[0]) + 0.5, float64(q[1]), float64(q[2]) + 0.5}
		if c.Sub(m.Position()).Len() < g.minDist {
			continue
		}
		if p := m.Navigation().CreatePath(m, c); p != nil && p.Complete() && p.Len() < g.minPath {
			continue
		}
		if wantPreferred && g.preferred.Has(under) {
			best = append(best, q)
		}
		any = append(any, q)
	}
	list := any
	if len(best) > 0 {
		list = best
	}
	for _, q := range list {
		c := mgl64.Vec3{float64(q[0]) + 0.5, float64(q[1]), float64(q[2]) + 0.5}
		if v, ok := g.launch(m.Position(), c); ok {
			g.target, g.vel = q, v
			return true
		}
	}
	return false
}

// CanUse ...
func (g *jumpToBlock) CanUse(m *mob.Mob) bool {
	if !g.init {
		g.init, g.next = true, m.Ticks()+int64(kit.RollTicks(m, g.cooldown))
	}
	if m.Ticks() < g.next || !m.OnGround() || m.Physics().InWater {
		return false
	}
	if !g.pick(m) {
		g.next = m.Ticks() + 20
		return false
	}
	return true
}

// CanContinueToUse ...
func (g *jumpToBlock) CanContinueToUse(*mob.Mob) bool { return g.state != 2 && g.ticks < 200 }

// Start ...
func (g *jumpToBlock) Start(m *mob.Mob) {
	g.state, g.ticks = 0, 0
	kit.Stop(m)
	faceTowards(m, g.target.Vec3Centre())
}

// Tick ...
func (g *jumpToBlock) Tick(m *mob.Mob) {
	g.ticks++
	switch g.state {
	case 0:
		m.SetVelocity(g.vel)
		kit.Flag(m, protocol.EntityDataFlagJumpGoal, true)
		g.state = 1
	case 1:
		if g.ticks > 3 && (m.OnGround() || m.Physics().InWater) {
			g.state = 2
		}
	}
}

// Stop ...
func (g *jumpToBlock) Stop(m *mob.Mob) {
	kit.Flag(m, protocol.EntityDataFlagJumpGoal, false)
	g.next = m.Ticks() + int64(kit.RollTicks(m, g.cooldown))
	g.state = 0
}

// layEgg is minecraft:behavior.lay_egg (BDS LayEggGoal on
// BaseMoveToBlockGoal; control flags move|look): a mob with this goal (added
// when it is pregnant) walks to a target_blocks block within
// search_range/search_height that has target_materials_above_block above
// it (air by default; from below too with allow_laying_from_below), lays
// for lay_seconds there (actor flag laying_egg with use_default_animation),
// places egg_type above the block, plays lay_egg_sound and fires on_lay.
// Dragonfly has neither turtle eggs nor frog spawn: when egg_type is not a
// known block nothing is placed, the rest happens.
//
//	search_range 0, search_height 1, goal_radius 0.5, speed_multiplier 1,
//	target_blocks [minecraft:sand], egg_type minecraft:turtle_egg,
//	lay_seconds 10, lay_egg_sound lay_egg, use_default_animation true.
type layEgg struct {
	mob.GoalBase
	debugName
	xz, y           int
	radius, speed   float64
	targets         kit.BlockList
	above           []string
	fromBelow, anim bool
	layTicks        int
	egg, sound      string
	onLay           kit.Trigger

	target   cube.Pos
	reached  bool
	laying   int
	done     bool
	nextScan int64
}

func newLayEgg(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Range     *float64        `json:"search_range"`
		Height    *float64        `json:"search_height"`
		Radius    *float64        `json:"goal_radius"`
		Speed     *float64        `json:"speed_multiplier"`
		Targets   json.RawMessage `json:"target_blocks"`
		Above     []string        `json:"target_materials_above_block"`
		FromBelow bool            `json:"allow_laying_from_below"`
		Anim      *bool           `json:"use_default_animation"`
		LaySecs   *float64        `json:"lay_seconds"`
		Egg       string          `json:"egg_type"`
		Sound     *string         `json:"lay_egg_sound"`
		OnLay     json.RawMessage `json:"on_lay"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	g := &layEgg{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, debugName: "LayEggGoal",
		xz: int(kit.Or(v.Range, 0)), y: int(kit.Or(v.Height, 1)), radius: kit.Or(v.Radius, 0.5), speed: kit.Or(v.Speed, 1),
		above: v.Above, fromBelow: v.FromBelow, anim: kit.Or(v.Anim, true), layTicks: kit.Ticks(kit.Or(v.LaySecs, 10)),
		egg: v.Egg, sound: kit.Or(v.Sound, "lay_egg")}
	if g.egg == "" {
		g.egg = "minecraft:turtle_egg"
	}
	var err error
	if g.targets, err = kit.ParseBlockList(v.Targets); err != nil {
		return nil, err
	}
	if len(g.targets) == 0 {
		g.targets = kit.BlockList{"minecraft:sand"}
	}
	if len(g.above) == 0 {
		g.above = []string{"air"}
	}
	if g.onLay, err = kit.ParseTrigger(v.OnLay); err != nil {
		return nil, err
	}
	return g, nil
}

func (g *layEgg) params() map[string]float64 {
	return map[string]float64{"search_range": float64(g.xz), "search_height": float64(g.y), "goal_radius": g.radius, "speed_multiplier": g.speed,
		"lay_ticks": float64(g.layTicks), "use_default_animation": b2f(g.anim), "allow_laying_from_below": b2f(g.fromBelow)}
}

// materialOK reports whether the block at p is of the material name
// ("air", "water", ...).
func materialOK(tx *world.Tx, p cube.Pos, mat string) bool {
	b := tx.Block(p)
	switch mat {
	case "air", "":
		return air(tx, p)
	case "water":
		_, ok := b.(block.Water)
		return ok
	}
	return kit.BlockName(b) == "minecraft:"+mat
}

// valid is LayEggGoal::isValidTarget.
func (g *layEgg) valid(tx *world.Tx, p cube.Pos, b world.Block) bool {
	if !g.targets.Has(b) {
		if w, ok := b.(block.Water); !ok || !g.targets.HasName("minecraft:water") || !w.Still {
			return false
		}
	}
	return materialOK(tx, p.Side(cube.FaceUp), g.above[0])
}

// CanUse ...
func (g *layEgg) CanUse(m *mob.Mob) bool {
	if m.Ticks() < g.nextScan {
		return false
	}
	g.nextScan = m.Ticks() + 20
	tx := m.Tx()
	p, ok := kit.FindBlock(m, g.xz, g.y, "nearest", func(p cube.Pos, b world.Block) bool { return g.valid(tx, p, b) })
	g.target = p
	return ok
}

// CanContinueToUse ...
func (g *layEgg) CanContinueToUse(m *mob.Mob) bool {
	return !g.done && g.valid(m.Tx(), g.target, m.Tx().Block(g.target)) && (g.reached || !m.Navigation().Done())
}

// Start ...
func (g *layEgg) Start(m *mob.Mob) {
	g.reached, g.done, g.laying = false, false, 0
	kit.MoveToBlock(m, g.target.Side(cube.FaceUp), g.speed)
}

// Tick ...
func (g *layEgg) Tick(m *mob.Mob) {
	c := g.target.Vec3Centre()
	if !g.reached {
		if kit.HDistSq(m.Position(), c) <= (g.radius+0.5)*(g.radius+0.5) && math.Abs(m.Position()[1]-float64(g.target[1]+1)) <= 1.5 {
			g.reached = true
			kit.Stop(m)
			if g.anim {
				kit.Flag(m, protocol.EntityDataFlagLayingEgg, true)
			}
		} else if m.Navigation().Done() {
			kit.MoveToBlock(m, g.target.Side(cube.FaceUp), g.speed)
		}
		return
	}
	kit.Stop(m)
	if g.laying++; g.laying < g.layTicks {
		return
	}
	tx := m.Tx()
	if b, ok := world.BlockByName(cmisc.NormaliseBlockName(g.egg), nil); ok {
		tx.SetBlock(g.target.Side(cube.FaceUp), b, nil)
	}
	kit.Sound(m, g.sound)
	g.done = true
	g.onLay.Fire(m, nil)
}

// Stop ...
func (g *layEgg) Stop(m *mob.Mob) {
	kit.Flag(m, protocol.EntityDataFlagLayingEgg, false)
	kit.Stop(m)
}
