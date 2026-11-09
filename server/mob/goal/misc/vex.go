//go:build crossfly_full

package misc

import (
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// The goals Vex::initializeComponents adds to every vex (they are not in its
// entity file), with their priorities.
const (
	vexRandomMoveKey      = "minecraft:behavior.vex_random_move"
	vexCopyOwnerTargetKey = "minecraft:behavior.vex_copy_owner_target"
	vexRandomMovePriority = 8
	vexCopyTargetPriority = 2
)

func init() {
	mob.RegisterGoal(vexRandomMoveKey, newVexRandomMove)
	mob.RegisterGoal(vexCopyOwnerTargetKey, newVexCopyOwnerTarget)
	mob.RegisterInitHook(mob.InitHook{Name: "misc/vex_goals", Init: func(m *mob.Mob) {
		if m.Identifier() != "minecraft:vex" {
			return
		}
		if _, ok := m.Goals().Goal(vexRandomMoveKey); !ok {
			m.Goals().Add(vexRandomMoveKey, vexRandomMovePriority, &vexRandomMove{GoalBase: mob.GoalBase{Control: mob.FlagMove | mob.FlagLook}})
		}
		if _, ok := m.Goals().Goal(vexCopyOwnerTargetKey); !ok {
			m.Goals().Add(vexCopyOwnerTargetKey, vexCopyTargetPriority, &vexCopyOwnerTarget{})
		}
	}})
}

// vexRandomMove is minecraft:behavior.vex_random_move (BDS
// VexRandomMoveGoal): while its move control has nowhere to go, with a
// 1-in-7 chance a tick the vex picks an empty block up to 7 away sideways
// and 5 up or down (3 tries) and flies there at a quarter of its speed,
// looking at it when it has no target.
type vexRandomMove struct{ mob.GoalBase }

func newVexRandomMove(spec mob.GoalSpec) (mob.Goal, error) {
	return &vexRandomMove{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}}, nil
}

// CanUse ...
func (g *vexRandomMove) CanUse(m *mob.Mob) bool {
	return !m.MoveControl().HasWanted() && m.Rand().IntN(7) == 0
}

// Tick picks the spot around the vex (it has no bound origin in Bedrock:
// Mob::getBoundOrigin falls back to its block).
func (g *vexRandomMove) Tick(m *mob.Mob) {
	origin := cube.PosFromVec3(m.Position())
	r := m.Rand()
	for range 3 {
		p := origin.Add(cube.Pos{r.IntN(15) - 7, r.IntN(11) - 5, r.IntN(15) - 7})
		if p.OutOfBounds(m.Tx().Range()) || !isEmptyBlock(m.Tx(), p) {
			continue
		}
		at := p.Vec3().Add(mgl64.Vec3{0.5, 0.5, 0.5})
		m.MoveControl().SetWantedPosition(at, 0.25)
		if _, ok := m.Target(); !ok {
			m.LookControl().SetLookAt(at, 180, 20)
		}
		return
	}
}

// AppendDebugInfo ...
func (g *vexRandomMove) AppendDebugInfo(b *strings.Builder) { b.WriteString("VexRandomMoveGoal") }

// isEmptyBlock is BlockSource::isEmptyBlock: air.
func isEmptyBlock(tx *world.Tx, p cube.Pos) bool {
	_, ok := tx.Block(p).(block.Air)
	return ok
}

// vexCopyOwnerTarget is minecraft:behavior.vex_copy_owner_target (BDS
// VexCopyOwnerTargetGoal, a TargetGoal): the vex takes the target of the
// mob that owns it (its evoker) when it can attack it, and keeps it as
// TargetGoal::canContinueToUse does without needing to see it.
type vexCopyOwnerTarget struct{ mob.GoalBase }

func newVexCopyOwnerTarget(spec mob.GoalSpec) (mob.Goal, error) {
	return &vexCopyOwnerTarget{GoalBase: mob.GoalBase{Control: spec.FlagsOr(0)}}, nil
}

// IsTargetGoal ...
func (g *vexCopyOwnerTarget) IsTargetGoal() bool { return true }

// ownerTarget is the target of m's owner.
func ownerTarget(m *mob.Mob) (world.Entity, bool) {
	o := m.Owner()
	if o == nil {
		return nil, false
	}
	oe, ok := o.Entity(m.Tx())
	if !ok {
		return nil, false
	}
	om, ok := mob.Of(oe)
	if !ok {
		return nil, false
	}
	return om.Target()
}

// CanUse is VexCopyOwnerTargetGoal::canUse: the owner has a target the vex
// can attack (Mob::canAttack).
func (g *vexCopyOwnerTarget) CanUse(m *mob.Mob) bool {
	t, ok := ownerTarget(m)
	return ok && t.H() != m.H() && sense.Alive(t) && !sense.Invulnerable(t)
}

// CanContinueToUse is TargetGoal::canContinueToUse with must_see off: the
// target lives, is not a player on peaceful, stays within the follow range
// and can be hurt.
func (g *vexCopyOwnerTarget) CanContinueToUse(m *mob.Mob) bool {
	t, ok := m.Target()
	if !ok {
		return false
	}
	r := m.Attr(mob.AttrFollowRange)
	if !sense.Alive(t) || (sense.IsPlayer(t) && sense.DifficultyID(m.Tx()) == 0) ||
		m.Position().Sub(t.Position()).LenSqr() > r*r || sense.Invulnerable(t) {
		m.ResetTarget()
		return false
	}
	return true
}

// Start is VexCopyOwnerTargetGoal::start: the owner's target becomes the
// vex's.
func (g *vexCopyOwnerTarget) Start(m *mob.Mob) {
	if t, ok := ownerTarget(m); ok {
		m.SetTarget(t)
	}
}

// AppendDebugInfo ...
func (g *vexCopyOwnerTarget) AppendDebugInfo(b *strings.Builder) {
	b.WriteString(" VexCopyOwnerTargetGoal")
}
