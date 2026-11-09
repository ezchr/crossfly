//go:build crossfly_full

package locomotion

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/world"
)

// lastHurt is the last hit a mob took (Mob last hurt timestamp, cause and
// damage), recorded by a hurt hook for play_dead.
type lastHurt struct {
	tick  int64
	cause string
	dealt float64
}

type lastHurtKey struct{}

func recordHurt(m *mob.Mob, dealt float64, src world.DamageSource) {
	if dealt <= 0 {
		return
	}
	m.SetData(lastHurtKey{}, lastHurt{tick: m.Ticks(), cause: core.CauseOf(src), dealt: dealt})
}

// playDead is minecraft:behavior.play_dead (BDS PlayDeadGoal, axolotl): after
// a hit of one of damage_sources (all by default) passing filters, the mob
// plays dead for duration seconds (1): always below force_below_health,
// otherwise with random_start_chance (1) when the damage is above a roll of
// random_damage_range (0..0). It stops moving, gets Regeneration for the
// duration with apply_regeneration and shows the PLAYING_DEAD flag; at the
// end it forgets its target. Flags move|look.
type playDead struct {
	mob.GoalBase
	duration     int
	startChance  float64
	forceBelow   float64
	damageRange  floatRange
	regeneration bool
	causes       []string
	filter       mob.Filter

	seen    int64
	endTick int64
}

func newPlayDead(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Duration     *float64        `json:"duration"`
		StartChance  *float64        `json:"random_start_chance"`
		ForceBelow   float64         `json:"force_below_health"`
		DamageRange  floatRange      `json:"random_damage_range"`
		Regeneration bool            `json:"apply_regeneration"`
		Sources      json.RawMessage `json:"damage_sources"`
		Filters      json.RawMessage `json:"filters"`
	}{}
	if err := decodeGoal(spec, &j); err != nil {
		return nil, err
	}
	g := &playDead{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, duration: 20, startChance: 1, forceBelow: j.ForceBelow, damageRange: j.DamageRange, regeneration: j.Regeneration, seen: -1}
	if j.Duration != nil {
		g.duration = int(*j.Duration * 20)
	}
	if j.StartChance != nil {
		g.startChance = *j.StartChance
	}
	if len(j.Sources) > 0 {
		var list []string
		var one string
		switch {
		case json.Unmarshal(j.Sources, &list) == nil:
		case json.Unmarshal(j.Sources, &one) == nil:
			list = []string{one}
		}
		if !slices.Contains(list, "all") {
			g.causes = list
		}
	}
	f, err := mob.CompileFilter(j.Filters)
	if err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	g.filter = f
	return g, nil
}

func (g *playDead) CanUse(m *mob.Mob) bool {
	v, ok := m.Data(lastHurtKey{})
	if !ok {
		return false
	}
	h := v.(lastHurt)
	if h.tick == g.seen {
		return false
	}
	g.seen = h.tick
	if g.causes != nil && !slices.Contains(g.causes, h.cause) {
		return false
	}
	if !g.filter.Test(m.Ctx()) {
		return false
	}
	if m.Health() < g.forceBelow {
		return true
	}
	if m.Rand().Float64() < g.startChance {
		return h.dealt > float64(g.damageRange.rollInt(m))
	}
	return false
}

func (g *playDead) CanContinueToUse(m *mob.Mob) bool { return m.Ticks() < g.endTick }

func (g *playDead) Start(m *mob.Mob) {
	m.Navigation().Stop(m)
	if g.regeneration {
		m.AddEffect(effect.New(effect.Regeneration, 1, time.Duration(g.duration)*time.Second/20))
	}
	m.SetActorFlag(flagPlayingDead, true)
	g.endTick = m.Ticks() + int64(g.duration)
}

func (g *playDead) Stop(m *mob.Mob) {
	m.SetActorFlag(flagPlayingDead, false)
	m.ResetTarget()
}

func (g *playDead) AppendDebugInfo(b *strings.Builder) { b.WriteString("PlayDead") }

// PlayingDead reports whether m plays dead (the PLAYING_DEAD actor flag).
func PlayingDead(m *mob.Mob) bool { return m.ActorFlag(flagPlayingDead) }
