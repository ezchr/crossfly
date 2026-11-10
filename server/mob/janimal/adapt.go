//go:build crossfly_full

package janimal

import "github.com/df-mc/dragonfly/server/mob/javamob"

// BodyGoal is a janimal goal on Bodies (BreedGoal[*Body], TemptGoal[*Body], ...).
type BodyGoal interface {
	Flags() GoalFlags
	CanUse(b *Body) bool
	CanContinueToUse(b *Body) bool
	Start(b *Body)
	Stop(b *Body)
	Tick(b *Body)
}

// Goal adapts a janimal goal to a javamob.Goal for m.Goals().Add. The flag bits are the same (MOVE, LOOK,
// JUMP, TARGET).
func Goal(g BodyGoal) javamob.Goal { return &adapter{g: g} }

type adapter struct{ g BodyGoal }

func (a *adapter) Flags() javamob.Flags { return javamob.Flags(a.g.Flags()) }

func (a *adapter) CanUse(m *javamob.Mob) bool {
	b, ok := Wrap(m)
	return ok && a.g.CanUse(b)
}

func (a *adapter) CanContinueToUse(m *javamob.Mob) bool {
	b, ok := Wrap(m)
	return ok && a.g.CanContinueToUse(b)
}

func (a *adapter) Start(m *javamob.Mob) {
	if b, ok := Wrap(m); ok {
		a.g.Start(b)
	}
}

func (a *adapter) Stop(m *javamob.Mob) {
	if b, ok := Wrap(m); ok {
		a.g.Stop(b)
	}
}

func (a *adapter) Tick(m *javamob.Mob) {
	if b, ok := Wrap(m); ok {
		a.g.Tick(b)
	}
}

// PanicRunning lets Body.Panicking find a running PanicGoal (PathfinderMob.isPanicking).
func (a *adapter) PanicRunning() bool {
	p, ok := a.g.(*PanicGoal[*Body])
	return ok && p.Running()
}

// Unwrap returns the janimal goal (to read EatBlockGoal.EatAnimationTick, TemptGoal.Running...).
func (a *adapter) Unwrap() BodyGoal { return a.g }
