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
// TimerActorFlagBaseGoal, TimerActorFlag1/2/3Goal): after a cooldown it runs
// for a rolled duration with the actor flag timer_flag_N on, firing on_start
// and on_end.
//
//	cooldown_range  seconds before the next run (default 10 s). The first run
//	                also waits a cooldown roll from when the goal is added
//	                (TimerActorFlagBaseDefinition::initialize overwrites the
//	                constructor's now+200), so cooldown 0 starts on the next
//	                tick (bee effects) and the sniffer first sniffs after
//	                400-500 s.
//	duration_range  seconds the goal runs (default 2 s); both ranges are
//	                rolled in ticks like BDS (roundf(s*20)).
//	on_start, on_end  triggers. start() runs on_start before it sets the
//	                flag; stop() runs on_end before it clears the flag, then
//	                rolls the next cooldown.
//
// canUse: now > next start; canContinueToUse: now <= end. No control flags by
// default (BDS Goal()): the data sets them (sniffer move|look, stand_up move).
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
		g.init, g.nextStart = true, m.Ticks()+int64(tickRange(m, g.cooldown))
	}
	return m.Ticks() > g.nextStart
}

// CanContinueToUse ...
func (g *timerFlag) CanContinueToUse(m *mob.Mob) bool { return m.Ticks() <= g.endsAt }

// Start ...
func (g *timerFlag) Start(m *mob.Mob) {
	g.endsAt = m.Ticks() + int64(tickRange(m, g.length))
	g.onStart.Fire(m, nil)
	m.SetActorFlag(g.flag, true)
}

// Stop ...
func (g *timerFlag) Stop(m *mob.Mob) {
	g.onEnd.Fire(m, nil)
	m.SetActorFlag(g.flag, false)
	g.nextStart = m.Ticks() + int64(tickRange(m, g.cooldown))
}

// tickRange rolls a seconds range the way BDS IntRange does after
// TimerActorFlagBaseDefinition::initialize turned it into ticks: both ends
// rounded to ticks, then a whole number of ticks between them.
func tickRange(m *mob.Mob, r kit.Range) int {
	return kit.Range{Min: float64(kit.Ticks(r.Min)), Max: float64(kit.Ticks(r.Max))}.RollInt(m.Rand())
}

// AppendDebugInfo ...
func (g *timerFlag) AppendDebugInfo(b *strings.Builder) { b.WriteString("TimerActorFlag") }

var timerFlags = map[string]uint8{
	"minecraft:behavior.timer_flag_1": protocol.EntityDataFlagTimerFlag1,
	"minecraft:behavior.timer_flag_2": protocol.EntityDataFlagTimerFlag2,
	"minecraft:behavior.timer_flag_3": protocol.EntityDataFlagTimerFlag3,
}
