//go:build crossfly_full

package core

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/mob"
	ccore "github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// tempt is minecraft:behavior.tempt: follow the nearest player holding one
// of the items (BDS TemptGoal).
//
//	speed_multiplier 1, items, within_radius (0 = 16), can_get_scared (stop
//	when the player moves or turns while close), can_tempt_vertically,
//	can_tempt_while_ridden, tempt_sound, sound_interval, on_tempt_start /
//	on_tempt_end (1.26). Flags move|look.
//
// The mob walks to the player until it is within (both widths)/2 + 1.5
// blocks, repathing every 8-11 ticks, and waits 100 ticks after it stops
// before it can be tempted again.
type tempt struct {
	mob.GoalBase
	speed          float64
	items          []ccore.ItemDescriptor
	radius         float64
	scared         bool
	vertical       bool
	whileRidden    bool
	sound          string
	interval       soundInterval
	onStart, onEnd *ccore.Trigger

	player    *world.EntityHandle
	lastPos   mgl64.Vec3
	lastYaw   float64
	lastPitch float64
	cooldown  int
	repath    int
	running   bool
}

func newTempt(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Speed       float64         `json:"speed_multiplier"`
		Items       json.RawMessage `json:"items"`
		Radius      float64         `json:"within_radius"`
		Scared      bool            `json:"can_get_scared"`
		Vertical    bool            `json:"can_tempt_vertically"`
		WhileRidden bool            `json:"can_tempt_while_ridden"`
		Sound       string          `json:"tempt_sound"`
		Interval    *soundInterval  `json:"sound_interval"`
		OnStart     *ccore.Trigger  `json:"on_tempt_start"`
		OnEnd       *ccore.Trigger  `json:"on_tempt_end"`
	}{Speed: 1}
	if err := spec.Decode(&j); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	items, err := ccore.ParseItemList(j.Items)
	if err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	g := &tempt{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, speed: j.Speed, items: items,
		radius: j.Radius, scared: j.Scared, vertical: j.Vertical, whileRidden: j.WhileRidden, sound: j.Sound,
		onStart: j.OnStart, onEnd: j.OnEnd}
	if g.radius == 0 {
		g.radius = 16
	}
	if j.Interval != nil {
		g.interval = *j.Interval
	}
	return g, nil
}

// tempting reports whether p holds a tempting item in the main hand.
func (g *tempt) tempting(p *player.Player) bool {
	main, _ := p.HeldItems()
	return ccore.MatchesAny(g.items, main)
}

// nearestPlayer is Dimension::fetchNearestPlayer with the goal's predicate:
// a visible, living player within the radius holding a tempting item.
func (g *tempt) nearestPlayer(m *mob.Mob) (*player.Player, bool) {
	var best *player.Player
	bestD := g.radius * g.radius
	for e := range m.Tx().Players() {
		p, ok := isPlayer(e)
		if !ok || p.Dead() || !g.tempting(p) {
			continue
		}
		if d := distSq(p.Position(), m.Position()); d <= bestD {
			best, bestD = p, d
		}
	}
	return best, best != nil
}

// close reports whether the mob is near enough to the player to stop.
func (g *tempt) close(m *mob.Mob, p world.Entity) bool {
	box := p.H().Type().BBox(p).Translate(p.Position())
	c := box.Min().Add(box.Max()).Mul(0.5)
	d := c.Sub(m.Position())
	if !g.vertical {
		d[1] = 0
	}
	r := (m.BBox().Width()+box.Width())*0.5 + 1.5
	return d.LenSqr() < r*r
}

func (g *tempt) CanUse(m *mob.Mob) bool {
	if g.cooldown > 0 {
		g.cooldown--
		return false
	}
	if !hasNavigation(m) {
		return false
	}
	p, ok := g.nearestPlayer(m)
	if !ok {
		g.player = nil
		return false
	}
	g.player = p.H()
	if !g.close(m, p) {
		m.SetPersistent(true)
		return true
	}
	m.Navigation().Stop(m)
	return true
}

func (g *tempt) CanContinueToUse(m *mob.Mob) bool {
	if g.scared && g.player != nil {
		e, ok := g.player.Entity(m.Tx())
		if !ok {
			return false
		}
		pos, rot := e.Position(), e.Rotation()
		if distSq(pos, m.Position()) >= 36 {
			g.lastPos = pos
		} else if distSq(pos, g.lastPos) > 0.01 || math.Abs(rot.Yaw()-g.lastYaw) > 5 || math.Abs(rot.Pitch()-g.lastPitch) > 5 {
			return false
		}
		g.lastYaw, g.lastPitch = rot.Yaw(), rot.Pitch()
	}
	return g.CanUse(m)
}

func (g *tempt) Start(m *mob.Mob) {
	if g.player == nil {
		return
	}
	e, ok := g.player.Entity(m.Tx())
	if !ok {
		return
	}
	g.lastPos, g.lastYaw, g.lastPitch = e.Position(), e.Rotation().Yaw(), e.Rotation().Pitch()
	g.running, g.repath = true, 0
	if g.onStart != nil {
		ctx := m.Ctx()
		ctx.Other, ctx.Player = e, e
		g.onStart.Fire(m, ctx)
	}
}

func (g *tempt) Stop(m *mob.Mob) {
	var e world.Entity
	if g.player != nil {
		e, _ = g.player.Entity(m.Tx())
	}
	g.player = nil
	m.Navigation().Stop(m)
	g.cooldown, g.running = 100, false
	if g.onEnd != nil {
		ctx := m.Ctx()
		ctx.Other, ctx.Player = e, e
		g.onEnd.Fire(m, ctx)
	}
}

func (g *tempt) Tick(m *mob.Mob) {
	g.repath--
	if g.player == nil {
		return
	}
	e, ok := g.player.Entity(m.Tx())
	if !ok {
		return
	}
	m.LookControl().SetLookAtEntity(e, 30, maxHeadXRot)
	if !g.close(m, e) {
		if g.repath <= 0 {
			m.Navigation().MoveToEntity(m, e, g.speed)
			g.repath = 8 + m.Rand().IntN(4)
		}
	} else {
		m.Navigation().Stop(m)
	}
	if g.sound != "" && g.interval.due(m) {
		ccore.PlayMobSound(m, g.sound)
	}
}

// Tempted reports whether the goal is running (BDS Actor::isTempted).
func (g *tempt) Tempted() bool { return g.running }

func (g *tempt) AppendDebugInfo(b *strings.Builder) {
	fmt.Fprintf(b, "Tempt cooldown %d", g.cooldown)
}
