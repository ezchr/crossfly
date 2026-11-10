//go:build crossfly_full

package animal

import (
	"encoding/json"
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/mob/goal/combat"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/kit"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// stalkAndPounce is minecraft:behavior.stalk_and_pounce_on_target (BDS
// StalkAndPounceOnTargetGoal; defaults from LegacyGoalDefinition::parse):
// the fox crouches towards its target at stalk_speed, waits interest_time
// once within pounce_max_dist, leaps leap_dist forward and leap_height up,
// and on landing hits the target within strike_dist; landing in a
// stuck_blocks block (snow) sticks it for stuck_time.
//
//	stalk_speed 1.2, max_stalk_dist 10, leap_height 0.9, leap_dist 0.8,
//	pounce_max_dist 5, interest_time 2 s, stuck_time 2 s, strike_dist 2,
//	set_persistent false.
type stalkAndPounce struct {
	mob.GoalBase
	debugName
	stalkSpeed, maxStalk, leapH, leapD, pounceMax, strike float64
	interest, stuck                                       int
	stuckBlocks                                           mob.Filter
	hasStuckBlocks, setPersistent                         bool

	state  int // stalkState...
	timer  int
	target *world.EntityHandle
}

const (
	stalkState = iota
	interestState
	leapState
	stuckState
	doneState
)

func newStalkAndPounce(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		StalkSpeed    *float64        `json:"stalk_speed"`
		MaxStalkDist  *float64        `json:"max_stalk_dist"`
		LeapHeight    *float64        `json:"leap_height"`
		LeapDist      *float64        `json:"leap_dist"`
		PounceMaxDist *float64        `json:"pounce_max_dist"`
		InterestTime  *float64        `json:"interest_time"`
		StuckTime     *float64        `json:"stuck_time"`
		StrikeDist    *float64        `json:"strike_dist"`
		StuckBlocks   json.RawMessage `json:"stuck_blocks"`
		SetPersistent bool            `json:"set_persistent"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	g := &stalkAndPounce{
		GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook | mob.FlagJump)}, debugName: "StalkAndPounceOnTargetGoal",
		stalkSpeed: kit.Or(v.StalkSpeed, 1.2), maxStalk: kit.Or(v.MaxStalkDist, 10), leapH: kit.Or(v.LeapHeight, 0.9),
		leapD: kit.Or(v.LeapDist, 0.8), pounceMax: kit.Or(v.PounceMaxDist, 5), strike: kit.Or(v.StrikeDist, 2),
		interest: kit.Ticks(kit.Or(v.InterestTime, 2)), stuck: kit.Ticks(kit.Or(v.StuckTime, 2)), setPersistent: v.SetPersistent,
	}
	f, err := kit.Filter(v.StuckBlocks)
	if err != nil {
		return nil, err
	}
	g.stuckBlocks, g.hasStuckBlocks = f, !kit.IsNull(v.StuckBlocks)
	return g, nil
}

func (g *stalkAndPounce) params() map[string]float64 {
	return map[string]float64{"stalk_speed": g.stalkSpeed, "max_stalk_dist": g.maxStalk, "leap_height": g.leapH, "leap_dist": g.leapD,
		"pounce_max_dist": g.pounceMax, "strike_dist": g.strike, "interest_ticks": float64(g.interest), "stuck_ticks": float64(g.stuck)}
}

func (g *stalkAndPounce) targetOf(m *mob.Mob) (world.Entity, bool) {
	t, ok := m.Target()
	if !ok || !kit.Alive(t) {
		return nil, false
	}
	return t, true
}

// CanUse ...
func (g *stalkAndPounce) CanUse(m *mob.Mob) bool {
	t, ok := g.targetOf(m)
	if !ok || !m.OnGround() || m.Physics().InWater {
		return false
	}
	return kit.DistSq(t.Position(), m.Position()) <= g.maxStalk*g.maxStalk
}

// CanContinueToUse ...
func (g *stalkAndPounce) CanContinueToUse(m *mob.Mob) bool {
	if g.state == doneState {
		return false
	}
	if g.state == leapState || g.state == stuckState {
		return true
	}
	t, ok := g.targetOf(m)
	return ok && t.H() == g.target && kit.DistSq(t.Position(), m.Position()) <= g.maxStalk*g.maxStalk
}

// Start ...
func (g *stalkAndPounce) Start(m *mob.Mob) {
	t, _ := g.targetOf(m)
	g.target, g.state, g.timer = t.H(), stalkState, 0
	if g.setPersistent {
		m.SetPersistent(true)
	}
	kit.Flag(m, protocol.EntityDataFlagStalking, true)
	m.Navigation().MoveToEntity(m, t, g.stalkSpeed)
}

// Stop ...
func (g *stalkAndPounce) Stop(m *mob.Mob) {
	kit.Flag(m, protocol.EntityDataFlagStalking, false)
	kit.Flag(m, protocol.EntityDataFlagInterested, false)
	g.target, g.state = nil, stalkState
	kit.Stop(m)
}

// Tick ...
func (g *stalkAndPounce) Tick(m *mob.Mob) {
	t, ok := g.target.Entity(m.Tx())
	switch g.state {
	case stalkState:
		if !ok {
			g.state = doneState
			return
		}
		kit.LookAtEntity(m, t)
		if g.timer++; g.timer%10 == 0 {
			m.Navigation().MoveToEntity(m, t, g.stalkSpeed)
		}
		if m.OnGround() && kit.DistSq(t.Position(), m.Position()) <= g.pounceMax*g.pounceMax {
			kit.Stop(m)
			g.state, g.timer = interestState, g.interest
			kit.Flag(m, protocol.EntityDataFlagInterested, true)
		}
	case interestState:
		if !ok {
			g.state = doneState
			return
		}
		kit.LookAtEntity(m, t)
		if g.timer--; g.timer > 0 {
			return
		}
		kit.Flag(m, protocol.EntityDataFlagInterested, false)
		kit.Flag(m, protocol.EntityDataFlagStalking, false)
		faceTowards(m, t.Position())
		d := t.Position().Sub(m.Position())
		d[1] = 0
		if d.Len() > 1e-4 {
			d = d.Normalize()
		}
		m.SetVelocity(mgl64.Vec3{d[0] * g.leapD, g.leapH, d[2] * g.leapD})
		g.state, g.timer = leapState, 0
	case leapState:
		if g.timer++; g.timer < 3 || !m.OnGround() {
			return
		}
		feet := blockAt(m.Position())
		if g.hasStuckBlocks && g.stuckBlocks.Test(kit.BlockCtx(m, feet, m.Tx().Block(feet))) {
			g.state, g.timer = stuckState, g.stuck
			kit.Stop(m)
			return
		}
		if ok && kit.DistSq(t.Position(), m.Position()) <= g.strike*g.strike {
			combat.DoHurtTarget(m, t)
		}
		g.state = doneState
	case stuckState:
		m.StopMovement()
		if g.timer--; g.timer <= 0 {
			g.state = doneState
		}
	}
}

// nap is minecraft:behavior.nap (BDS NapGoal): the fox sleeps when
// can_nap_filters pass and no living entity within mob_detect_dist
// (mob_detect_height up/down) fails wake_mob_exceptions (the entity as
// "other"); creative and spectator players are ignored. After waking it
// waits cooldown_min..cooldown_max seconds.
//
//	cooldown_min 0, cooldown_max 0, mob_detect_dist 6, mob_detect_height 6.
type nap struct {
	mob.GoalBase
	debugName
	cdMin, cdMax         float64
	detectDist, detectHt float64
	canNap, exceptions   mob.Filter
	next                 int64
}

func newNap(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		CooldownMin     float64         `json:"cooldown_min"`
		CooldownMax     float64         `json:"cooldown_max"`
		MobDetectDist   *float64        `json:"mob_detect_dist"`
		MobDetectHeight *float64        `json:"mob_detect_height"`
		CanNap          json.RawMessage `json:"can_nap_filters"`
		Exceptions      json.RawMessage `json:"wake_mob_exceptions"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	g := &nap{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook | mob.FlagJump)}, debugName: "NapGoal",
		cdMin: v.CooldownMin, cdMax: math.Max(v.CooldownMin, v.CooldownMax), detectDist: kit.Or(v.MobDetectDist, 6), detectHt: kit.Or(v.MobDetectHeight, 6)}
	var err error
	if g.canNap, err = kit.Filter(v.CanNap); err != nil {
		return nil, err
	}
	if g.exceptions, err = kit.Filter(v.Exceptions); err != nil {
		return nil, err
	}
	if kit.IsNull(v.Exceptions) {
		g.exceptions = mob.FilterFunc(func(*mob.FilterContext) bool { return false })
	}
	return g, nil
}

func (g *nap) params() map[string]float64 {
	return map[string]float64{"cooldown_min": g.cdMin, "cooldown_max": g.cdMax, "mob_detect_dist": g.detectDist, "mob_detect_height": g.detectHt}
}

// woken reports whether an entity near m keeps it awake.
func (g *nap) woken(m *mob.Mob) bool {
	for _, e := range kit.Around(m, g.detectDist, g.detectHt) {
		if !kit.Alive(e) {
			continue
		}
		if _, ok := e.(interface{ Dead() bool }); !ok {
			continue // not living (items, arrows)
		}
		if kit.IsPlayer(e) && (kit.Creative(e) || !kit.VisiblePlayer(e)) {
			continue
		}
		if !g.exceptions.Test(kit.OtherCtx(m, e)) {
			return true
		}
	}
	return false
}

// CanUse ...
func (g *nap) CanUse(m *mob.Mob) bool {
	return m.Ticks() >= g.next && g.canNap.Test(m.Ctx()) && !g.woken(m)
}

// CanContinueToUse ...
func (g *nap) CanContinueToUse(m *mob.Mob) bool { return g.canNap.Test(m.Ctx()) && !g.woken(m) }

// Start ...
func (g *nap) Start(m *mob.Mob) {
	m.StopMovement()
	kit.Flag(m, protocol.EntityDataFlagSleeping, true)
}

// Tick keeps the fox still.
func (g *nap) Tick(m *mob.Mob) { kit.Stop(m) }

// Stop ...
func (g *nap) Stop(m *mob.Mob) {
	kit.Flag(m, protocol.EntityDataFlagSleeping, false)
	g.next = m.Ticks() + int64(kit.RollTicks(m, kit.Range{Min: g.cdMin, Max: g.cdMax}))
}

// findCover is minecraft:behavior.find_cover (BDS FindCoverGoal): a mob
// under open sky walks to a shaded spot within 10 blocks (combat.FindShade)
// at speed_multiplier, then waits cooldown_time seconds before it looks
// again.
//
//	speed_multiplier 1, cooldown_time 0.
type findCover struct {
	mob.GoalBase
	debugName
	speed    float64
	cooldown int
	next     int64
	wanted   mgl64.Vec3
}

func newFindCover(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Speed    *float64 `json:"speed_multiplier"`
		Cooldown float64  `json:"cooldown_time"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	return &findCover{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)}, debugName: "FindCoverGoal",
		speed: kit.Or(v.Speed, 1), cooldown: kit.Ticks(v.Cooldown)}, nil
}

func (g *findCover) params() map[string]float64 {
	return map[string]float64{"speed_multiplier": g.speed, "cooldown_ticks": float64(g.cooldown)}
}

// CanUse ...
func (g *findCover) CanUse(m *mob.Mob) bool {
	if m.Ticks() < g.next {
		return false
	}
	b := m.BBox()
	if !sense.SkyVisible(m.Tx(), cube.Pos{int(math.Floor(m.Pos()[0])), int(math.Floor(b.Min()[1])), int(math.Floor(m.Pos()[2]))}) {
		return false
	}
	p, ok := combat.FindShade(m)
	g.wanted = p
	return ok
}

// CanContinueToUse ...
func (g *findCover) CanContinueToUse(m *mob.Mob) bool { return !m.Navigation().Done() }

// Start ...
func (g *findCover) Start(m *mob.Mob) { kit.MoveTo(m, g.wanted, g.speed) }

// Stop ...
func (g *findCover) Stop(m *mob.Mob) { g.next = m.Ticks() + int64(g.cooldown) }

// eatCarriedItem is minecraft:behavior.eat_carried_item (BDS
// EatCarriedItemGoal): food held in the main hand for delay_before_eating
// seconds (default 0) is eaten: the hand empties, the "eat" sound plays and
// on_eat fires. source: java/paper Fox.aiStep (ticksSinceEaten 560..600).
type eatCarriedItem struct {
	mob.GoalBase
	debugName
	delay int
	onEat kit.Trigger
	held  int
}

func newEatCarriedItem(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Delay float64         `json:"delay_before_eating"`
		OnEat json.RawMessage `json:"on_eat"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	t, err := kit.ParseTrigger(v.OnEat)
	if err != nil {
		return nil, err
	}
	return &eatCarriedItem{GoalBase: mob.GoalBase{Control: spec.FlagsOr(0)}, debugName: "EatCarriedItemGoal",
		delay: int(float32(v.Delay) * 20), onEat: t}, nil
}

func (g *eatCarriedItem) params() map[string]float64 {
	return map[string]float64{"delay_ticks": float64(g.delay)}
}

func edible(s item.Stack) bool {
	if s.Empty() {
		return false
	}
	_, ok := s.Item().(item.Consumable)
	return ok
}

// CanUse counts the ticks the food was held.
func (g *eatCarriedItem) CanUse(m *mob.Mob) bool {
	main, _ := m.HeldItems()
	if !edible(main) {
		g.held = 0
		return false
	}
	g.held++
	return g.held > g.delay
}

// CanContinueToUse ...
func (*eatCarriedItem) CanContinueToUse(*mob.Mob) bool { return false }

// Start eats the item.
func (g *eatCarriedItem) Start(m *mob.Mob) {
	main, off := m.HeldItems()
	m.SetHeldItems(main.Grow(-1), off)
	kit.Sound(m, "eat")
	g.held = 0
	g.onEat.Fire(m, nil)
}

// randomLookAroundAndSit is minecraft:behavior.random_look_around_and_sit
// (BDS RandomLookAroundAndSitGoal): with probability per tick a mob that
// stands still on the ground sits down and looks around min..max_look_count
// times, min..max_look_time ticks each, within the horizontal view angles of
// its body; then it stands and waits random_look_around_cooldown seconds.
//
//	min_look_count 1, max_look_count 2, min_look_time 20, max_look_time 40,
//	min/max_angle_of_view_horizontal -180/180, probability 0.02,
//	random_look_around_cooldown 0, continue_if_leashed false.
type randomLookAroundAndSit struct {
	mob.GoalBase
	debugName
	minCount, maxCount int
	minTime, maxTime   int
	minAngle, maxAngle float64
	prob               float64
	cooldown           int

	looks, lookTicks int
	lookAt           mgl64.Vec3
	next             int64
}

func newRandomLookAroundAndSit(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		MinCount *int     `json:"min_look_count"`
		MaxCount *int     `json:"max_look_count"`
		MinTime  *int     `json:"min_look_time"`
		MaxTime  *int     `json:"max_look_time"`
		MinAngle *float64 `json:"min_angle_of_view_horizontal"`
		MaxAngle *float64 `json:"max_angle_of_view_horizontal"`
		Prob     *float64 `json:"probability"`
		Cooldown float64  `json:"random_look_around_cooldown"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	g := &randomLookAroundAndSit{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, debugName: "RandomLookAroundAndSitGoal",
		minCount: kit.Or(v.MinCount, 1), maxCount: kit.Or(v.MaxCount, 2), minTime: kit.Or(v.MinTime, 20), maxTime: kit.Or(v.MaxTime, 40),
		minAngle: kit.Or(v.MinAngle, -180), maxAngle: kit.Or(v.MaxAngle, 180), prob: kit.Or(v.Prob, 0.02), cooldown: kit.Ticks(v.Cooldown)}
	g.maxCount, g.maxTime = max(g.maxCount, g.minCount), max(g.maxTime, g.minTime)
	return g, nil
}

func (g *randomLookAroundAndSit) params() map[string]float64 {
	return map[string]float64{"min_look_count": float64(g.minCount), "max_look_count": float64(g.maxCount), "min_look_time": float64(g.minTime),
		"max_look_time": float64(g.maxTime), "min_angle": g.minAngle, "max_angle": g.maxAngle, "probability": g.prob, "cooldown_ticks": float64(g.cooldown)}
}

// CanUse ...
func (g *randomLookAroundAndSit) CanUse(m *mob.Mob) bool {
	if m.Ticks() < g.next || !m.OnGround() || m.Physics().InWater || !m.Navigation().Done() {
		return false
	}
	return m.Rand().Float64() < g.prob
}

// CanContinueToUse ...
func (g *randomLookAroundAndSit) CanContinueToUse(m *mob.Mob) bool {
	return g.looks > 0 || g.lookTicks > 0
}

// Start ...
func (g *randomLookAroundAndSit) Start(m *mob.Mob) {
	kit.Stop(m)
	sit(m, true)
	g.looks = g.minCount + m.Rand().IntN(g.maxCount-g.minCount+1)
	g.lookTicks = 0
}

func (g *randomLookAroundAndSit) pick(m *mob.Mob) {
	yaw := m.BodyYaw() + g.minAngle + m.Rand().Float64()*(g.maxAngle-g.minAngle)
	r := mgl64.DegToRad(yaw)
	g.lookAt = m.EyePosition().Add(mgl64.Vec3{-math.Sin(r) * 2, 0, math.Cos(r) * 2})
	g.lookTicks = g.minTime + m.Rand().IntN(g.maxTime-g.minTime+1)
	g.looks--
}

// Tick ...
func (g *randomLookAroundAndSit) Tick(m *mob.Mob) {
	if g.lookTicks <= 0 && g.looks > 0 {
		g.pick(m)
	}
	g.lookTicks--
	kit.LookAt(m, g.lookAt)
}

// Stop ...
func (g *randomLookAroundAndSit) Stop(m *mob.Mob) {
	sit(m, false)
	g.looks, g.lookTicks = 0, 0
	g.next = m.Ticks() + int64(g.cooldown)
}

// raidGarden is minecraft:behavior.raid_garden (BDS RaidGardenGoal on
// BaseMoveToBlockGoal): the mob walks to a block of "blocks" (crops only
// when fully grown) within search_range/search_height, waits
// initial_eat_delay there and eats it: a crop loses a growth stage (an
// unripe one is destroyed, source: java/paper Rabbit.RaidGardenGoal). After
// max_to_eat bites (0: no limit) it is full for full_delay seconds; between
// bites it waits eat_delay seconds.
//
//	search_range 0, search_height 1, goal_radius 0.5, speed_multiplier 1,
//	max_to_eat 6, initial_eat_delay 0, eat_delay 2, full_delay 100.
type raidGarden struct {
	mob.GoalBase
	debugName
	blocks                        kit.BlockList
	xz, y                         int
	radius, speed                 float64
	maxToEat, initDelay, eatDelay int
	fullDelay                     int
	eaten, wait                   int
	target                        cube.Pos
	next, fullUntil               int64
	reached, done                 bool
}

func newRaidGarden(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Blocks    json.RawMessage `json:"blocks"`
		Range     *float64        `json:"search_range"`
		Height    *float64        `json:"search_height"`
		Radius    *float64        `json:"goal_radius"`
		Speed     *float64        `json:"speed_multiplier"`
		MaxToEat  *int            `json:"max_to_eat"`
		InitDelay float64         `json:"initial_eat_delay"`
		EatDelay  *float64        `json:"eat_delay"`
		FullDelay *float64        `json:"full_delay"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	bl, err := kit.ParseBlockList(v.Blocks)
	if err != nil {
		return nil, err
	}
	return &raidGarden{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, debugName: "RaidGardenGoal",
		blocks: bl, xz: int(kit.Or(v.Range, 0)), y: int(kit.Or(v.Height, 1)), radius: kit.Or(v.Radius, 0.5), speed: kit.Or(v.Speed, 1),
		maxToEat: kit.Or(v.MaxToEat, 6), initDelay: kit.Ticks(v.InitDelay), eatDelay: kit.Ticks(kit.Or(v.EatDelay, 2)),
		fullDelay: kit.Ticks(kit.Or(v.FullDelay, 100))}, nil
}

func (g *raidGarden) params() map[string]float64 {
	return map[string]float64{"search_range": float64(g.xz), "search_height": float64(g.y), "goal_radius": g.radius, "speed_multiplier": g.speed,
		"max_to_eat": float64(g.maxToEat), "initial_eat_delay_ticks": float64(g.initDelay), "eat_delay_ticks": float64(g.eatDelay), "full_delay_ticks": float64(g.fullDelay)}
}

// raidableBlock is a block with the "minecraft:is_raidable" tag that handles
// "minecraft:on_raided" itself (sweet berry bushes and cave vines drop their
// berries, SweetBerryBushBlock/CaveVinesBlock::executeEvent).
type raidableBlock interface {
	Raidable() bool
	Raid(pos cube.Pos, tx *world.Tx, e world.Entity)
}

// edibleCrop reports whether b is a block of the list the mob may eat.
func (g *raidGarden) edibleCrop(b world.Block) bool {
	if !g.blocks.Has(b) {
		return false
	}
	if r, ok := b.(raidableBlock); ok {
		return r.Raidable()
	}
	if c, ok := b.(interface{ GrowthStage() int }); ok {
		return c.GrowthStage() >= 7
	}
	return true
}

// CanUse searches every 20 ticks (BDS tick_interval).
func (g *raidGarden) CanUse(m *mob.Mob) bool {
	if m.Ticks() < g.fullUntil || m.Ticks() < g.next || m.Ticks()%20 != 0 {
		return false
	}
	p, ok := kit.FindBlock(m, g.xz, g.y, "nearest", func(_ cube.Pos, b world.Block) bool { return g.edibleCrop(b) })
	g.target = p
	return ok
}

// CanContinueToUse ...
func (g *raidGarden) CanContinueToUse(m *mob.Mob) bool {
	return !g.done && g.edibleCrop(m.Tx().Block(g.target)) && (g.reached || !m.Navigation().Done())
}

// Start ...
func (g *raidGarden) Start(m *mob.Mob) {
	g.reached, g.done, g.wait = false, false, g.initDelay
	kit.MoveToBlock(m, g.target, g.speed)
}

// Tick ...
func (g *raidGarden) Tick(m *mob.Mob) {
	c := g.target.Vec3Centre()
	kit.LookAt(m, c)
	if !g.reached {
		if kit.HDistSq(m.Position(), c) <= (g.radius+0.5)*(g.radius+0.5) && math.Abs(m.Position()[1]-float64(g.target[1])) <= 1.5 {
			g.reached = true
			kit.Stop(m)
		}
		return
	}
	if g.wait--; g.wait > 0 {
		return
	}
	tx := m.Tx()
	b := tx.Block(g.target)
	if r, ok := b.(raidableBlock); ok {
		r.Raid(g.target, tx, m)
	} else if c, ok := b.(block.Carrot); ok {
		if c.Growth <= 0 {
			tx.SetBlock(g.target, block.Air{}, nil)
		} else {
			c.Growth--
			tx.SetBlock(g.target, c, nil)
		}
	} else {
		tx.SetBlock(g.target, block.Air{}, nil)
	}
	kit.Sound(m, "eat")
	g.eaten++
	g.done = true
	g.next = m.Ticks() + int64(g.eatDelay)
	if g.maxToEat > 0 && g.eaten >= g.maxToEat {
		g.eaten = 0
		g.fullUntil = m.Ticks() + int64(g.fullDelay)
	}
}

// Stop ...
func (g *raidGarden) Stop(m *mob.Mob) { kit.Stop(m) }

// defendTrustedTarget is minecraft:behavior.defend_trusted_target (BDS
// DefendTrustedTargetGoal, a target goal): an entity within within_radius
// that targets one the mob trusts (filter test "trusts", minecraft:trust)
// becomes the mob's target; on_defend_start fires and aggro_sound plays.
//
//	within_radius 0 (fox JSON: 25), must_see false.
type defendTrustedTarget struct {
	mob.GoalBase
	debugName
	radius   float64
	mustSee  bool
	sound    string
	onStart  kit.Trigger
	trusts   mob.Filter
	selected *world.EntityHandle
}

func newDefendTrustedTarget(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Radius  float64         `json:"within_radius"`
		MustSee bool            `json:"must_see"`
		Sound   string          `json:"aggro_sound"`
		OnStart json.RawMessage `json:"on_defend_start"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	t, err := kit.ParseTrigger(v.OnStart)
	if err != nil {
		return nil, err
	}
	f, err := kit.Filter(json.RawMessage(`{"test":"trusts","subject":"other","value":true}`))
	if err != nil {
		return nil, err
	}
	return &defendTrustedTarget{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)}, debugName: "DefendTrustedTargetGoal",
		radius: v.Radius, mustSee: v.MustSee, sound: v.Sound, onStart: t, trusts: f}, nil
}

func (g *defendTrustedTarget) params() map[string]float64 {
	return map[string]float64{"within_radius": g.radius, "must_see": b2f(g.mustSee)}
}

// IsTargetGoal ...
func (*defendTrustedTarget) IsTargetGoal() bool { return true }

// CanUse ...
func (g *defendTrustedTarget) CanUse(m *mob.Mob) bool {
	if g.radius <= 0 || m.Ticks()%10 != 0 {
		return false
	}
	g.selected = nil
	r2 := g.radius * g.radius
	for _, e := range kit.Around(m, g.radius, g.radius) {
		am, ok := kit.Mob(e)
		if !ok || am.Dead() || kit.DistSq(e.Position(), m.Position()) > r2 {
			continue
		}
		victim, ok := am.Target()
		if !ok || victim.H() == m.H() || !g.trusts.Test(kit.OtherCtx(m, victim)) {
			continue
		}
		if g.mustSee && !kit.CanSee(m, e) {
			continue
		}
		g.selected = e.H()
		return true
	}
	return false
}

// CanContinueToUse ...
func (g *defendTrustedTarget) CanContinueToUse(m *mob.Mob) bool {
	t, ok := m.Target()
	return ok && kit.Alive(t) && t.H() == g.selected && kit.DistSq(t.Position(), m.Position()) <= g.radius*g.radius
}

// Start ...
func (g *defendTrustedTarget) Start(m *mob.Mob) {
	if e, ok := g.selected.Entity(m.Tx()); ok {
		m.SetTarget(e)
		kit.Sound(m, g.sound)
		ctx := kit.OtherCtx(m, e)
		ctx.Target = e
		g.onStart.Fire(m, ctx)
	}
}

// Stop ...
func (g *defendTrustedTarget) Stop(m *mob.Mob) {
	if t := m.TargetHandle(); t != nil && t == g.selected {
		m.ResetTarget()
	}
	g.selected = nil
}
