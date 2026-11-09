//go:build crossfly_full

package combat

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/mob/component/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// meleeBase is BDS MeleeAttackBaseGoal: chasing the target with path
// recalculation back-off and attacking when the subclass says it can.
type meleeBase struct {
	mob.GoalBase
	key string

	attackOnce        bool    // +104
	requireComplete   bool    // +106
	trackTarget       bool    // +107
	randomStop        int     // +108
	cooldownTime      int     // +116
	minPath, maxPath  int     // +120, +124
	pathFailInc       int     // +132
	innerInc          int     // +136
	outerInc          int     // +140
	innerSq, outerSq  float64 // +144, +148
	fov               float64 // +152
	yMaxHead, xMaxRot float64 // +156, +160
	speed             float64 // +164
	attackTypes       string  // +188
	setPersistent     bool    // +400
	spreadFire        bool    // +401
	onAttack, onKill  sense.Trigger

	target    *world.EntityHandle // +64
	path      mob.Path            // +168
	cooldown  int                 // +112
	ticker    int                 // +128
	lastPos   mgl64.Vec3          // +176
	hitResult bool                // +105
}

type rawMelee struct {
	AttackOnce          bool            `json:"attack_once"`
	AttackTypes         string          `json:"attack_types"`
	CooldownTime        *float64        `json:"cooldown_time"`
	InnerBoundaryInc    *float64        `json:"inner_boundary_time_increase"`
	MaxPathTime         *float64        `json:"max_path_time"`
	MeleeFOV            *float64        `json:"melee_fov"`
	MinPathTime         *float64        `json:"min_path_time"`
	OnAttack            json.RawMessage `json:"on_attack"`
	OnKill              json.RawMessage `json:"on_kill"`
	OuterBoundaryInc    *float64        `json:"outer_boundary_time_increase"`
	PathFailInc         *float64        `json:"path_fail_time_increase"`
	PathInnerBoundary   *float64        `json:"path_inner_boundary"`
	PathOuterBoundary   *float64        `json:"path_outer_boundary"`
	RandomStopInterval  int             `json:"random_stop_interval"`
	ReachMultiplier     *float64        `json:"reach_multiplier"`
	RequireCompletePath bool            `json:"require_complete_path"`
	SetPersistent       bool            `json:"set_persistent"`
	SpeedMultiplier     *float64        `json:"speed_multiplier"`
	TrackTarget         bool            `json:"track_target"`
	XMaxRotation        *float64        `json:"x_max_rotation"`
	YMaxHeadRotation    *float64        `json:"y_max_head_rotation"`
	CanSpreadOnFire     bool            `json:"can_spread_on_fire"`
	HorizontalReach     *float64        `json:"horizontal_reach"`
}

func or(p *float64, def float64) float64 {
	if p != nil {
		return *p
	}
	return def
}

func newMeleeBase(spec mob.GoalSpec, v rawMelee) (meleeBase, error) {
	g := meleeBase{
		GoalBase:        mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)},
		key:             spec.Key,
		attackOnce:      v.AttackOnce,
		requireComplete: v.RequireCompletePath,
		trackTarget:     v.TrackTarget,
		randomStop:      v.RandomStopInterval,
		cooldownTime:    sense.Ticks(or(v.CooldownTime, 1)),
		minPath:         sense.Ticks(or(v.MinPathTime, 0.2)),
		maxPath:         sense.Ticks(or(v.MaxPathTime, 0.55)),
		pathFailInc:     sense.Ticks(or(v.PathFailInc, 0.75)),
		innerInc:        sense.Ticks(or(v.InnerBoundaryInc, 0.25)),
		outerInc:        sense.Ticks(or(v.OuterBoundaryInc, 0.5)),
		fov:             or(v.MeleeFOV, 90),
		yMaxHead:        or(v.YMaxHeadRotation, 30),
		xMaxRot:         or(v.XMaxRotation, 30),
		speed:           or(v.SpeedMultiplier, 1),
		attackTypes:     v.AttackTypes,
		setPersistent:   v.SetPersistent,
		spreadFire:      v.CanSpreadOnFire,
	}
	inner, outer := or(v.PathInnerBoundary, 16), or(v.PathOuterBoundary, 32)
	g.innerSq, g.outerSq = inner*inner, outer*outer
	g.cooldown = g.cooldownTime
	var err error
	if g.onAttack, err = sense.ParseTrigger(v.OnAttack); err != nil {
		return g, fmt.Errorf("%v: on_attack: %w", spec.Key, err)
	}
	if g.onKill, err = sense.ParseTrigger(v.OnKill); err != nil {
		return g, fmt.Errorf("%v: on_kill: %w", spec.Key, err)
	}
	return g, nil
}

func (g *meleeBase) lockedTarget(m *mob.Mob) (world.Entity, bool) {
	if g.target == nil {
		return nil, false
	}
	return g.target.Entity(m.Tx())
}

// CanUse is MeleeAttackBaseGoal::canUse.
func (g *meleeBase) CanUse(m *mob.Mob) bool {
	if !m.HasComponent("minecraft:attack") {
		return false
	}
	t, ok := m.Target()
	if !ok || !sense.Alive(t) {
		return false
	}
	if g.attackTypes != "" && !hasFamily(t, g.attackTypes) {
		return false
	}
	g.target = t.H()
	p := m.Navigation().CreatePathToEntity(m, t)
	g.path = p
	if p == nil || (g.requireComplete && !p.Complete()) {
		return false
	}
	if g.setPersistent && sense.IsPlayer(t) {
		m.SetPersistent(true)
	}
	return true
}

// CanContinueToUse is MeleeAttackBaseGoal::canContinueToUse.
func (g *meleeBase) CanContinueToUse(m *mob.Mob) bool {
	if g.attackOnce && g.hitResult {
		m.ResetTarget()
		return false
	}
	if !m.HasComponent("minecraft:attack") {
		return false
	}
	if g.randomStop > 0 && m.Rand().IntN(g.randomStop) == 0 {
		m.ResetTarget()
		return false
	}
	t, ok := m.Target()
	if !ok {
		return false
	}
	lt, ok := g.lockedTarget(m)
	if !ok || lt.H() != t.H() || !sense.Alive(t) {
		return false
	}
	if sense.IsPlayer(t) && sense.DifficultyID(m.Tx()) == 0 {
		m.ResetTarget()
		return false
	}
	if g.trackTarget {
		return true
	}
	return !m.Navigation().Done()
}

// Start is MeleeAttackBaseGoal::start.
func (g *meleeBase) Start(m *mob.Mob) {
	if g.path != nil {
		m.Navigation().MoveAlong(m, g.path, g.speed)
		g.path = nil
	}
	g.ticker = g.pathFailInc
	g.hitResult = false
	m.UpdateMeta(func(meta *mob.Meta) { meta.Aggressive = true })
}

// Stop is MeleeAttackBaseGoal::stop.
func (g *meleeBase) Stop(m *mob.Mob) {
	g.target = nil
	m.Navigation().Stop(m)
	m.UpdateMeta(func(meta *mob.Meta) { meta.Aggressive = false })
}

// moveToTarget is MeleeAttackBaseGoal::_attemptMoveToTargetPosition.
func (g *meleeBase) moveToTarget(m *mob.Mob, t world.Entity, canReach bool, distSq float64) {
	m.LookControl().SetLookAtEntity(t, g.xMaxRot, g.yMaxHead)
	g.ticker--
	if !g.trackTarget && !sense.CanSee(m, t) {
		return
	}
	pos := t.Position()
	pos[1] = sense.BBox(t).Min()[1]
	g.lastPos = pos
	if g.ticker > 0 {
		return
	}
	// Recalculate with no path, rarely when the path already ends at the
	// target, and otherwise only while the target is out of reach.
	recalc := true
	if p := m.Navigation().Path(); p != nil {
		if p.End() == cube.PosFromVec3(pos) {
			recalc = m.Rand().Float64() < 0.05
		} else {
			recalc = !canReach
		}
	}
	if !recalc {
		return
	}
	g.ticker = g.minPath
	if g.maxPath > g.minPath {
		g.ticker += m.Rand().IntN(g.maxPath - g.minPath)
	}
	switch {
	case distSq > g.outerSq:
		g.ticker += g.outerInc
	case distSq > g.innerSq:
		g.ticker += g.innerInc
	}
	if !m.Navigation().MoveToEntity(m, t, g.speed) {
		g.ticker += g.pathFailInc
	}
}

// attackTarget is MeleeAttackBaseGoal::_attemptAttackTarget.
func (g *meleeBase) attackTarget(m *mob.Mob, t world.Entity, canAttack, lineOfSight bool) {
	if !canAttack || !lineOfSight || !sense.WithinFOV(m, sense.Center(t), g.fov) {
		return
	}
	g.cooldown = g.cooldownTime
	for _, v := range m.Tx().Viewers(m.Position()) {
		v.ViewEntityAction(m, entity.SwingArmAction{})
	}
	atk := readAttack(m)
	if atk.hasDamage {
		m.Attributes().SetBase(mob.AttrAttackDamage, atk.damage.Roll(m.Rand()))
	}
	g.hitResult = DoHurtTarget(m, t)
	if g.hitResult {
		if g.spreadFire && m.OnFireDuration() > 0 {
			f := fireChance[sense.DifficultyID(m.Tx())]
			main, _ := m.HeldItems()
			if m.Rand().Float64() < f && main.Empty() {
				if fl, ok := t.(interface{ SetOnFire(time.Duration) }); ok && int(f) > 0 {
					fl.SetOnFire(time.Duration(2*int(f)) * time.Second)
				}
			}
		}
		ctx := m.Ctx()
		ctx.Other, ctx.Target = t, t
		g.onAttack.Fire(m, ctx)
		if !sense.Alive(t) {
			g.onKill.Fire(m, ctx)
		}
		if atk.effect != nil && sense.Alive(t) {
			if l, ok := t.(entity.Living); ok && atk.effectTicks > 0 {
				l.AddEffect(effect.New(atk.effect, 1, time.Duration(atk.effectTicks)*time.Second/20))
			}
		}
	}
	if g.attackOnce && g.hitResult {
		m.ResetTarget()
	}
	if a, ok := mob.ComponentOf[sense.AngerTimer](m); ok {
		a.RestartAngerTimer(m)
	}
}

// fireChance is the chance (and twice the seconds) a burning attacker sets
// its target on fire, by difficulty (BDS _attemptAttackTarget).
var fireChance = [4]float64{0, 0.42, 1.14, 1.92}

// meleeAttack is minecraft:behavior.melee_attack (BDS MeleeAttackGoal): the
// target is in reach within reach_multiplier mob widths.
type meleeAttack struct {
	meleeBase
	reachMultiplier float64 // +424
}

func newMeleeAttack(spec mob.GoalSpec) (mob.Goal, error) {
	var v rawMelee
	if err := spec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	b, err := newMeleeBase(spec, v)
	if err != nil {
		return nil, err
	}
	return &meleeAttack{meleeBase: b, reachMultiplier: or(v.ReachMultiplier, 2)}, nil
}

// reachSq is MeleeAttackGoal::_getAttackReachSqr.
func (g *meleeAttack) reachSq(m *mob.Mob, t world.Entity) float64 {
	w := math.Max(sense.Width(m), 0.5)
	r := w * g.reachMultiplier
	return r*r + sense.Width(t)
}

// Tick is MeleeAttackGoal::tick.
func (g *meleeAttack) Tick(m *mob.Mob) {
	t, ok := g.lockedTarget(m)
	if !ok {
		return
	}
	tb, mb := sense.BBox(t), m.BBox()
	tpos := t.Position()
	tpos[1] = tb.Min()[1]
	g.cooldown = max(g.cooldown-1, 0)
	reach := g.reachSq(m, t)
	horiz := sense.HorizontalDistSq(tpos, m.Pos())
	dist3 := tpos.Sub(m.Pos()).LenSqr()
	canReach := ((horiz <= reach && tb.Max()[1] > mb.Min()[1] && tb.Min()[1] < mb.Max()[1]) || dist3 <= reach) && g.cooldown <= 0
	los := sense.LineOfSight(m.Tx(), m.EyePosition(), sense.EyePos(t))
	g.moveToTarget(m, t, canReach, dist3)
	g.attackTarget(m, t, canReach, los)
}

// AppendDebugInfo ...
func (g *meleeAttack) AppendDebugInfo(b *strings.Builder) { b.WriteString("MeleeAttack") }

// meleeBoxAttack is minecraft:behavior.melee_box_attack (BDS
// MeleeBoxAttackGoal): the target is in reach when its box touches the mob's
// box grown horizontally by horizontal_reach.
type meleeBoxAttack struct {
	meleeBase
	horizontalReach float64 // +408
}

func newMeleeBoxAttack(spec mob.GoalSpec) (mob.Goal, error) {
	var v rawMelee
	if err := spec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	b, err := newMeleeBase(spec, v)
	if err != nil {
		return nil, err
	}
	return &meleeBoxAttack{meleeBase: b, horizontalReach: or(v.HorizontalReach, 0.8)}, nil
}

// AttackBox is the box a melee_box_attack mob hits in.
func (g *meleeBoxAttack) AttackBox(m *mob.Mob) cube.BBox {
	b := m.BBox()
	r := g.horizontalReach
	return cube.Box(b.Min()[0]-r, b.Min()[1], b.Min()[2]-r, b.Max()[0]+r, b.Max()[1], b.Max()[2]+r)
}

// Tick is MeleeBoxAttackGoal::tick.
func (g *meleeBoxAttack) Tick(m *mob.Mob) {
	t, ok := g.lockedTarget(m)
	if !ok {
		return
	}
	g.cooldown = max(g.cooldown-1, 0)
	canAttack := g.AttackBox(m).IntersectsWith(sense.BBox(t)) && g.cooldown <= 0
	distSq := sense.HorizontalDistSq(t.Position(), m.Pos())
	los := sense.LineOfSight(m.Tx(), m.EyePosition(), sense.EyePos(t))
	g.moveToTarget(m, t, canAttack, distSq)
	g.attackTarget(m, t, canAttack, los)
}

// AppendDebugInfo ...
func (g *meleeBoxAttack) AppendDebugInfo(b *strings.Builder) { b.WriteString("MeleeBoxAttack") }

// attackDef is the part of minecraft:attack the melee goals read.
type attackDef struct {
	damage      sense.Range
	hasDamage   bool
	effect      effect.LastingType
	effectTicks int
}

func readAttack(m *mob.Mob) attackDef {
	var a attackDef
	raw, ok := m.ComponentJSON("minecraft:attack")
	if !ok {
		return a
	}
	var v struct {
		Damage         json.RawMessage `json:"damage"`
		EffectName     string          `json:"effect_name"`
		EffectDuration float64         `json:"effect_duration"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return a
	}
	if len(v.Damage) > 0 {
		if r, err := sense.ParseRange(v.Damage, sense.Range{}); err == nil {
			a.damage, a.hasDamage = r, true
		}
	}
	a.effect = effectByName(v.EffectName)
	a.effectTicks = sense.Ticks(v.EffectDuration)
	return a
}

// effectByName maps BDS effect names to Dragonfly lasting effects.
func effectByName(name string) effect.LastingType {
	switch name {
	case "poison":
		return effect.Poison
	case "wither":
		return effect.Wither
	case "slowness":
		return effect.Slowness
	case "weakness":
		return effect.Weakness
	case "hunger":
		return effect.Hunger
	case "nausea":
		return effect.Nausea
	case "blindness":
		return effect.Blindness
	case "mining_fatigue":
		return effect.MiningFatigue
	case "darkness":
		return effect.Darkness
	case "levitation":
		return effect.Levitation
	}
	return nil
}

// DoHurtTarget is the mob's melee hit (BDS Mob::doHurtTarget): the attack
// damage attribute plus the held weapon, scaled by difficulty against
// players, then knock back. It reports whether damage was dealt.
func DoHurtTarget(m *mob.Mob, t world.Entity) bool {
	l, ok := t.(entity.Living)
	if !ok {
		return false
	}
	dmg := m.Attr(mob.AttrAttackDamage)
	// BDS Mob::doHurtTarget: + int(weapon damage + Sharpness bonus).
	main, _ := m.HeldItems()
	dmg += loot.WeaponDamage(main)
	if sense.IsPlayer(t) {
		switch sense.DifficultyID(m.Tx()) {
		case 0:
			dmg = 0
		case 1:
			dmg = math.Min(dmg/2+1, dmg)
		case 3:
			dmg *= 1.5
		}
	}
	if dmg <= 0 {
		return false
	}
	n, res := l.Hurt(dmg, entity.AttackDamageSource{Attacker: m})
	if !res.Accepted() && n <= 0 {
		return false
	}
	// minecraft:apply_knockback_rules may replace the default knock back.
	if k, ok := mob.ComponentOf[sense.KnockbackApplier](m); !ok || !k.ApplyKnockback(m, t, n) {
		l.KnockBack(m.Position(), 0.4+loot.KnockbackBonus(main), 0.4)
	}
	m.SetLastHurtMob(t)
	loot.PostHit(main, t)
	return true
}

func hasFamily(e world.Entity, f string) bool {
	if m, ok := mob.Of(e); ok {
		return m.HasFamily(f)
	}
	return f == "player" && sense.IsPlayer(e)
}
