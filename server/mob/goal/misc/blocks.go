//go:build crossfly_full

package misc

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/kit"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// moveToBlock is the BDS BaseMoveToBlockGoal/MoveToBlockGoal shared by
// minecraft:behavior.move_to_block (bees to flowers and hives) and
// stomp_turtle_egg (StompEggGoal): every tick_interval ticks, with
// start_chance, it looks for one of target_blocks (passing
// target_block_filters, the block as "block") within search_range and
// search_height, nearest or random (target_selection_method), walks to it
// at speed_multiplier until within goal_radius of the block plus
// target_offset, fires on_reach, stays stay_duration seconds and fires
// on_stay_completed.
//
//	tick_interval 20, start_chance 1, search_range 0, search_height 1,
//	goal_radius 0.5, stay_duration 0, speed_multiplier 1
type moveToBlock struct {
	mob.GoalBase
	interval        int
	chance          float64
	rangeXZ, rangeY int
	radius          float64
	stay            int
	speed           float64
	method          string
	offset          mgl64.Vec3
	blocks          kit.BlockList
	filter          mob.Filter
	onReach, onStay []kit.Trigger

	// reached is called once on reaching the block (stomp_turtle_egg breaks
	// the egg); it returns false when the goal should end at once.
	reached func(m *mob.Mob, p cube.Pos) bool

	target            cube.Pos
	reachedAt, stayed int
	hasReached        bool
	nextScan          int64
	repath            int
}

type rawMoveToBlock struct {
	TickInterval    *int            `json:"tick_interval"`
	Interval        *int            `json:"interval"`
	StartChance     *float64        `json:"start_chance"`
	SearchRange     *int            `json:"search_range"`
	SearchHeight    *int            `json:"search_height"`
	GoalRadius      *float64        `json:"goal_radius"`
	StayDuration    float64         `json:"stay_duration"`
	SpeedMultiplier *float64        `json:"speed_multiplier"`
	Method          string          `json:"target_selection_method"`
	TargetOffset    []float64       `json:"target_offset"`
	TargetBlocks    json.RawMessage `json:"target_blocks"`
	TargetFilters   json.RawMessage `json:"target_block_filters"`
	OnReach         json.RawMessage `json:"on_reach"`
	OnStayCompleted json.RawMessage `json:"on_stay_completed"`
}

func parseMoveToBlock(spec mob.GoalSpec, v rawMoveToBlock, defRange, defHeight int, defRadius float64) (*moveToBlock, error) {
	g := &moveToBlock{
		GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)},
		interval: kit.Or(v.TickInterval, kit.Or(v.Interval, 20)), chance: kit.Or(v.StartChance, 1),
		rangeXZ: kit.Or(v.SearchRange, defRange), rangeY: kit.Or(v.SearchHeight, defHeight),
		radius: kit.Or(v.GoalRadius, defRadius), stay: kit.Ticks(v.StayDuration), speed: kit.Or(v.SpeedMultiplier, 1),
		method: v.Method,
	}
	if len(v.TargetOffset) == 3 {
		g.offset = mgl64.Vec3{v.TargetOffset[0], v.TargetOffset[1], v.TargetOffset[2]}
	}
	var err error
	if g.blocks, err = kit.ParseBlockList(v.TargetBlocks); err != nil {
		return nil, err
	}
	if g.filter, err = kit.Filter(v.TargetFilters); err != nil {
		return nil, err
	}
	if g.onReach, err = kit.ParseTriggers(v.OnReach); err != nil {
		return nil, err
	}
	if g.onStay, err = kit.ParseTriggers(v.OnStayCompleted); err != nil {
		return nil, err
	}
	return g, nil
}

func newMoveToBlock(spec mob.GoalSpec) (mob.Goal, error) {
	var v rawMoveToBlock
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	return parseMoveToBlock(spec, v, 0, 1, 0.5)
}

func (g *moveToBlock) matches(m *mob.Mob, p cube.Pos, b world.Block) bool {
	if !g.blocks.Has(b) {
		return false
	}
	return g.filter.Test(kit.BlockCtx(m, p, b))
}

// CanUse ...
func (g *moveToBlock) CanUse(m *mob.Mob) bool {
	if len(g.blocks) == 0 || m.Ticks() < g.nextScan {
		return false
	}
	g.nextScan = m.Ticks() + int64(max(g.interval, 1))
	if g.chance < 1 && m.Rand().Float64() >= g.chance {
		return false
	}
	p, ok := kit.FindBlockIn(m, g.rangeXZ, g.rangeY, g.method, g.blocks, func(p cube.Pos, b world.Block) bool { return g.matches(m, p, b) })
	if !ok {
		return false
	}
	g.target = p
	return true
}

func (g *moveToBlock) goalPos() mgl64.Vec3 {
	return mgl64.Vec3{float64(g.target[0]) + 0.5, float64(g.target[1]), float64(g.target[2]) + 0.5}.Add(g.offset)
}

// CanContinueToUse ...
func (g *moveToBlock) CanContinueToUse(m *mob.Mob) bool {
	if !g.matches(m, g.target, m.Tx().Block(g.target)) {
		return false
	}
	if g.hasReached {
		return g.stayed < g.stay
	}
	return g.repath < 1200
}

// Start ...
func (g *moveToBlock) Start(m *mob.Mob) {
	g.hasReached, g.stayed, g.repath = false, 0, 0
	kit.MoveTo(m, g.goalPos(), g.speed)
}

// Tick ...
func (g *moveToBlock) Tick(m *mob.Mob) {
	goal := g.goalPos()
	kit.LookAt(m, goal)
	if !g.hasReached {
		g.repath++
		// BaseMoveToBlockGoal::isValidTarget distance: within goal_radius of
		// the block centre (feet height ignored within a block).
		d := m.Position().Sub(goal)
		if math.Hypot(d[0], d[2]) <= g.radius+0.5 && math.Abs(d[1]) <= 1.5 {
			g.hasReached = true
			kit.Stop(m)
			ctx := kit.BlockCtx(m, g.target, m.Tx().Block(g.target))
			kit.FireAll(m, g.onReach, ctx)
			if g.reached != nil && !g.reached(m, g.target) {
				g.stayed = g.stay
			}
			return
		}
		if g.repath%40 == 0 || m.Navigation().Done() {
			kit.MoveTo(m, goal, g.speed)
		}
		return
	}
	if g.stayed++; g.stayed == g.stay {
		kit.FireAll(m, g.onStay, kit.BlockCtx(m, g.target, m.Tx().Block(g.target)))
	}
}

// Stop ...
func (g *moveToBlock) Stop(m *mob.Mob) {
	kit.Stop(m)
	g.hasReached = false
}

// AppendDebugInfo ...
func (g *moveToBlock) AppendDebugInfo(b *strings.Builder) { b.WriteString("MoveToBlock") }

// newStompTurtleEgg is minecraft:behavior.stomp_turtle_egg (zombies, BDS
// StompEggGoal on BaseMoveToBlockGoal): turtle eggs within search_range
// (10) / search_height (1), interval 20, goal_radius 0.5; on reaching one
// the mob jumps on it (source: java/paper ZombieAttackTurtleEggGoal: 60
// ticks of stomping, the egg cracks and breaks). Dragonfly has no turtle
// egg block, so the goal only runs where one is (world.BlockByName).
func newStompTurtleEgg(spec mob.GoalSpec) (mob.Goal, error) {
	var v rawMoveToBlock
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	g, err := parseMoveToBlock(spec, v, 10, 1, 0.5)
	if err != nil {
		return nil, err
	}
	g.blocks = kit.BlockList{"minecraft:turtle_egg"}
	g.stay = 60
	g.reached = func(m *mob.Mob, p cube.Pos) bool { return true }
	return &stompEgg{moveToBlock: g}, nil
}

type stompEgg struct{ *moveToBlock }

// Tick stomps once reached.
func (g *stompEgg) Tick(m *mob.Mob) {
	g.moveToBlock.Tick(m)
	if !g.hasReached {
		return
	}
	if g.stayed%10 == 0 {
		m.JumpControl().Jump()
		kit.Sound(m, "crack")
	}
	if g.stayed >= g.stay {
		m.Tx().SetBlock(g.target, block.Air{}, nil)
		kit.Sound(m, "break")
	}
}

// AppendDebugInfo ...
func (g *stompEgg) AppendDebugInfo(b *strings.Builder) { b.WriteString("StompEgg") }

// avoidBlock is minecraft:behavior.avoid_block (hoglins avoid warped fungi,
// portals and respawn anchors; piglins soul fire; BDS AvoidBlockGoal):
// every tick_interval (1) ticks it looks for target_blocks within
// search_range (0) / search_height (0); the nearest or a random one
// (target_selection_method) is fled from to a random spot 16/7 blocks away
// on the far side, walking at walk_speed_modifier (1), at
// sprint_speed_modifier (1) while within 7 blocks of the block.
// avoid_block_sound plays every sound_interval (3..8 s); on_escape fires
// when the mob gets away.
type avoidBlock struct {
	mob.GoalBase
	interval        int
	rangeXZ, rangeY int
	walk, sprint    float64
	method          string
	blocks          kit.BlockList
	sound           string
	soundInterval   kit.Range
	onEscape        []kit.Trigger

	from      mgl64.Vec3
	nextScan  int64
	nextSound int64
}

func newAvoidBlock(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		TickInterval  *int            `json:"tick_interval"`
		SearchRange   int             `json:"search_range"`
		SearchHeight  int             `json:"search_height"`
		Walk          *float64        `json:"walk_speed_modifier"`
		Sprint        *float64        `json:"sprint_speed_modifier"`
		Method        string          `json:"target_selection_method"`
		Blocks        json.RawMessage `json:"target_blocks"`
		Sound         string          `json:"avoid_block_sound"`
		SoundInterval json.RawMessage `json:"sound_interval"`
		OnEscape      json.RawMessage `json:"on_escape"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	g := &avoidBlock{
		GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)},
		interval: kit.Or(v.TickInterval, 1), rangeXZ: v.SearchRange, rangeY: v.SearchHeight,
		walk: kit.Or(v.Walk, 1), sprint: kit.Or(v.Sprint, 1), method: v.Method, sound: v.Sound,
	}
	var err error
	if g.blocks, err = kit.ParseBlockList(v.Blocks); err != nil {
		return nil, err
	}
	if g.soundInterval, err = kit.ParseRange(v.SoundInterval, kit.Range{Min: 3, Max: 8}); err != nil {
		return nil, err
	}
	if g.onEscape, err = kit.ParseTriggers(v.OnEscape); err != nil {
		return nil, err
	}
	return g, nil
}

// CanUse ...
func (g *avoidBlock) CanUse(m *mob.Mob) bool {
	if len(g.blocks) == 0 || m.Ticks() < g.nextScan {
		return false
	}
	g.nextScan = m.Ticks() + int64(max(g.interval, 1))
	p, ok := kit.FindBlockIn(m, g.rangeXZ, g.rangeY, g.method, g.blocks, func(_ cube.Pos, b world.Block) bool { return g.blocks.Has(b) })
	if !ok {
		return false
	}
	g.from = p.Vec3Centre()
	dest, ok := kit.RandomPosAway(m, 16, 7, g.from)
	if !ok || kit.DistSq(dest, g.from) < kit.DistSq(m.Position(), g.from) {
		return false
	}
	return kit.MoveTo(m, dest, g.walk)
}

// CanContinueToUse ...
func (g *avoidBlock) CanContinueToUse(m *mob.Mob) bool { return !m.Navigation().Done() }

// Start ...
func (g *avoidBlock) Start(m *mob.Mob) {
	g.nextSound = m.Ticks()
}

// Tick ...
func (g *avoidBlock) Tick(m *mob.Mob) {
	if kit.DistSq(m.Position(), g.from) < 49 {
		m.Navigation().SetSpeed(g.sprint)
	} else {
		m.Navigation().SetSpeed(g.walk)
	}
	if g.sound != "" && m.Ticks() >= g.nextSound {
		kit.Sound(m, g.sound)
		g.nextSound = m.Ticks() + int64(kit.RollTicks(m, g.soundInterval))
	}
}

// Stop ...
func (g *avoidBlock) Stop(m *mob.Mob) {
	kit.Stop(m)
	kit.FireAll(m, g.onEscape, nil)
}

// AppendDebugInfo ...
func (g *avoidBlock) AppendDebugInfo(b *strings.Builder) { b.WriteString("AvoidBlock") }

// moveToRandomBlock is minecraft:behavior.move_to_random_block (patrol
// captains, BDS MoveToRandomBlockGoal): picks a random block within
// block_distance (16) and walks towards it at speed_multiplier (1) in
// steps the pathfinder can reach, until within within_radius (0) of it.
type moveToRandomBlock struct {
	mob.GoalBase
	distance, radius, speed float64
	goal                    mgl64.Vec3
	hasGoal                 bool
}

func newMoveToRandomBlock(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		BlockDistance   *float64 `json:"block_distance"`
		WithinRadius    float64  `json:"within_radius"`
		SpeedMultiplier *float64 `json:"speed_multiplier"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	return &moveToRandomBlock{
		GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)},
		distance: kit.Or(v.BlockDistance, 16), radius: v.WithinRadius, speed: kit.Or(v.SpeedMultiplier, 1),
	}, nil
}

// CanUse ...
func (g *moveToRandomBlock) CanUse(m *mob.Mob) bool {
	if _, ok := m.Target(); ok {
		return false
	}
	if !g.hasGoal || kit.HDistSq(m.Position(), g.goal) <= math.Max(g.radius, 1)*math.Max(g.radius, 1) {
		a := m.Rand().Float64() * 2 * math.Pi
		r := g.distance * (0.5 + m.Rand().Float64()*0.5)
		g.goal = m.Position().Add(mgl64.Vec3{math.Cos(a) * r, 0, math.Sin(a) * r})
		g.hasGoal = true
	}
	return m.Navigation().Done()
}

// CanContinueToUse ...
func (g *moveToRandomBlock) CanContinueToUse(m *mob.Mob) bool {
	_, hasTarget := m.Target()
	return !hasTarget && !m.Navigation().Done()
}

// Start walks a step of at most 10 blocks towards the goal (the path
// search is bounded; source: java/paper PatrollingMonster.LongDistancePatrolGoal).
func (g *moveToRandomBlock) Start(m *mob.Mob) {
	d := g.goal.Sub(m.Position())
	d[1] = 0
	step := d
	if d.Len() > 10 {
		step = d.Normalize().Mul(10)
	}
	p := cube.PosFromVec3(m.Position().Add(step))
	if gp, ok := kit.Ground(m.Tx(), p, 8); ok {
		p = gp
	}
	if !kit.MoveToBlock(m, p, g.speed) {
		g.hasGoal = false
	}
}

// Stop ...
func (g *moveToRandomBlock) Stop(m *mob.Mob) { kit.Stop(m) }

// AppendDebugInfo ...
func (g *moveToRandomBlock) AppendDebugInfo(b *strings.Builder) { b.WriteString("MoveToRandomBlock") }
