//go:build crossfly_full

package misc

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/goal/combat"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/kit"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// chargeAttack is minecraft:behavior.charge_attack (vex, BDS
// ChargeAttackGoal; source: java/paper Vex.VexChargeAttackGoal for the
// order): with a 1-in-7 chance per tick (success_rate 0.1428) a mob with a
// target further than min_distance (2) blocks flies straight at the
// target's eyes at speed_multiplier (1) with the charging actor flag on.
// Touching the target hits it; while within 3 blocks it keeps aiming at the
// target's eyes. It stops when the move control reaches its point.
type chargeAttack struct {
	mob.GoalBase
	minDist, rate, speed float64
}

func newChargeAttack(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		MinDistance     *float64 `json:"min_distance"`
		SuccessRate     *float64 `json:"success_rate"`
		SpeedMultiplier *float64 `json:"speed_multiplier"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	return &chargeAttack{
		GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)},
		minDist:  kit.Or(v.MinDistance, 2), rate: kit.Or(v.SuccessRate, 0.1428), speed: kit.Or(v.SpeedMultiplier, 1),
	}, nil
}

// CanUse ...
func (g *chargeAttack) CanUse(m *mob.Mob) bool {
	t, ok := m.Target()
	if !ok || !kit.Alive(t) || m.MoveControl().HasWanted() {
		return false
	}
	if m.Rand().Float64() >= g.rate {
		return false
	}
	return kit.DistSq(m.Position(), t.Position()) > g.minDist*g.minDist
}

// CanContinueToUse ...
func (g *chargeAttack) CanContinueToUse(m *mob.Mob) bool {
	t, ok := m.Target()
	return ok && kit.Alive(t) && m.ActorFlag(protocol.EntityDataFlagCharging) && m.MoveControl().HasWanted()
}

// Start ...
func (g *chargeAttack) Start(m *mob.Mob) {
	t, _ := m.Target()
	m.MoveControl().SetWantedPosition(kit.EyePos(t), g.speed)
	kit.Flag(m, protocol.EntityDataFlagCharging, true)
	kit.Sound(m, "charge")
}

// Tick ...
func (g *chargeAttack) Tick(m *mob.Mob) {
	t, ok := m.Target()
	if !ok {
		return
	}
	if m.BBox().IntersectsWith(kit.BBox(t)) {
		combat.DoHurtTarget(m, t)
		kit.Swing(m)
		kit.Flag(m, protocol.EntityDataFlagCharging, false)
		return
	}
	if kit.DistSq(m.Position(), t.Position()) < 9 {
		m.MoveControl().SetWantedPosition(kit.EyePos(t), g.speed)
	}
}

// Stop ...
func (g *chargeAttack) Stop(m *mob.Mob) { kit.Flag(m, protocol.EntityDataFlagCharging, false) }

// AppendDebugInfo ...
func (g *chargeAttack) AppendDebugInfo(b *strings.Builder) { b.WriteString("ChargeAttack") }

// knockbackRoar is minecraft:behavior.knockback_roar (ravager, BDS
// KnockbackRoarGoal): the mob stops and roars for duration (1 s, roaring
// actor flag); attack_time (0.5 s) in, every living entity within
// knockback_range (4) passing knockback_filters is pushed away with
// knockback_horizontal_strength / knockback_vertical_strength (4/4) and,
// passing damage_filters, takes knockback_damage (6). on_roar_start /
// on_roar_end fire, then cooldown_time (0.1 s) passes before the next
// roar. knockback_height_cap (0.4) caps the vertical push.
type knockbackRoar struct {
	mob.GoalBase
	duration, attackAt, cooldown int
	damage, h, v, rng, hcap      float64
	kbFilter, dmgFilter          mob.Filter
	onStart, onEnd               []kit.Trigger
	timer                        int
	cd                           kit.Cooldown
}

func newKnockbackRoar(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Duration      *float64        `json:"duration"`
		AttackTime    *float64        `json:"attack_time"`
		Damage        *float64        `json:"knockback_damage"`
		Horizontal    *float64        `json:"knockback_horizontal_strength"`
		Vertical      *float64        `json:"knockback_vertical_strength"`
		Range         *float64        `json:"knockback_range"`
		HeightCap     *float64        `json:"knockback_height_cap"`
		KnockFilters  json.RawMessage `json:"knockback_filters"`
		DamageFilters json.RawMessage `json:"damage_filters"`
		OnRoarStart   json.RawMessage `json:"on_roar_start"`
		OnRoarEnd     json.RawMessage `json:"on_roar_end"`
		CooldownTime  *float64        `json:"cooldown_time"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	g := &knockbackRoar{
		GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook | mob.FlagJump)},
		duration: max(kit.Ticks(kit.Or(v.Duration, 1)), 1), attackAt: kit.Ticks(kit.Or(v.AttackTime, 0.5)),
		cooldown: kit.Ticks(kit.Or(v.CooldownTime, 0.1)),
		damage:   kit.Or(v.Damage, 6), h: kit.Or(v.Horizontal, 4), v: kit.Or(v.Vertical, 4),
		rng: kit.Or(v.Range, 4), hcap: kit.Or(v.HeightCap, 0.4),
	}
	var err error
	if g.kbFilter, err = kit.Filter(v.KnockFilters); err != nil {
		return nil, err
	}
	if g.dmgFilter, err = kit.Filter(v.DamageFilters); err != nil {
		return nil, err
	}
	if g.onStart, err = kit.ParseTriggers(v.OnRoarStart); err != nil {
		return nil, err
	}
	if g.onEnd, err = kit.ParseTriggers(v.OnRoarEnd); err != nil {
		return nil, err
	}
	return g, nil
}

// CanUse ...
func (g *knockbackRoar) CanUse(m *mob.Mob) bool { return g.cd.Ready(m) }

// CanContinueToUse ...
func (g *knockbackRoar) CanContinueToUse(*mob.Mob) bool { return g.timer < g.duration }

// CanBeInterrupted ...
func (g *knockbackRoar) CanBeInterrupted(*mob.Mob) bool { return false }

// Start ...
func (g *knockbackRoar) Start(m *mob.Mob) {
	g.timer = 0
	kit.Stop(m)
	kit.Flag(m, protocol.EntityDataFlagRoaring, true)
	kit.Sound(m, "roar")
	kit.FireAll(m, g.onStart, nil)
}

// Tick ...
func (g *knockbackRoar) Tick(m *mob.Mob) {
	g.timer++
	if g.timer != g.attackAt {
		return
	}
	for _, e := range kit.Around(m, g.rng, g.rng) {
		l, ok := e.(entity.Living)
		if !ok || !kit.Alive(e) || kit.DistSq(e.Position(), m.Position()) > g.rng*g.rng {
			continue
		}
		ctx := kit.OtherCtx(m, e)
		if g.dmgFilter.Test(ctx) && g.damage > 0 {
			l.Hurt(g.damage, entity.AttackDamageSource{Attacker: m})
		}
		if g.kbFilter.Test(ctx) && kit.Alive(e) {
			roarPush(m, l, g.h, g.v, g.hcap)
		}
	}
}

// roarPush pushes l away from m: horizontal strength over the distance
// (the closer, the harder; BDS KnockbackRoarGoal::_knockback), vertical
// capped at hcap.
func roarPush(m *mob.Mob, l entity.Living, h, v, hcap float64) {
	d := l.Position().Sub(m.Position())
	d[1] = 0
	dist := math.Max(d.Len(), 0.001)
	strength := h / math.Max(dist, 1) * 0.2
	dir := d.Mul(1 / dist)
	vel := mgl64.Vec3{dir[0] * strength, math.Min(v*0.1, hcap), dir[2] * strength}
	l.SetVelocity(l.Velocity().Add(vel))
}

// Stop ...
func (g *knockbackRoar) Stop(m *mob.Mob) {
	kit.Flag(m, protocol.EntityDataFlagRoaring, false)
	g.cd.Set(m, g.cooldown)
	kit.FireAll(m, g.onEnd, nil)
}

// AppendDebugInfo ...
func (g *knockbackRoar) AppendDebugInfo(b *strings.Builder) { b.WriteString("KnockbackRoar") }
