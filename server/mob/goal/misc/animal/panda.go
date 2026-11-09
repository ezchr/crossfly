//go:build crossfly_full

package animal

import (
	"encoding/json"
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/kit"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// roll is minecraft:behavior.roll (BDS RollGoal::_handleRoll, same as
// java/paper Panda.handleRoll): with probability per tick a panda on the
// ground with room ahead rolls forward for 32 ticks: a 0.27 hop and a push
// of 0.2 (0.1 for babies) along its facing on the first tick, the push kept
// up every tick, a hop again at ticks 7, 15 and 23. Actor flag rolling.
//
//	probability 0.02 (LegacyGoalDefinition default).
type roll struct {
	mob.GoalBase
	debugName
	prob    float64
	counter int
	delta   mgl64.Vec3
}

func newRoll(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Prob *float64 `json:"probability"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	return &roll{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook | mob.FlagJump)}, debugName: "RollGoal", prob: kit.Or(v.Prob, 0.02)}, nil
}

func (g *roll) params() map[string]float64 { return map[string]float64{"probability": g.prob} }

// CanUse ...
func (g *roll) CanUse(m *mob.Mob) bool {
	if !m.OnGround() || m.Physics().InWater || m.Sitting() || m.Rand().Float64() >= g.prob {
		return false
	}
	// RollGoal::canUse: the block ahead (by the facing's dominant axes) must
	// be free.
	f := forward(m)
	dx, dz := 0, 0
	if math.Abs(f[0]) > 0.5 {
		dx = int(math.Copysign(1, f[0]))
	}
	if math.Abs(f[2]) > 0.5 {
		dz = int(math.Copysign(1, f[2]))
	}
	p := blockAt(m.Position()).Add(cube.Pos{dx, 0, dz})
	return !kit.Solid(m.Tx(), p)
}

// CanContinueToUse ...
func (g *roll) CanContinueToUse(m *mob.Mob) bool { return g.counter <= 32 && !m.Physics().InWater }

// Start ...
func (g *roll) Start(m *mob.Mob) {
	g.counter = 0
	kit.Stop(m)
	kit.Flag(m, protocol.EntityDataFlagRolling, true)
}

// Tick is RollGoal::_handleRoll.
func (g *roll) Tick(m *mob.Mob) {
	g.counter++
	if g.counter > 32 {
		return
	}
	v := m.Velocity()
	switch {
	case g.counter == 1:
		f, s := forward(m), 0.2
		if m.Baby() {
			s = 0.1
		}
		g.delta = mgl64.Vec3{v[0] + f[0]*s, 0, v[2] + f[2]*s}
		m.SetVelocity(g.delta.Add(mgl64.Vec3{0, 0.27, 0}))
	case g.counter == 7 || g.counter == 15 || g.counter == 23:
		y := v[1]
		if m.OnGround() {
			y = 0.27
		}
		m.SetVelocity(mgl64.Vec3{0, y, 0})
	default:
		m.SetVelocity(mgl64.Vec3{g.delta[0], v[1], g.delta[2]})
	}
}

// Stop ...
func (g *roll) Stop(m *mob.Mob) { kit.Flag(m, protocol.EntityDataFlagRolling, false) }

// sneeze is minecraft:behavior.sneeze (BDS SneezeGoal): with probability
// per tick (after cooldown_time) the panda prepares (prepare_sound, actor
// flag sneezing) for prepare_time, then sneezes: "sound" plays, the
// loot_table drops with drop_item_chance and the entities of entity_types
// within within_radius jump (source: java/paper Panda.afterSneeze).
//
//	probability 0.02, cooldown_time 0, prepare_time 1 s, drop_item_chance 1,
//	within_radius 0.
type sneeze struct {
	mob.GoalBase
	debugName
	prob, dropChance, radius float64
	cooldown, prepare        int
	table, sound, preSound   string
	types                    []mob.Filter

	timer int
	next  int64
}

func newSneeze(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Prob        *float64 `json:"probability"`
		Cooldown    float64  `json:"cooldown_time"`
		Prepare     *float64 `json:"prepare_time"`
		DropChance  *float64 `json:"drop_item_chance"`
		Radius      float64  `json:"within_radius"`
		Loot        string   `json:"loot_table"`
		Sound       string   `json:"sound"`
		PreSound    string   `json:"prepare_sound"`
		EntityTypes []struct {
			Filters json.RawMessage `json:"filters"`
		} `json:"entity_types"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	g := &sneeze{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, debugName: "SneezeGoal",
		prob: kit.Or(v.Prob, 0.02), cooldown: kit.Ticks(v.Cooldown), prepare: kit.Ticks(kit.Or(v.Prepare, 1)),
		dropChance: kit.Or(v.DropChance, 1), radius: v.Radius, table: v.Loot, sound: v.Sound, preSound: v.PreSound}
	for _, t := range v.EntityTypes {
		f, err := kit.Filter(t.Filters)
		if err != nil {
			return nil, err
		}
		g.types = append(g.types, f)
	}
	return g, nil
}

func (g *sneeze) params() map[string]float64 {
	return map[string]float64{"probability": g.prob, "cooldown_ticks": float64(g.cooldown), "prepare_ticks": float64(g.prepare),
		"drop_item_chance": g.dropChance, "within_radius": g.radius}
}

// CanUse ...
func (g *sneeze) CanUse(m *mob.Mob) bool {
	return m.Ticks() >= g.next && !m.Physics().InWater && m.Rand().Float64() < g.prob
}

// CanContinueToUse ...
func (g *sneeze) CanContinueToUse(*mob.Mob) bool { return g.timer >= 0 }

// Start ...
func (g *sneeze) Start(m *mob.Mob) {
	g.timer = g.prepare
	kit.Stop(m)
	kit.Flag(m, protocol.EntityDataFlagSneezing, true)
	kit.Sound(m, g.preSound)
}

// Tick ...
func (g *sneeze) Tick(m *mob.Mob) {
	if g.timer--; g.timer > 0 {
		return
	}
	g.timer = -1
	kit.Sound(m, g.sound)
	if g.table != "" && m.Rand().Float64() <= g.dropChance {
		dropAt(m.Tx(), m, m.Position().Add(forward(m).Mul(0.5)).Add(mgl64.Vec3{0, m.EyeHeight(), 0}), rollLoot(m, g.table))
	}
	if g.radius <= 0 {
		return
	}
	for _, e := range kit.Around(m, g.radius, g.radius) {
		om, ok := kit.Mob(e)
		if !ok || om.Dead() || kit.DistSq(e.Position(), m.Position()) > g.radius*g.radius {
			continue
		}
		for _, f := range g.types {
			if f.Test(kit.OtherCtx(m, e)) {
				om.JumpControl().Jump()
				break
			}
		}
	}
}

// Stop ...
func (g *sneeze) Stop(m *mob.Mob) {
	kit.Flag(m, protocol.EntityDataFlagSneezing, false)
	g.next = m.Ticks() + int64(g.cooldown)
}

// snack is minecraft:behavior.snacking (BDS SnackGoal): a panda holding, or
// seeing within 6 blocks, an item of "items" walks to it, takes it into its
// main hand (within 2 blocks), sits and eats until the snacking_stop_chance
// roll per tick ends it; then the item is gone and it waits
// snacking_cooldown_min + random(snacking_cooldown) seconds.
//
//	snacking_cooldown 7.5, snacking_cooldown_min 0.5,
//	snacking_stop_chance 0.0016666667.
type snack struct {
	mob.GoalBase
	debugName
	cooldown, cooldownMin int
	stop                  float64
	items                 []core.ItemDescriptor

	item *world.EntityHandle
	next int64
}

func newSnack(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Cooldown    *float64        `json:"snacking_cooldown"`
		CooldownMin *float64        `json:"snacking_cooldown_min"`
		Stop        *float64        `json:"snacking_stop_chance"`
		Items       json.RawMessage `json:"items"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	items, err := core.ParseItemList(v.Items)
	if err != nil {
		return nil, err
	}
	return &snack{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, debugName: "SnackGoal",
		cooldown: int(math.Max(float64(float32(kit.Or(v.Cooldown, 7.5))*20), 1)), cooldownMin: int(math.Max(float64(float32(kit.Or(v.CooldownMin, 0.5))*20), 0)),
		stop: math.Min(math.Max(kit.Or(v.Stop, 0.0016666667), 0), 1), items: items}, nil
}

func (g *snack) params() map[string]float64 {
	return map[string]float64{"cooldown_ticks": float64(g.cooldown), "cooldown_min_ticks": float64(g.cooldownMin), "stop_chance": g.stop}
}

func (g *snack) snackable(s item.Stack) bool { return !s.Empty() && core.MatchesAny(g.items, s) }

func (g *snack) holding(m *mob.Mob) bool {
	main, _ := m.HeldItems()
	return g.snackable(main)
}

// findItem is SnackGoal::_hasSnackableItems: an item entity within 6 blocks.
func (g *snack) findItem(m *mob.Mob) (world.Entity, bool) {
	for _, e := range kit.Around(m, 6, 6) {
		if s, ok := itemEntity(e); ok && g.snackable(s) {
			return e, true
		}
	}
	return nil, false
}

// CanUse ...
func (g *snack) CanUse(m *mob.Mob) bool {
	if m.Ticks() < g.next || m.Physics().InWater || !m.OnGround() {
		return false
	}
	if g.holding(m) {
		return true
	}
	e, ok := g.findItem(m)
	if ok {
		g.item = e.H()
	}
	return ok
}

// CanContinueToUse ...
func (g *snack) CanContinueToUse(m *mob.Mob) bool {
	if g.holding(m) {
		return m.Rand().Float64() >= g.stop
	}
	if g.item == nil {
		return false
	}
	e, ok := g.item.Entity(m.Tx())
	return ok && !e.H().Closed()
}

// Start ...
func (g *snack) Start(m *mob.Mob) {
	if g.holding(m) {
		return
	}
	if e, ok := g.item.Entity(m.Tx()); ok {
		m.Navigation().MoveToEntity(m, e, 1)
	}
}

// Tick ...
func (g *snack) Tick(m *mob.Mob) {
	if g.holding(m) {
		kit.Stop(m)
		if !m.Sitting() {
			sit(m, true)
			kit.Flag(m, protocol.EntityDataFlagEating, true)
		}
		if m.Ticks()%20 == 0 {
			kit.Sound(m, "eat")
		}
		return
	}
	e, ok := g.item.Entity(m.Tx())
	if !ok {
		return
	}
	kit.LookAt(m, e.Position())
	if kit.DistSq(e.Position(), m.Position()) > 4 {
		if m.Navigation().Done() {
			m.Navigation().MoveToEntity(m, e, 1)
		}
		return
	}
	s, _ := itemEntity(e)
	_ = e.Close()
	if s.Count() > 1 {
		dropAt(m.Tx(), m, e.Position(), []item.Stack{s.Grow(-1)})
	}
	_, off := m.HeldItems()
	m.SetHeldItems(s.Grow(1-s.Count()), off)
	g.item = nil
}

// Stop ...
func (g *snack) Stop(m *mob.Mob) {
	if g.holding(m) {
		_, off := m.HeldItems()
		m.SetHeldItems(item.Stack{}, off)
	}
	sit(m, false)
	kit.Flag(m, protocol.EntityDataFlagEating, false)
	g.item = nil
	g.next = m.Ticks() + int64(g.cooldownMin+m.Rand().IntN(g.cooldown))
}

// layDown is minecraft:behavior.lay_down (BDS LayDownGoal): out of water,
// with a 1/interval chance per tick (always with 0) the mob lies down (actor
// flag laying_down) and gets up with a 1/random_stop_interval chance per
// tick (at once with 0).
//
//	interval 120, random_stop_interval 120.
type layDown struct {
	mob.GoalBase
	debugName
	interval, stop int
}

func newLayDown(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Interval *int `json:"interval"`
		Stop     *int `json:"random_stop_interval"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	return &layDown{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, debugName: "LayDownGoal",
		interval: max(kit.Or(v.Interval, 120), 0), stop: max(kit.Or(v.Stop, 120), 0)}, nil
}

func (g *layDown) params() map[string]float64 {
	return map[string]float64{"interval": float64(g.interval), "random_stop_interval": float64(g.stop)}
}

// CanUse ...
func (g *layDown) CanUse(m *mob.Mob) bool {
	if m.Physics().InWater {
		return false
	}
	return g.interval == 0 || m.Rand().IntN(g.interval) == 0
}

// CanContinueToUse ...
func (g *layDown) CanContinueToUse(m *mob.Mob) bool { return g.stop != 0 && m.Rand().IntN(g.stop) != 0 }

// Start ...
func (g *layDown) Start(m *mob.Mob) {
	kit.Stop(m)
	kit.Flag(m, protocol.EntityDataFlagLayingDown, true)
}

// Tick ...
func (g *layDown) Tick(m *mob.Mob) { kit.Stop(m) }

// Stop ...
func (g *layDown) Stop(m *mob.Mob) { kit.Flag(m, protocol.EntityDataFlagLayingDown, false) }

// scared is minecraft:behavior.scared (BDS ScaredGoal): during a
// thunderstorm a mob that is not sitting is scared (actor flag scared): it
// stands up and stops; when the storm ends it stops being scared. Control
// flags move and look (BDS: 3).
type scared struct {
	mob.GoalBase
	debugName
	interval int
}

func newScared(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Interval *int `json:"sound_interval"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	return &scared{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, debugName: "ScaredGoal", interval: kit.Or(v.Interval, 0)}, nil
}

func (g *scared) params() map[string]float64 {
	return map[string]float64{"sound_interval": float64(g.interval)}
}

// CanUse ...
func (g *scared) CanUse(m *mob.Mob) bool { return !m.Sitting() && thundering(m) }

// CanContinueToUse ...
func (g *scared) CanContinueToUse(m *mob.Mob) bool { return thundering(m) }

// Start ...
func (g *scared) Start(m *mob.Mob) {
	sit(m, false)
	kit.Stop(m)
	kit.Flag(m, protocol.EntityDataFlagScared, true)
}

// Tick ...
func (g *scared) Tick(m *mob.Mob) {
	kit.Stop(m)
	if g.interval > 0 && m.Rand().IntN(g.interval) == 0 {
		kit.Sound(m, "ambient")
	}
}

// Stop ...
func (g *scared) Stop(m *mob.Mob) { kit.Flag(m, protocol.EntityDataFlagScared, false) }

// randomSitting is minecraft:behavior.random_sitting (BDS RandomSittingGoal
// definition in LegacyGoalDefinition): on the ground and out of water, after
// cooldown seconds, with start_chance per tick the mob sits; after
// min_sit_time seconds it stands up with stop_chance per tick.
//
//	start_chance 0.1, stop_chance 0.3, cooldown 100 s, min_sit_time 10 s.
type randomSitting struct {
	mob.GoalBase
	debugName
	start, stop       float64
	cooldown, minTime int
	sat               int
	next              int64
}

func newRandomSitting(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Start    *float64 `json:"start_chance"`
		Stop     *float64 `json:"stop_chance"`
		Cooldown *float64 `json:"cooldown"`
		MinTime  *float64 `json:"min_sit_time"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	return &randomSitting{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, debugName: "RandomSittingGoal",
		start: kit.Or(v.Start, 0.1), stop: kit.Or(v.Stop, 0.3), cooldown: kit.Ticks(kit.Or(v.Cooldown, 100)), minTime: kit.Ticks(kit.Or(v.MinTime, 10))}, nil
}

func (g *randomSitting) params() map[string]float64 {
	return map[string]float64{"start_chance": g.start, "stop_chance": g.stop, "cooldown_ticks": float64(g.cooldown), "min_sit_ticks": float64(g.minTime)}
}

// CanUse ...
func (g *randomSitting) CanUse(m *mob.Mob) bool {
	if m.Ticks() < g.next || !m.OnGround() || m.Physics().InWater || m.Sitting() {
		return false
	}
	return m.Rand().Float64() < g.start
}

// CanContinueToUse ...
func (g *randomSitting) CanContinueToUse(m *mob.Mob) bool {
	if m.Physics().InWater {
		return false
	}
	return g.sat < g.minTime || m.Rand().Float64() >= g.stop
}

// Start ...
func (g *randomSitting) Start(m *mob.Mob) {
	g.sat = 0
	kit.Stop(m)
	sit(m, true)
}

// Tick ...
func (g *randomSitting) Tick(m *mob.Mob) {
	g.sat++
	kit.Stop(m)
}

// Stop ...
func (g *randomSitting) Stop(m *mob.Mob) {
	sit(m, false)
	g.next = m.Ticks() + int64(g.cooldown)
}
