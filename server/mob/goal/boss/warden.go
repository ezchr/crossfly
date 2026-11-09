//go:build crossfly_full

package boss

import (
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	bc "github.com/df-mc/dragonfly/server/mob/component/boss"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// --- behavior.sonic_boom ---

// sonicBoom is SonicBoomGoal: when the warden has not hit in melee for a
// while (Warden cooldowns: 200 ticks after a new target, 100 after a melee
// hit, attack_cooldown after a boom) and the target is within 15
// horizontally and 20 vertically, it walks on, charges for
// duration_until_attack_sound and then hits through walls and armour
// (attack_damage, BDS cause SonicBoom; Resistance still counts) with a
// knock back.
//
//	duration 0 (warden 3 s), speed_multiplier 1 (1.2)
//	attack_damage 30 (10), attack_cooldown 5 s (2)
//	attack_range_horizontal 15, attack_range_vertical 20
//	duration_until_attack_sound 1.7 s
//	knockback_horizontal_strength / vertical / height_cap 0 (2.5 / 0.5 / 0.5)
//	attack_sound, charge_sound
type sonicBoom struct {
	mob.GoalBase
	duration, untilSound int
	speed, damage        float64
	cooldown             int
	rangeH, rangeV       float64
	kbH, kbV, kbCap      float64
	attackSound          string
	chargeSound          string

	attackTick, end int64
}

func newSonicBoom(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Duration    *float64 `json:"duration"`
		Speed       *float64 `json:"speed_multiplier"`
		Damage      *float64 `json:"attack_damage"`
		Cooldown    *float64 `json:"attack_cooldown"`
		RangeH      *float64 `json:"attack_range_horizontal"`
		RangeV      *float64 `json:"attack_range_vertical"`
		UntilSound  *float64 `json:"duration_until_attack_sound"`
		KbH         *float64 `json:"knockback_horizontal_strength"`
		KbV         *float64 `json:"knockback_vertical_strength"`
		KbCap       *float64 `json:"knockback_height_cap"`
		AttackSound string   `json:"attack_sound"`
		ChargeSound string   `json:"charge_sound"`
	}
	if err := spec.Decode(&v); err != nil {
		return nil, err
	}
	return &sonicBoom{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)},
		duration: bc.Ticks(or(v.Duration, 0)), speed: or(v.Speed, 1), damage: or(v.Damage, 30),
		cooldown: bc.Ticks(or(v.Cooldown, 5)), rangeH: or(v.RangeH, 15), rangeV: or(v.RangeV, 20),
		untilSound: bc.Ticks(or(v.UntilSound, 1.7)), kbH: or(v.KbH, 0), kbV: or(v.KbV, 0), kbCap: or(v.KbCap, 0),
		attackSound: v.AttackSound, chargeSound: v.ChargeSound}, nil
}

func (g *sonicBoom) target(m *mob.Mob) (world.Entity, bool) {
	t, ok := m.Target()
	if !ok || !bc.Living(t) || !m.HasNavigation() {
		return nil, false
	}
	if sense.IsPlayer(t) && sense.DifficultyID(m.Tx()) == 0 {
		m.ResetTarget()
		return nil, false
	}
	return t, true
}

func (g *sonicBoom) CanUse(m *mob.Mob) bool {
	t, ok := g.target(m)
	if !ok || m.Ticks() <= bc.WardenOf(m).SonicCooldown {
		return false
	}
	d := t.Position().Sub(m.Position())
	return math.Hypot(d[0], d[2]) <= g.rangeH && math.Abs(d[1]) <= g.rangeV
}

func (g *sonicBoom) CanContinueToUse(m *mob.Mob) bool {
	if m.Ticks() > g.end {
		return false
	}
	t, ok := g.target(m)
	if !ok {
		return false
	}
	d := t.Position().Sub(m.Position())
	return math.Hypot(d[0], d[2]) <= g.rangeH && math.Abs(d[1]) <= g.rangeV
}

func (g *sonicBoom) Start(m *mob.Mob) {
	t, _ := m.Target()
	m.Navigation().MoveToEntity(m, t, g.speed)
	m.SetActorFlag(protocol.EntityDataFlagSonicBoom, true)
	g.attackTick = m.Ticks() + int64(g.untilSound)
	g.end = m.Ticks() + int64(g.duration)
	m.LookControl().SetLookAtEntity(t, 10, 90)
	if g.chargeSound != "" {
		m.PlaySound(g.chargeSound)
	}
}

func (g *sonicBoom) Tick(m *mob.Mob) {
	t, ok := m.Target()
	if !ok {
		return
	}
	m.Navigation().MoveToEntity(m, t, g.speed)
	m.LookControl().SetLookAtEntity(t, 10, 90)
	if m.Ticks() <= g.attackTick {
		return
	}
	g.attackTick = m.Ticks() + int64(g.duration-g.untilSound)
	g.boom(m, t)
}

// boom is the hit: particles from the warden's body to the target's eyes,
// the sound, the damage and the knock back.
func (g *sonicBoom) boom(m *mob.Mob, t world.Entity) {
	tx := m.Tx()
	from := sense.Center(m)
	to := sense.EyePos(t)
	dir := to.Sub(from)
	dist := dir.Len()
	if dist > 0 {
		dir = dir.Normalize()
	}
	for i := 1; i <= int(math.Floor(dist))+6; i++ {
		tx.AddParticle(from.Add(dir.Mul(float64(i))), particle.Named{Bedrock: "minecraft:sonic_explosion", Java: "sonic_boom"})
	}
	if g.attackSound != "" {
		m.PlaySound(g.attackSound)
	}
	if l, ok := t.(entity.Living); ok {
		l.Hurt(g.damage, bc.SonicBoomDamageSource{Attacker: m})
	}
	if _, isMob := mob.Of(t); isMob || sense.IsPlayer(t) {
		bc.KnockBack(t, m.Position(), g.kbH, g.kbV, g.kbCap)
	}
}

func (g *sonicBoom) Stop(m *mob.Mob) {
	m.SetActorFlag(protocol.EntityDataFlagSonicBoom, false)
	bc.WardenOf(m).SonicCooldown = m.Ticks() + int64(g.cooldown)
}

func (g *sonicBoom) AppendDebugInfo(b *strings.Builder) { b.WriteString("Sonic boom") }

// --- behavior.roar ---

// roar is RoarGoal: a warden without a target whose top nuisance is past
// the angry threshold roars at it for duration (actor flag roaring), then
// makes it the target with angry_boost more anger.
type roar struct {
	mob.GoalBase
	duration int
	at       *world.EntityHandle
	end      int64
}

func newRoar(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Duration *float64 `json:"duration"`
	}
	if err := spec.Decode(&v); err != nil {
		return nil, err
	}
	return &roar{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, duration: bc.Ticks(or(v.Duration, 0))}, nil
}

func (g *roar) CanUse(m *mob.Mob) bool {
	if _, has := m.Target(); has {
		return false
	}
	a, ok := bc.AngerOf(m)
	if !ok {
		return false
	}
	top, ok := a.TopNuisance(m)
	if !ok || a.BelowThreshold(top) {
		return false
	}
	g.at = top.H()
	return true
}

func (g *roar) CanContinueToUse(m *mob.Mob) bool {
	_, has := m.Target()
	return !has && m.Ticks() <= g.end
}

func (g *roar) Start(m *mob.Mob) {
	g.end = m.Ticks() + int64(g.duration)
	m.SetActorFlag(protocol.EntityDataFlagRoaring, true)
	m.StopMovement()
	m.PlaySound("roar")
}

func (g *roar) Tick(m *mob.Mob) {
	if t, ok := g.at.Entity(m.Tx()); ok {
		m.LookControl().SetLookAt(sense.EyePos(t), 30, 30)
	}
}

func (g *roar) Stop(m *mob.Mob) {
	m.SetActorFlag(protocol.EntityDataFlagRoaring, false)
	t, ok := g.at.Entity(m.Tx())
	if !ok || !bc.Living(t) {
		return
	}
	m.SetTarget(t)
	if a, ok := bc.AngerOf(m); ok {
		a.TryIncreaseAngerAt(m, t, a.Boost, false)
	}
}

func (g *roar) AppendDebugInfo(b *strings.Builder) { b.WriteString("Roar") }

// --- behavior.dig ---

// dig is DigGoal: after idle_time without vibrations and suspicion (and
// idle_time since it appeared), a warden on the ground digs into it for
// duration (actor flag digging, on_start) and is gone. In water or lava it
// leaves at once with the agitated sound.
//
//	duration 0 (warden 5.5 s), idle_time 0 (60 s)
//	vibration_is_disturbance, suspicion_is_disturbance false (true)
//	allow_dig_when_named false, digs_in_daylight false, on_start
type dig struct {
	mob.GoalBase
	duration, idle  int
	vibration       bool
	suspicion       bool
	named, daylight bool
	onStart         sense.Trigger
	created, end    int64
	createdSet      bool
}

func newDig(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Duration  *float64 `json:"duration"`
		Idle      *float64 `json:"idle_time"`
		Vibration bool     `json:"vibration_is_disturbance"`
		Suspicion bool     `json:"suspicion_is_disturbance"`
		Named     bool     `json:"allow_dig_when_named"`
		Daylight  bool     `json:"digs_in_daylight"`
		OnStart   jsonRaw  `json:"on_start"`
	}
	if err := spec.Decode(&v); err != nil {
		return nil, err
	}
	t, err := sense.ParseTrigger(v.OnStart.raw())
	if err != nil {
		return nil, err
	}
	return &dig{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)},
		duration: bc.Ticks(or(v.Duration, 0)), idle: bc.Ticks(or(v.Idle, 0)), vibration: v.Vibration,
		suspicion: v.Suspicion, named: v.Named, daylight: v.Daylight, onStart: t}, nil
}

func (g *dig) CanUse(m *mob.Mob) bool {
	now := m.Ticks()
	if !g.createdSet {
		g.created, g.createdSet = now, true
	}
	switch {
	case m.ActorFlag(protocol.EntityDataFlagDigging):
		return false
	case m.NameTag() != "" && !g.named:
		return false
	case !m.OnGround() && !m.Physics().InWater && !m.Physics().InLava:
		return false
	}
	if g.daylight && sense.IsDay(m.Tx()) && sense.SkyVisible(m.Tx(), cube.PosFromVec3(m.EyePosition())) {
		return true
	}
	if l, ok := bc.ListenerOf(m); ok && g.vibration && now-l.LastTick < int64(g.idle) {
		return false
	}
	if s, ok := bc.SuspectOf(m); ok && g.suspicion && now-s.LastTick < int64(g.idle) {
		return false
	}
	return now-g.created >= int64(g.idle)
}

func (g *dig) CanContinueToUse(m *mob.Mob) bool { return m.Ticks() <= g.end }

func (g *dig) Start(m *mob.Mob) {
	g.end = m.Ticks() + int64(g.duration)
	if m.Physics().InWater || m.Physics().InLava {
		m.PlaySound("agitated")
		_ = m.Close()
		return
	}
	m.SetActorFlag(protocol.EntityDataFlagDigging, true)
	m.StopMovement()
	g.onStart.Fire(m, nil)
}

func (g *dig) Tick(m *mob.Mob) { m.StopMovement() }

// Stop removes the warden, finished or not (DigGoal::stop); a warden that
// died meanwhile (void, /kill) keeps its death.
func (g *dig) Stop(m *mob.Mob) {
	if !m.Dead() {
		_ = m.Close()
	}
}

func (g *dig) AppendDebugInfo(b *strings.Builder) { b.WriteString("Dig") }

// --- behavior.emerge ---

// emerge is EmergeGoal: the warden climbs out of the ground for duration
// (actor flag emerging, invulnerable), then on_done; cooldown_time keeps it
// from starting again at once.
type emerge struct {
	mob.GoalBase
	duration, cooldown int
	onDone             sense.Trigger
	next, end          int64
}

func newEmerge(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Duration *float64 `json:"duration"`
		Cooldown *float64 `json:"cooldown_time"`
		OnDone   jsonRaw  `json:"on_done"`
	}
	if err := spec.Decode(&v); err != nil {
		return nil, err
	}
	t, err := sense.ParseTrigger(v.OnDone.raw())
	if err != nil {
		return nil, err
	}
	return &emerge{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook | mob.FlagJump)},
		duration: bc.Ticks(or(v.Duration, 5)), cooldown: bc.Ticks(or(v.Cooldown, 0.5)), onDone: t}, nil
}

func (g *emerge) CanUse(m *mob.Mob) bool { return m.Ticks() >= g.next }

func (g *emerge) CanContinueToUse(m *mob.Mob) bool { return m.Ticks() <= g.end }

func (g *emerge) Start(m *mob.Mob) {
	g.end = m.Ticks() + int64(g.duration)
	m.SetActorFlag(protocol.EntityDataFlagEmerging, true)
	m.StopMovement()
	m.PlaySound("emerge")
}

func (g *emerge) Tick(m *mob.Mob) { m.StopMovement() }

func (g *emerge) Stop(m *mob.Mob) {
	g.next = m.Ticks() + int64(g.cooldown)
	m.SetActorFlag(protocol.EntityDataFlagEmerging, false)
	g.onDone.Fire(m, nil)
}

func (g *emerge) AppendDebugInfo(b *strings.Builder) { b.WriteString("Emerge") }

// --- behavior.investigate_suspicious_location ---

// investigate is InvestigateSuspiciousLocationGoal: a warden without a
// target walks to its suspicious position (suspect_tracking) until it is
// within goal_radius, the position changes or no path is found; then the
// position is forgotten.
type investigate struct {
	mob.GoalBase
	speed, radius float64

	dest            cube.Pos
	end             cube.Pos
	failed, reached bool
}

func newInvestigate(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Speed  *float64 `json:"speed_multiplier"`
		Radius *float64 `json:"goal_radius"`
	}
	if err := spec.Decode(&v); err != nil {
		return nil, err
	}
	return &investigate{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)},
		speed: or(v.Speed, 1), radius: or(v.Radius, 1.5)}, nil
}

func centre(p cube.Pos) mgl64.Vec3 { return p.Vec3Centre() }

func (g *investigate) CanUse(m *mob.Mob) bool {
	if _, has := m.Target(); has || !m.HasNavigation() || m.Riding() {
		return false
	}
	s, ok := bc.SuspectOf(m)
	if !ok {
		return false
	}
	p, ok := s.SuspiciousPos()
	if !ok || m.Position().Sub(centre(p)).LenSqr() <= g.radius*g.radius {
		return false
	}
	g.dest = p
	return true
}

func (g *investigate) CanContinueToUse(m *mob.Mob) bool {
	if _, has := m.Target(); has || g.failed || g.reached {
		return false
	}
	s, ok := bc.SuspectOf(m)
	if !ok {
		return false
	}
	p, ok := s.SuspiciousPos()
	return ok && p == g.dest
}

func (g *investigate) Start(m *mob.Mob) {
	g.failed, g.reached = false, false
	if !m.Navigation().MoveTo(m, g.dest.Vec3Middle(), g.speed) {
		g.failed = true
		return
	}
	g.end = g.dest
	if p := m.Navigation().Path(); p != nil {
		g.end = p.End()
	}
}

func (g *investigate) Tick(m *mob.Mob) {
	if g.failed || !m.Navigation().Done() {
		return
	}
	if m.Position().Sub(centre(g.end)).LenSqr() <= g.radius*g.radius || m.Position().Sub(centre(g.dest)).LenSqr() <= g.radius*g.radius {
		g.reached = true
		return
	}
	if !m.Navigation().MoveTo(m, g.end.Vec3Middle(), g.speed) {
		g.failed = true
	}
}

func (g *investigate) Stop(m *mob.Mob) {
	if s, ok := bc.SuspectOf(m); ok {
		if p, ok := s.SuspiciousPos(); ok && p == g.dest {
			s.Clear()
		}
	}
	m.Navigation().Stop(m)
}

func (g *investigate) AppendDebugInfo(b *strings.Builder) {
	b.WriteString("Investigate suspicious location")
}
