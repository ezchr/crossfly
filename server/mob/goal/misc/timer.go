//go:build crossfly_full

package misc

import (
	"encoding/json"
	"strings"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/kit"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// timerFlag is minecraft:behavior.timer_flag_1/2/3 (BDS
// TimerActorFlagBaseGoal): after a cooldown it runs for a rolled duration
// with the actor flag timer_flag_N on, firing on_start and on_end.
//
//	cooldown_range  seconds before the next run (default 10 s); the first
//	                run waits 200 ticks after the goal is added
//	duration_range  seconds the goal runs (default 2 s)
//	on_start, on_end  triggers
//
// No control flags by default (BDS Goal()).
type timerFlag struct {
	mob.GoalBase
	flag              uint8
	cooldown, length  kit.Range
	onStart, onEnd    kit.Trigger
	nextStart, endsAt int64
	init              bool
}

func newTimerFlag(flag uint8) mob.GoalFactory {
	return func(spec mob.GoalSpec) (mob.Goal, error) {
		var v struct {
			Cooldown json.RawMessage `json:"cooldown_range"`
			Duration json.RawMessage `json:"duration_range"`
			OnStart  json.RawMessage `json:"on_start"`
			OnEnd    json.RawMessage `json:"on_end"`
		}
		if err := kit.Decode(spec, &v); err != nil {
			return nil, err
		}
		g := &timerFlag{GoalBase: mob.GoalBase{Control: spec.FlagsOr(0)}, flag: flag}
		var err error
		if g.cooldown, err = kit.ParseRange(v.Cooldown, kit.Range{Min: 10, Max: 10}); err != nil {
			return nil, err
		}
		if g.length, err = kit.ParseRange(v.Duration, kit.Range{Min: 2, Max: 2}); err != nil {
			return nil, err
		}
		if g.onStart, err = kit.ParseTrigger(v.OnStart); err != nil {
			return nil, err
		}
		if g.onEnd, err = kit.ParseTrigger(v.OnEnd); err != nil {
			return nil, err
		}
		return g, nil
	}
}

// CanUse ...
func (g *timerFlag) CanUse(m *mob.Mob) bool {
	if !g.init {
		g.init, g.nextStart = true, m.Ticks()+200
	}
	return m.Ticks() > g.nextStart
}

// CanContinueToUse ...
func (g *timerFlag) CanContinueToUse(m *mob.Mob) bool { return m.Ticks() <= g.endsAt }

// Start ...
func (g *timerFlag) Start(m *mob.Mob) {
	g.endsAt = m.Ticks() + int64(kit.RollTicks(m, g.length))
	m.SetActorFlag(g.flag, true)
	g.onStart.Fire(m, nil)
}

// Stop ...
func (g *timerFlag) Stop(m *mob.Mob) {
	g.nextStart = m.Ticks() + int64(kit.RollTicks(m, g.cooldown))
	m.SetActorFlag(g.flag, false)
	g.onEnd.Fire(m, nil)
}

// AppendDebugInfo ...
func (g *timerFlag) AppendDebugInfo(b *strings.Builder) { b.WriteString("TimerActorFlag") }

var timerFlags = map[string]uint8{
	"minecraft:behavior.timer_flag_1": protocol.EntityDataFlagTimerFlag1,
	"minecraft:behavior.timer_flag_2": protocol.EntityDataFlagTimerFlag2,
	"minecraft:behavior.timer_flag_3": protocol.EntityDataFlagTimerFlag3,
}
