//go:build crossfly_full

package combat

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/mob/component/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// rangedAttack is minecraft:behavior.ranged_attack (BDS RangedAttackGoal):
// approach to attack_radius, stand still once the target was seen for
// target_in_sight_time, back off inside attack_radius_min, and fire through
// minecraft:shooter on attack_interval (bursts of burst_shots), or, in
// charge mode (charge_shoot_trigger: ghast, blaze), once the target was
// seen for charge_shoot_trigger seconds.
type rangedAttack struct {
	mob.GoalBase
	intervalMin, intervalMax int     // +132, +136
	burstShots               int     // +148
	burstInterval            int     // +124
	sightTime                int     // +152
	radius                   float64 // +156
	radiusMin                float64 // +160
	radiusMinSq              float64 // +164
	yMaxHead, xMaxRot        float64 // +168, +172
	fov                      float64 // +176
	speed                    float64 // +180
	setPersistent            bool    // +106
	swing                    bool    // +107
	// Charge mode (+104): chargeCharged and chargeShoot are
	// charge_charged_trigger and charge_shoot_trigger in ticks (+140, +144).
	chargeMode                 bool
	chargeCharged, chargeShoot int

	target      *world.EntityHandle
	attackTime  int // +108
	burstLeft   int // +116
	burstTicker int // +120
	seeTime     int // +112
	charge      int // +128
	charged     bool
}

func newRangedAttack(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		AttackInterval    json.RawMessage `json:"attack_interval"`
		AttackIntervalMin *float64        `json:"attack_interval_min"`
		AttackIntervalMax *float64        `json:"attack_interval_max"`
		AttackRadius      *float64        `json:"attack_radius"`
		AttackRadiusMin   *float64        `json:"attack_radius_min"`
		AttackRange       json.RawMessage `json:"attack_range"`
		BurstInterval     float64         `json:"burst_interval"`
		BurstShots        *int            `json:"burst_shots"`
		ChargeCharged     float64         `json:"charge_charged_trigger"`
		ChargeShoot       float64         `json:"charge_shoot_trigger"`
		RangedFOV         *float64        `json:"ranged_fov"`
		SetPersistent     bool            `json:"set_persistent"`
		SpeedMultiplier   *float64        `json:"speed_multiplier"`
		Swing             bool            `json:"swing"`
		TargetInSightTime *float64        `json:"target_in_sight_time"`
		XMaxRotation      *float64        `json:"x_max_rotation"`
		YMaxHeadRotation  *float64        `json:"y_max_head_rotation"`
	}
	if err := spec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	g := &rangedAttack{
		GoalBase:      mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)},
		burstShots:    1,
		burstInterval: sense.Ticks(v.BurstInterval),
		sightTime:     sense.Ticks(or(v.TargetInSightTime, 1)),
		yMaxHead:      or(v.YMaxHeadRotation, 30),
		xMaxRot:       or(v.XMaxRotation, 30),
		fov:           or(v.RangedFOV, 90),
		speed:         or(v.SpeedMultiplier, 1),
		setPersistent: v.SetPersistent,
		swing:         v.Swing,
		attackTime:    -1,
		chargeCharged: sense.Ticks(v.ChargeCharged),
		chargeShoot:   sense.Ticks(v.ChargeShoot),
	}
	g.chargeMode = g.chargeShoot > 0
	// attack_interval is seconds: a number, or {min, max}; the separate
	// attack_interval_min/max override it.
	iv, err := sense.ParseRange(v.AttackInterval, sense.Range{})
	if err != nil {
		return nil, fmt.Errorf("%v: attack_interval: %w", spec.Key, err)
	}
	if v.AttackIntervalMin != nil {
		iv.Min = *v.AttackIntervalMin
	}
	if v.AttackIntervalMax != nil {
		iv.Max = *v.AttackIntervalMax
	}
	iv.Max = math.Max(iv.Max, iv.Min)
	g.intervalMin, g.intervalMax = sense.Ticks(iv.Min), sense.Ticks(iv.Max)
	rr, err := sense.ParseRange(v.AttackRange, sense.Range{})
	if err != nil {
		return nil, fmt.Errorf("%v: attack_range: %w", spec.Key, err)
	}
	g.radius, g.radiusMin = rr.Max, rr.Min
	if v.AttackRadius != nil {
		g.radius = *v.AttackRadius
	}
	if v.AttackRadiusMin != nil {
		g.radiusMin = *v.AttackRadiusMin
	}
	g.radiusMinSq = g.radiusMin * g.radiusMin
	if v.BurstShots != nil && *v.BurstShots > 0 {
		g.burstShots = *v.BurstShots
	}
	return g, nil
}

// CanUse is RangedAttackGoal::canUse.
func (g *rangedAttack) CanUse(m *mob.Mob) bool {
	// An empty crossbow must be charged first (charge_held_item).
	if loot.NeedsCharge(m) {
		return false
	}
	t, ok := m.Target()
	if !ok || !sense.Alive(t) {
		return false
	}
	g.target = t.H()
	if g.setPersistent && sense.IsPlayer(t) {
		m.SetPersistent(true)
	}
	return true
}

// CanContinueToUse is RangedAttackGoal::canContinueToUse: canUse, or still
// walking.
func (g *rangedAttack) CanContinueToUse(m *mob.Mob) bool {
	return g.CanUse(m) || !m.Navigation().Done()
}

// Start ...
func (g *rangedAttack) Start(m *mob.Mob) {
	m.UpdateMeta(func(meta *mob.Meta) { meta.Aggressive = true })
}

// Stop is RangedAttackGoal::stop.
func (g *rangedAttack) Stop(m *mob.Mob) {
	g.target = nil
	g.seeTime = 0
	if g.chargeMode {
		g.setCharged(m, false)
	}
	m.UpdateMeta(func(meta *mob.Meta) { meta.Aggressive = false })
}

// Tick is RangedAttackGoal::tick.
func (g *rangedAttack) Tick(m *mob.Mob) {
	if g.target == nil {
		return
	}
	t, ok := g.target.Entity(m.Tx())
	if !ok || !sense.Alive(t) {
		return
	}
	tpos := t.Position()
	aim := mgl64.Vec3{tpos[0], sense.BBox(t).Min()[1], tpos[2]}
	distSq := aim.Sub(m.Pos()).LenSqr()
	seen := sense.CanSee(m, t)
	if seen {
		g.seeTime++
	} else {
		g.seeTime = 0
	}
	nav := m.Navigation()
	switch {
	case distSq <= g.radius*g.radius && g.seeTime >= g.sightTime && distSq >= g.radiusMinSq:
		// In range and seen long enough: stand and face the target.
		nav.Stop(m)
		m.MoveControl().Stop()
		yaw := sense.YawTowards(m.Pos(), tpos)
		m.SetBodyYaw(yaw)
		m.SetYaw(yaw)
	case distSq < g.radiusMinSq:
		// Too close: walk away to attack_radius_min + 0.5.
		back := g.radiusMin - math.Sqrt(distSq) + 0.5
		d := m.Pos().Sub(tpos)
		d[1] = 0
		if d.Len() > 1e-4 {
			d = d.Normalize()
		}
		nav.MoveTo(m, m.Pos().Add(d.Mul(back)), g.speed)
	default:
		nav.MoveToEntity(m, t, g.speed)
	}
	m.LookControl().SetLookAtEntity(t, g.xMaxRot, g.yMaxHead)
	g.handleAttack(m, t, aim, distSq, seen)
}

// interval is the next attack time: the interval range scaled by the
// distance to the target (BDS: floor((max-min) * dist/radius + min)).
func (g *rangedAttack) interval(distSq float64) int {
	f := 1.0
	if g.radius > 0 {
		f = math.Sqrt(distSq) / g.radius
	}
	return int(math.Floor(float64(g.intervalMax-g.intervalMin)*f + float64(g.intervalMin)))
}

// handleAttack is RangedAttackGoal::handleAttackBehavior.
func (g *rangedAttack) handleAttack(m *mob.Mob, t world.Entity, aim mgl64.Vec3, distSq float64, seen bool) {
	if g.burstLeft <= 0 && g.chargeMode {
		// Charge mode: the charge rises while the target is seen and falls
		// while it is not; at charge_shoot_trigger a burst starts and the
		// charge drops by twice the trigger. Above charge_charged_trigger
		// the mob shows itself charged (the ghast's open mouth, the
		// blaze's fire), with the warn sound as it gets there.
		if seen {
			if g.charge++; g.charge >= g.chargeShoot {
				g.burstLeft, g.burstTicker = g.burstShots, 0
				g.charge -= 2 * g.chargeShoot
			}
		} else {
			g.charge--
		}
		g.setCharged(m, g.charge > g.chargeCharged)
		if g.charge == g.chargeCharged {
			m.PlaySound("warn")
		}
		return
	}
	if g.burstLeft <= 0 {
		g.attackTime--
		switch {
		case g.attackTime == 0:
			g.burstLeft, g.burstTicker = g.burstShots, 0
		case g.attackTime < 0:
			g.attackTime = g.interval(distSq)
		}
		return
	}
	g.burstTicker--
	if !seen || distSq > g.radius*g.radius || distSq < g.radiusMinSq {
		return
	}
	if g.burstTicker > 0 || !sense.WithinFOV(m, aim, g.fov) {
		return
	}
	if s, ok := mob.ComponentOf[sense.Shooter](m); ok {
		s.Shoot(m, t)
	}
	if !g.chargeMode {
		if a, ok := mob.ComponentOf[sense.AngerTimer](m); ok {
			a.RestartAngerTimer(m)
		}
		g.attackTime = g.interval(distSq)
	}
	loot.Discharge(m)
	if g.swing {
		for _, v := range m.Tx().Viewers(m.Position()) {
			v.ViewEntityAction(m, entity.SwingArmAction{})
		}
	}
	if g.burstLeft--; g.burstLeft > 0 {
		g.burstTicker = g.burstInterval
	}
}

// setCharged is Actor::setCharged plus the ChargeAmount actor data the goal
// keeps with it: the Bedrock charged flag and, for Java viewers, the ghast's
// charging and the blaze's on fire state.
func (g *rangedAttack) setCharged(m *mob.Mob, v bool) {
	if g.charged == v && m.ActorFlag(protocol.EntityDataFlagCharged) == v {
		return
	}
	g.charged = v
	m.SetActorFlag(protocol.EntityDataFlagCharged, v)
	var amount byte
	if v {
		amount = 1
	}
	m.SetActorData(protocol.EntityDataKeyChargeAmount, amount)
}

// AppendDebugInfo ...
func (g *rangedAttack) AppendDebugInfo(b *strings.Builder) {
	fmt.Fprintf(b, "RangedAttack t=%d burst=%d", g.attackTime, g.burstLeft)
	if g.chargeMode {
		fmt.Fprintf(b, " charge=%d", g.charge)
	}
}
