//go:build crossfly_full

package boss

import (
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/mob"
	bc "github.com/df-mc/dragonfly/server/mob/component/boss"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// --- behavior.wither_random_attack_pos_goal ---

// witherAttackPos is WitherRandomAttackPosGoal (a RandomStrollGoal with
// speed 1, xz 10, y 7): when the wither wants to move (phase 1, after a
// volley), it relocates to a spot 10 blocks from its target in a random
// direction, at its own height; then it waits 20 ticks before shooting.
type witherAttackPos struct {
	mob.GoalBase
	ticks int
}

func newWitherAttackPos(spec mob.GoalSpec) (mob.Goal, error) {
	return &witherAttackPos{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)}}, nil
}

func (g *witherAttackPos) CanUse(m *mob.Mob) bool {
	if m.Identifier() != bc.WitherID {
		return false
	}
	w := bc.WitherOf(m)
	t, ok := m.Target()
	return ok && bc.Living(t) && w.Phase1 && w.WantsToMove && !w.Pathing
}

func (g *witherAttackPos) CanContinueToUse(m *mob.Mob) bool {
	w := bc.WitherOf(m)
	t, ok := m.Target()
	if !ok || !bc.Living(t) || g.ticks > 100 {
		return false
	}
	d := w.PathTarget.Sub(m.Position())
	d[1] = 0
	return d.Len() > 1.5
}

func (g *witherAttackPos) Start(m *mob.Mob) {
	w := bc.WitherOf(m)
	t, _ := m.Target()
	a := m.Rand().Float64() * 2 * math.Pi
	dir := mgl64.Vec3{math.Cos(a), 0, math.Sin(a)}
	tp := t.Position()
	w.PathTarget = mgl64.Vec3{tp[0] + dir[0]*10, m.Position()[1], tp[2] + dir[2]*10}
	w.Pathing = true
	g.ticks = 0
}

func (g *witherAttackPos) Tick(*mob.Mob) { g.ticks++ }

func (g *witherAttackPos) Stop(m *mob.Mob) {
	w := bc.WitherOf(m)
	w.WantsToMove, w.Pathing = false, false
	w.ShotDelay = 20
}

func (g *witherAttackPos) AppendDebugInfo(b *strings.Builder) {
	b.WriteString("Wither random attack pos")
}

// --- behavior.wither_target_highest_damage ---

// witherHighestDamage is WitherTargetHighestDamage: the player that dealt
// the wither the most damage (within follow range, attackable) is the
// target, switched as soon as another player has dealt more.
type witherHighestDamage struct {
	mob.GoalBase
	pick *world.EntityHandle
}

func newWitherHighestDamage(spec mob.GoalSpec) (mob.Goal, error) {
	return &witherHighestDamage{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)}}, nil
}

// IsTargetGoal ...
func (*witherHighestDamage) IsTargetGoal() bool { return true }

// highest is the player with the most damage that may be attacked.
func (g *witherHighestDamage) highest(m *mob.Mob) (world.Entity, bool) {
	if m.Identifier() != bc.WitherID {
		return nil, false
	}
	w := bc.WitherOf(m)
	tx := m.Tx()
	rng := m.Attr(mob.AttrFollowRange)
	if rng < 70 {
		rng = 70 // WitherBoss::reloadHardcoded FOLLOW_RANGE
	}
	var best world.Entity
	bestD := 0.0
	for h, dmg := range w.Damage {
		e, ok := h.Entity(tx)
		if !ok {
			delete(w.Damage, h)
			continue
		}
		if !bc.AttackablePlayer(e) || e.Position().Sub(m.Position()).LenSqr() > rng*rng {
			continue
		}
		if dmg > bestD {
			best, bestD = e, dmg
		}
	}
	return best, best != nil
}

func (g *witherHighestDamage) CanUse(m *mob.Mob) bool {
	e, ok := g.highest(m)
	if !ok || e.H() == m.TargetHandle() {
		return false
	}
	g.pick = e.H()
	return true
}

func (g *witherHighestDamage) CanContinueToUse(m *mob.Mob) bool {
	e, ok := g.highest(m)
	return ok && e.H() == m.TargetHandle() && sense.Alive(e)
}

func (g *witherHighestDamage) Start(m *mob.Mob) {
	if e, ok := g.pick.Entity(m.Tx()); ok {
		m.SetTarget(e)
	}
}

func (g *witherHighestDamage) AppendDebugInfo(b *strings.Builder) {
	b.WriteString("Wither target highest damage")
}
