//go:build crossfly_full

package core

import (
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/go-gl/mathgl/mgl64"
)

// randomStroll is minecraft:behavior.random_stroll: now and then walk to a
// random spot nearby (BDS RandomStrollGoal).
//
//	speed_multiplier 1, xz_dist 10 (>= 1), y_dist 7 (>= 1), interval 120
//	(1 in interval chance per tick to start; 0 = always). Flags move|look.
type randomStroll struct {
	mob.GoalBase
	speed    float64
	xz, y    int
	interval int

	wanted mgl64.Vec3
	done   bool
}

func newRandomStroll(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Speed    float64 `json:"speed_multiplier"`
		XZ       *int    `json:"xz_dist"`
		Y        *int    `json:"y_dist"`
		Interval *int    `json:"interval"`
	}{Speed: 1}
	if err := spec.Decode(&j); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	g := &randomStroll{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, speed: j.Speed, xz: 10, y: 7, interval: 120}
	if j.XZ != nil {
		g.xz = max(*j.XZ, 1)
	}
	if j.Y != nil {
		g.y = max(*j.Y, 1)
	}
	if j.Interval != nil {
		g.interval = *j.Interval
	}
	return g, nil
}

func (g *randomStroll) CanUse(m *mob.Mob) bool {
	if !hasNavigation(m) {
		return false
	}
	if g.interval > 0 && m.Rand().IntN(g.interval) != 0 {
		return false
	}
	pos, ok := RandomPos(m, g.xz, g.y, 0, nil, 10)
	if !ok {
		return false
	}
	g.wanted = pos
	return true
}

func (g *randomStroll) CanContinueToUse(*mob.Mob) bool { return !g.done }

func (g *randomStroll) Start(m *mob.Mob) {
	// The navigation paths to the block's centre.
	if !m.Navigation().MoveTo(m, g.wanted.Add(mgl64.Vec3{0.5, 0, 0.5}), g.speed) {
		g.done = true
	}
}

func (g *randomStroll) Tick(m *mob.Mob) { g.done = m.Navigation().Done() }

func (g *randomStroll) Stop(m *mob.Mob) {
	m.Navigation().Stop(m)
	g.done = false
}

func (g *randomStroll) AppendDebugInfo(b *strings.Builder) {
	fmt.Fprintf(b, "RandomStroll to %.0f %.0f %.0f", g.wanted[0], g.wanted[1], g.wanted[2])
}
