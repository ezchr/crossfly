//go:build crossfly_full

package hostile

import (
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/javamob"
)

// portGoal is a goal of this package written against *mob.Mob (the ports of
// milestone 1): Flags and CanUse, plus any of CanContinueToUse, Start, Stop,
// Tick, IsInterruptable, RequiresUpdateEveryTick and IsTargetGoal.
type portGoal interface {
	CanUse(m *mob.Mob) bool
	Flags() mob.ControlFlags
}

// goalOf runs a port goal in the base's GoalSelector.
func goalOf(g portGoal) javamob.Goal {
	f := g.Flags()
	var flags javamob.Flags
	if f&mob.FlagMove != 0 {
		flags |= javamob.FlagMove
	}
	if f&mob.FlagLook != 0 {
		flags |= javamob.FlagLook
	}
	if f&mob.FlagJump != 0 {
		flags |= javamob.FlagJump
	}
	if t, ok := g.(interface{ IsTargetGoal() bool }); ok && t.IsTargetGoal() {
		flags |= javamob.FlagTarget
	}
	return &adaptedGoal{GoalBase: javamob.GoalBase{Control: flags}, g: g}
}

type adaptedGoal struct {
	javamob.GoalBase
	g portGoal
}

func (a *adaptedGoal) CanUse(m *javamob.Mob) bool { return a.g.CanUse(m.Mob) }

func (a *adaptedGoal) CanContinueToUse(m *javamob.Mob) bool {
	if c, ok := a.g.(interface{ CanContinueToUse(*mob.Mob) bool }); ok {
		return c.CanContinueToUse(m.Mob)
	}
	return a.g.CanUse(m.Mob)
}

func (a *adaptedGoal) IsInterruptable() bool {
	if i, ok := a.g.(interface{ IsInterruptable() bool }); ok {
		return i.IsInterruptable()
	}
	return true
}

func (a *adaptedGoal) RequiresUpdateEveryTick() bool {
	if e, ok := a.g.(interface{ RequiresUpdateEveryTick() bool }); ok {
		return e.RequiresUpdateEveryTick()
	}
	return false
}

func (a *adaptedGoal) Start(m *javamob.Mob) {
	if s, ok := a.g.(interface{ Start(*mob.Mob) }); ok {
		s.Start(m.Mob)
	}
}

func (a *adaptedGoal) Stop(m *javamob.Mob) {
	if s, ok := a.g.(interface{ Stop(*mob.Mob) }); ok {
		s.Stop(m.Mob)
	}
}

func (a *adaptedGoal) Tick(m *javamob.Mob) {
	if t, ok := a.g.(interface{ Tick(*mob.Mob) }); ok {
		t.Tick(m.Mob)
	}
}
