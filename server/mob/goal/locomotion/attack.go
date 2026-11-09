//go:build crossfly_full

package locomotion

import (
	"fmt"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/mob/goal/combat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// guardianAttack is minecraft:behavior.guardian_attack (BDS
// GuardianAttackGoal): stand still, look at the target and charge the beam
// (the target actor data the clients draw); 10 ticks in the attack sound
// plays, after the attack duration (80 ticks, elder 60) the target takes the
// mob's attack damage, then magic_damage (+ hard_mode_extra_magic_damage on
// hard, + elder_extra_magic_damage for elders) on top, both scaled by
// difficulty against players (5+1 on normal, 7.5+4.5 on hard; elders 5+3
// and 7.5+7.5), and the mob lets go of it.
// Losing sight of the target drops it; a non-elder closer than min_distance
// stops.
//
//	magic_damage 1, hard_mode_extra_magic_damage 2, elder_extra_magic_damage
//	2, x_max_rotation 90, y_max_head_rotation 90, min_distance 3,
//	sound_delay_time 0.5 s. Flags move|look.
type guardianAttack struct {
	mob.GoalBase
	magic, hardExtra, elderExtra float64
	xMax, yMax, minDist          float64
	attackTime                   int
}

func newGuardianAttack(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Magic   *float64 `json:"magic_damage"`
		Hard    *float64 `json:"hard_mode_extra_magic_damage"`
		Elder   *float64 `json:"elder_extra_magic_damage"`
		XMax    *float64 `json:"x_max_rotation"`
		YMax    *float64 `json:"y_max_head_rotation"`
		MinDist *float64 `json:"min_distance"`
	}{}
	if err := decodeGoal(spec, &j); err != nil {
		return nil, err
	}
	g := &guardianAttack{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, magic: 1, hardExtra: 2, elderExtra: 2, xMax: 90, yMax: 90, minDist: 3}
	set := func(dst *float64, v *float64) {
		if v != nil {
			*dst = *v
		}
	}
	set(&g.magic, j.Magic)
	set(&g.hardExtra, j.Hard)
	set(&g.elderExtra, j.Elder)
	set(&g.xMax, j.XMax)
	set(&g.yMax, j.YMax)
	set(&g.minDist, j.MinDist)
	return g, nil
}

// elder reports whether m is an elder guardian (ActorFlag ELDER).
func elder(m *mob.Mob) bool { return m.Identifier() == "minecraft:elder_guardian" }

// guardianAttackDuration is Guardian::getAttackDuration.
func guardianAttackDuration(m *mob.Mob) int {
	if elder(m) {
		return 60
	}
	return 80
}

func (g *guardianAttack) CanUse(m *mob.Mob) bool {
	t, ok := m.Target()
	return ok && alive(m.Tx(), t) && hasNavigation(m)
}

func (g *guardianAttack) CanContinueToUse(m *mob.Mob) bool {
	if !g.CanUse(m) {
		return false
	}
	t, _ := m.Target()
	return elder(m) || t.Position().Sub(m.Position()).LenSqr() > g.minDist*g.minDist
}

func (g *guardianAttack) Start(m *mob.Mob) {
	g.attackTime = -10
	m.Navigation().Stop(m)
	if t, ok := m.Target(); ok {
		m.LookControl().SetLookAtEntity(t, 90, 90)
	}
}

// guardianSound is the attack sound actor event (28).
type guardianSound struct{}

func (guardianSound) EntityAction()     {}
func (guardianSound) ActorEvent() uint8 { return packet.ActorEventGuardianAttackSound }

func (g *guardianAttack) Tick(m *mob.Mob) {
	t, ok := m.Target()
	if !ok {
		return
	}
	m.Navigation().Stop(m)
	m.LookControl().SetLookAtEntity(t, g.yMax, g.xMax)
	if !sense.CanSee(m, t) {
		m.ResetTarget()
		return
	}
	g.attackTime++
	if g.attackTime == 0 {
		core.ShowAction(m, guardianSound{})
		return
	}
	if g.attackTime < guardianAttackDuration(m) {
		return
	}
	l, living := t.(entity.Living)
	if living {
		var dealt float64
		// Player.Hurt scales the attack hit by difficulty itself.
		if dmg := core.AttackOf(m).Damage; dmg > 0 {
			if n, res := l.Hurt(dmg, entity.AttackDamageSource{Attacker: m}); res.Accepted() {
				dealt = n
			}
		}
		magic := g.magic
		if sense.DifficultyID(m.Tx()) == 3 {
			magic += g.hardExtra
		}
		if elder(m) {
			magic += g.elderExtra
		}
		if magic = scaleMobDamage(m, t, magic); magic > 0 {
			// BDS marks the target "chained" (setChainedDamageEffects) so
			// the magic hit is not swallowed by the hurt cooldown the first
			// hit started. Dragonfly's players and mobs only count what
			// exceeds the last hit within the cooldown, so the magic hit is
			// raised by what the first one dealt.
			l.Hurt(dealt+magic, core.CauseSource{Cause: "magic"})
		}
	}
	m.ResetTarget()
	g.attackTime = 0
}

func (g *guardianAttack) Stop(m *mob.Mob) {
	m.ResetTarget()
	g.attackTime = 0
}

// scaleMobDamage is the difficulty scaling Player::_hurt applies to damage a
// mob deals to a player: none on peaceful, min(d/2+1, d) on easy, d x 1.5 on
// hard. Damage to anything else is not scaled. Player.Hurt scales attacks
// itself; the magic hit is a plain cause source it does not scale, as its
// amount also carries the first hit (already scaled).
func scaleMobDamage(m *mob.Mob, t world.Entity, dmg float64) float64 {
	if !sense.IsPlayer(t) {
		return dmg
	}
	switch sense.DifficultyID(m.Tx()) {
	case 0:
		return 0
	case 1:
		return math.Min(dmg/2+1, dmg)
	case 3:
		return dmg * 1.5
	}
	return dmg
}

func (g *guardianAttack) AppendDebugInfo(b *strings.Builder) {
	fmt.Fprintf(b, "GuardianAttack %d", g.attackTime)
}

// GuardianAttackTime is the beam charge of a guardian's attack goal (0 when
// none runs).
func GuardianAttackTime(m *mob.Mob) int {
	gl, ok := m.Goals().Goal("minecraft:behavior.guardian_attack")
	if !ok {
		return 0
	}
	if g, ok := gl.(*guardianAttack); ok {
		return max(g.attackTime, 0)
	}
	return 0
}

// aquaticChargeAttack is minecraft:behavior.aquatic_charge_attack (nautilus,
// 1.26; not in BDS 1.21.3, so the 1.26 JSON fields with the Java nautilus
// dash): in water, with a target within max_charge_distance, dash at
// charge_speed x the swim speed through the target's position and
// charge_overshoot_distance past it; touching the target (boxes grown by
// attack_reach) hurts it and knocks it back with knockback_force. Between
// charges it waits charge_cooldown_time seconds.
//
//	attack_reach 0, charge_cooldown_time [4, 4] s, charge_overshoot_distance
//	1.5, charge_speed 1.2, knockback_force 2, max_charge_distance 16.
//	Flags move|look.
type aquaticChargeAttack struct {
	mob.GoalBase
	reach, overshoot, speed, knockback, maxDist float64
	cooldownRange                               floatRange

	cooldown int
	dest     mgl64.Vec3
	ticks    int
	hit      bool
}

func newAquaticChargeAttack(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Reach     *float64   `json:"attack_reach"`
		Cooldown  floatRange `json:"charge_cooldown_time"`
		Overshoot *float64   `json:"charge_overshoot_distance"`
		Speed     *float64   `json:"charge_speed"`
		Knockback *float64   `json:"knockback_force"`
		MaxDist   *float64   `json:"max_charge_distance"`
	}{}
	if err := decodeGoal(spec, &j); err != nil {
		return nil, err
	}
	g := &aquaticChargeAttack{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, overshoot: 1.5, speed: 1.2, knockback: 2, maxDist: 16, cooldownRange: floatRange{Min: 4, Max: 4}}
	set := func(dst *float64, v *float64) {
		if v != nil {
			*dst = *v
		}
	}
	set(&g.reach, j.Reach)
	set(&g.overshoot, j.Overshoot)
	set(&g.speed, j.Speed)
	set(&g.knockback, j.Knockback)
	set(&g.maxDist, j.MaxDist)
	if j.Cooldown.set {
		g.cooldownRange = j.Cooldown
	}
	return g, nil
}

// maxChargeTicks bounds one dash.
const maxChargeTicks = 60

func (g *aquaticChargeAttack) CanUse(m *mob.Mob) bool {
	if g.cooldown > 0 {
		g.cooldown--
		return false
	}
	t, ok := m.Target()
	if !ok || !alive(m.Tx(), t) || !m.Physics().InWater {
		return false
	}
	return t.Position().Sub(m.Position()).LenSqr() <= g.maxDist*g.maxDist && sense.CanSee(m, t)
}

func (g *aquaticChargeAttack) Start(m *mob.Mob) {
	t, _ := m.Target()
	p := sense.Center(t)
	g.dest = p.Add(normalize(p.Sub(m.Position())).Mul(g.overshoot))
	g.ticks, g.hit = 0, false
	if hasNavigation(m) {
		m.Navigation().Stop(m)
	}
}

func (g *aquaticChargeAttack) CanContinueToUse(m *mob.Mob) bool {
	if g.hit || g.ticks >= maxChargeTicks || !m.Physics().InWater {
		return false
	}
	if _, ok := m.Target(); !ok {
		return false
	}
	return g.dest.Sub(m.Position()).LenSqr() > 1
}

func (g *aquaticChargeAttack) Tick(m *mob.Mob) {
	g.ticks++
	t, ok := m.Target()
	if !ok {
		return
	}
	m.LookControl().SetLookAtEntity(t, 30, 30)
	m.MoveControl().SetWantedPosition(g.dest, g.speed)
	if m.BBox().Grow(g.reach).IntersectsWith(entityBox(t)) {
		if combat.DoHurtTarget(m, t) {
			if kb, ok := t.(interface {
				KnockBack(src mgl64.Vec3, force, height float64)
			}); ok {
				kb.KnockBack(m.Position(), 0.2*g.knockback, 0.1*g.knockback)
			}
		}
		g.hit = true
	}
}

func (g *aquaticChargeAttack) Stop(m *mob.Mob) {
	m.MoveControl().Stop()
	g.cooldown = int(g.cooldownRange.roll(m) * 20)
}

func (g *aquaticChargeAttack) AppendDebugInfo(b *strings.Builder) {
	fmt.Fprintf(b, "AquaticChargeAttack %d", g.ticks)
}

var _ world.Entity
