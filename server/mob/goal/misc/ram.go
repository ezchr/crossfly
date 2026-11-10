//go:build crossfly_full

package misc

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/goal/combat"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/kit"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// ramAttack is minecraft:behavior.ram_attack (goat, BDS RamAttackGoal):
//
//  1. the target is between min_ram_distance (4) and ram_distance (7)
//     blocks away horizontally, the cooldown is over and a straight charge
//     path exists (RamAttackGoal::_hasChargePath: every block on the line
//     is pathable);
//  2. the goat walks to the ram start at run_speed (BDS default 1), faces
//     the target and lowers its head for 20 ticks (pre_ram_sound,
//     ram_attack actor flag); the ram ends at the centre of the block the
//     target stood in then (RamAttackGoal::_initiateRamAttack);
//  3. it charges at ram_speed (2) to that end, for at most 140 ticks;
//  4. touching the target (_tryKnockbackTarget) hurts it, plays
//     ram_impact_sound and knocks it back with knockback_force (5) scaled by
//     the goat's speed in m/s x 1/12, clamped to 0.2..3
//     (baby_knockback_modifier 1/3 for babies), knockback_height (0.1);
//  5. a goat that reaches the end (within 0.7) without hitting anything
//     still tries for 2 ticks, then loses a horn if the block one past the
//     end is a log, stone, packed ice or one of some ores
//     (RamAttackGoalUtils::RamGoalItemDropperImpl, goat_horns.go).
//
// The goal then waits cooldown_range (10..20 s) before the next ram.
// on_start fires when the charge begins (the vanilla goat adds its
// attack_cooldown group).
type ramAttack struct {
	mob.GoalBase
	runSpeed, ramSpeed  float64
	minDist, ramDist    float64
	force, height, baby float64
	preSound, impactSnd string
	cooldown            kit.Range
	onStart             []kit.Trigger
	nextRam             int64
	state               int
	timer               int
	end, dir            mgl64.Vec3
	target              *world.EntityHandle
	init                bool
	// hornDrop is RamGoalItemDropperImpl's flag: the block past the end is
	// one a goat loses a horn on.
	hornDrop bool
}

const (
	ramIdle = iota
	ramApproach
	ramPrepare
	ramCharge
	// ramFinish is BDS state 5: at the end, 2 more ticks of trying to hit.
	ramFinish
)

// ramMaxChargeTicks is how long a charge may take (RamAttackGoal::tick).
const ramMaxChargeTicks = 140

func newRamAttack(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		RunSpeed        *float64        `json:"run_speed"`
		RamSpeed        *float64        `json:"ram_speed"`
		MinRamDistance  *float64        `json:"min_ram_distance"`
		RamDistance     *float64        `json:"ram_distance"`
		KnockbackForce  *float64        `json:"knockback_force"`
		KnockbackHeight *float64        `json:"knockback_height"`
		BabyModifier    *float64        `json:"baby_knockback_modifier"`
		PreRamSound     string          `json:"pre_ram_sound"`
		RamImpactSound  string          `json:"ram_impact_sound"`
		CooldownRange   json.RawMessage `json:"cooldown_range"`
		OnStart         json.RawMessage `json:"on_start"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	g := &ramAttack{
		GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)},
		runSpeed: kit.Or(v.RunSpeed, 1), ramSpeed: kit.Or(v.RamSpeed, 2),
		minDist: kit.Or(v.MinRamDistance, 4), ramDist: kit.Or(v.RamDistance, 7),
		force: kit.Or(v.KnockbackForce, 5), height: kit.Or(v.KnockbackHeight, 0.1), baby: kit.Or(v.BabyModifier, 1.0/3),
		preSound: v.PreRamSound, impactSnd: v.RamImpactSound,
	}
	var err error
	if g.cooldown, err = kit.ParseRange(v.CooldownRange, kit.Range{Min: 10, Max: 20}); err != nil {
		return nil, err
	}
	if g.onStart, err = kit.ParseTriggers(v.OnStart); err != nil {
		return nil, err
	}
	return g, nil
}

func (g *ramAttack) targetOf(m *mob.Mob) (world.Entity, bool) {
	t, ok := m.Target()
	if !ok || !kit.Alive(t) || kit.Creative(t) {
		return nil, false
	}
	return t, true
}

// CanUse ...
func (g *ramAttack) CanUse(m *mob.Mob) bool {
	if !g.init {
		g.init = true
		g.resetCooldown(m)
	}
	if m.Ticks() < g.nextRam || !m.OnGround() {
		return false
	}
	t, ok := g.targetOf(m)
	if !ok {
		return false
	}
	d := math.Sqrt(kit.HDistSq(m.Position(), t.Position()))
	if d < g.minDist || d > g.ramDist+2 {
		return false
	}
	return hasChargePath(m, t.Position())
}

func (g *ramAttack) resetCooldown(m *mob.Mob) {
	g.nextRam = m.Ticks() + int64(kit.RollTicks(m, g.cooldown))
}

// hasChargePath is RamAttackGoal::_hasChargePath: every block on the line
// from the goat to the target is free to walk through on solid ground.
func hasChargePath(m *mob.Mob, to mgl64.Vec3) bool {
	tx := m.Tx()
	from := m.Position()
	d := to.Sub(from)
	d[1] = 0
	n := int(math.Ceil(d.Len()))
	if n == 0 {
		return true
	}
	step := d.Mul(1 / float64(n))
	for i := 1; i <= n; i++ {
		p := cube.PosFromVec3(from.Add(step.Mul(float64(i))))
		if kit.Solid(tx, p) || kit.Solid(tx, p.Side(cube.FaceUp)) {
			return false
		}
		if !kit.Solid(tx, p.Side(cube.FaceDown)) && !kit.Solid(tx, p.Add(cube.Pos{0, -2, 0})) {
			return false
		}
	}
	return true
}

// CanContinueToUse ...
func (g *ramAttack) CanContinueToUse(m *mob.Mob) bool {
	if g.state == ramIdle {
		return false
	}
	if g.state == ramCharge || g.state == ramFinish {
		return true
	}
	t, ok := g.targetOf(m)
	return ok && t.H() == g.target
}

// Start ...
func (g *ramAttack) Start(m *mob.Mob) {
	t, _ := g.targetOf(m)
	g.target = t.H()
	g.state = ramApproach
	d := math.Sqrt(kit.HDistSq(m.Position(), t.Position()))
	if d <= g.ramDist {
		g.prepare(m, t)
		return
	}
	// Walk until within ram_distance.
	dir := m.Position().Sub(t.Position())
	dir[1] = 0
	if dir.Len() > 0 {
		dir = dir.Normalize()
	}
	kit.MoveTo(m, t.Position().Add(dir.Mul(g.ramDist-0.5)), g.runSpeed)
}

func (g *ramAttack) prepare(m *mob.Mob, t world.Entity) {
	g.state, g.timer = ramPrepare, 20
	tp := cube.PosFromVec3(t.Position())
	g.end = mgl64.Vec3{float64(tp[0]) + 0.5, float64(tp[1]), float64(tp[2]) + 0.5}
	kit.Stop(m)
	kit.Flag(m, protocol.EntityDataFlagRamAttack, true)
	kit.Sound(m, g.preSound)
}

func (g *ramAttack) charge(m *mob.Mob, t world.Entity) {
	g.state, g.timer = ramCharge, 0
	start := m.Position()
	d := g.end.Sub(start)
	d[1] = 0
	if d.Len() > 0 {
		g.dir = d.Normalize()
	}
	g.hornDrop = goatHornBlockAhead(m.Tx(), start, g.end)
	kit.FireAll(m, g.onStart, kit.OtherCtx(m, t))
	kit.MoveTo(m, g.end, g.ramSpeed)
}

// Tick ...
func (g *ramAttack) Tick(m *mob.Mob) {
	t, ok := g.target.Entity(m.Tx())
	switch g.state {
	case ramApproach:
		if !ok {
			g.state = ramIdle
			return
		}
		kit.LookAtEntity(m, t)
		if math.Sqrt(kit.HDistSq(m.Position(), t.Position())) <= g.ramDist || m.Navigation().Done() {
			g.prepare(m, t)
		}
	case ramPrepare:
		if !ok {
			g.state = ramIdle
			return
		}
		kit.LookAtEntity(m, t)
		m.SetYaw(kit.YawTowards(m.Position(), t.Position()))
		if g.timer--; g.timer <= 0 {
			g.charge(m, t)
		}
	case ramCharge:
		g.timer++
		kit.LookAt(m, g.end.Add(mgl64.Vec3{0, m.EyeHeight()}))
		if g.tryHit(m, t, ok) {
			return
		}
		if g.reachedEnd(m) {
			kit.Stop(m)
			g.state, g.timer = ramFinish, 2
			return
		}
		if m.Navigation().Done() || g.timer > ramMaxChargeTicks || m.Physics().HorizontalCollision && g.timer > 5 {
			g.state = ramIdle
		}
	case ramFinish:
		if g.tryHit(m, t, ok) {
			return
		}
		if g.timer--; g.timer <= 0 {
			// RamGoalItemDropperImpl::tryDropHorn at the end of the ram.
			if g.hornDrop {
				goatDropHorn(m, g.end.Add(mgl64.Vec3{0, 0.5}))
			}
			g.hornDrop = false
			g.state = ramIdle
		}
	}
}

// tryHit is RamAttackGoal::_tryKnockbackTarget: touching the target ends the
// ram with the hit, and no horn drops (dontDropHorn).
func (g *ramAttack) tryHit(m *mob.Mob, t world.Entity, ok bool) bool {
	if !ok || !kit.Alive(t) || !m.BBox().Grow(0.2).IntersectsWith(kit.BBox(t)) {
		return false
	}
	g.impact(m, t)
	g.hornDrop = false
	g.state = ramIdle
	return true
}

// reachedEnd reports whether the goat's centre is within 0.7 of the centre
// of the end block.
func (g *ramAttack) reachedEnd(m *mob.Mob) bool {
	c := m.Position().Add(mgl64.Vec3{0, m.BBox().Height() / 2})
	return c.Sub(g.end.Add(mgl64.Vec3{0, 0.5})).Len() < 0.7
}

// impact is RamAttackGoal::_tryKnockbackTarget.
func (g *ramAttack) impact(m *mob.Mob, t world.Entity) {
	v := m.Velocity()
	speed := math.Hypot(v[0], v[2]) * 20 // m/s
	f := math.Min(math.Max(speed*(1.0/12), 0.2), 3) * g.force
	if m.Baby() {
		f *= g.baby
	}
	combat.DoHurtTarget(m, t)
	if l, ok := t.(interface {
		KnockBack(src mgl64.Vec3, force, height float64)
	}); ok && kit.Alive(t) {
		// DF knock back is a velocity: the BDS force is in blocks over the
		// impulse, scaled down like a 0.4 melee knock back is.
		l.KnockBack(m.Position(), f*0.4/2.5, g.height+0.3)
	}
	kit.Sound(m, g.impactSnd)
}

// Stop ...
func (g *ramAttack) Stop(m *mob.Mob) {
	g.state = ramIdle
	g.target = nil
	g.hornDrop = false
	kit.Flag(m, protocol.EntityDataFlagRamAttack, false)
	kit.Stop(m)
	g.resetCooldown(m)
}

// AppendDebugInfo ...
func (g *ramAttack) AppendDebugInfo(b *strings.Builder) {
	b.WriteString("RamAttack")
	if g.state != ramIdle {
		b.WriteString([]string{"", " approach", " prepare", " charge", " finish"}[g.state])
	}
}
