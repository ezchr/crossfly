//go:build crossfly_full

package javamob

import "fmt"

// Flags are Goal.Flag values: the controls a goal takes. Two running goals never share a flag.
type Flags uint8

const (
	FlagMove Flags = 1 << iota
	FlagLook
	FlagJump
	FlagTarget
)

// Goal is a Java Goal. CanUse and Flags are required; the optional interfaces below default as in Goal.java
// (CanContinueToUse = CanUse, interruptible, no-op start/stop/tick, ticked every other tick).
type Goal interface {
	CanUse(m *Mob) bool
	Flags() Flags
}

// The optional Goal methods.
type (
	GoalContinuer     interface{ CanContinueToUse(m *Mob) bool }
	GoalInterruptible interface{ IsInterruptable() bool }
	GoalStarter       interface{ Start(m *Mob) }
	GoalStopper       interface{ Stop(m *Mob) }
	GoalTicker        interface{ Tick(m *Mob) }
	// GoalEveryTick goals are ticked every tick, not only on the ticks the selector evaluates
	// (requiresUpdateEveryTick).
	GoalEveryTick interface{ RequiresUpdateEveryTick() bool }
)

// GoalBase gives a goal its flags. Embed it and set Control.
type GoalBase struct{ Control Flags }

// Flags ...
func (g GoalBase) Flags() Flags { return g.Control }

// AdjustedTickDelay is Goal.adjustedTickDelay: ticks halved (rounded up) for a goal ticked every other tick.
func AdjustedTickDelay(g Goal, ticks int) int {
	if e, ok := g.(GoalEveryTick); ok && e.RequiresUpdateEveryTick() {
		return ticks
	}
	return ReducedTickDelay(ticks)
}

// ReducedTickDelay is Goal.reducedTickDelay: ceil(ticks / 2).
func ReducedTickDelay(ticks int) int { return (ticks + 1) / 2 }

// wrappedGoal is a WrappedGoal: a goal with its priority and whether it runs.
type wrappedGoal struct {
	g         Goal
	prio      int
	running   bool
	everyTick bool
	flags     Flags
}

func (w *wrappedGoal) interruptible() bool {
	if i, ok := w.g.(GoalInterruptible); ok {
		return i.IsInterruptable()
	}
	return true
}

func (w *wrappedGoal) canContinue(m *Mob) bool {
	if c, ok := w.g.(GoalContinuer); ok {
		return c.CanContinueToUse(m)
	}
	return w.g.CanUse(m)
}

func (w *wrappedGoal) start(m *Mob) {
	if w.running {
		return
	}
	w.running = true
	if s, ok := w.g.(GoalStarter); ok {
		s.Start(m)
	}
}

func (w *wrappedGoal) stop(m *Mob) {
	if !w.running {
		return
	}
	w.running = false
	if s, ok := w.g.(GoalStopper); ok {
		s.Stop(m)
	}
}

func (w *wrappedGoal) tickGoal(m *Mob) {
	if t, ok := w.g.(GoalTicker); ok {
		t.Tick(m)
	}
}

// GoalSelector is Java's GoalSelector (with Paper's flag sets, which behave the same): goals in insertion order,
// each flag locked by the goal using it, a goal of lower priority number replacing an interruptible one.
type GoalSelector struct {
	goals    []*wrappedGoal
	locked   [4]*wrappedGoal
	disabled Flags
	// pending holds changes made while the selector iterates, applied after.
	iterating bool
	pending   []func()
}

// Add adds a goal at the priority (lower runs first). Adding a goal already in the selector does nothing.
func (gs *GoalSelector) Add(prio int, g Goal) {
	if gs.iterating {
		gs.pending = append(gs.pending, func() { gs.Add(prio, g) })
		return
	}
	for _, w := range gs.goals {
		if w.g == g {
			return
		}
	}
	every := false
	if e, ok := g.(GoalEveryTick); ok {
		every = e.RequiresUpdateEveryTick()
	}
	gs.goals = append(gs.goals, &wrappedGoal{g: g, prio: prio, everyTick: every, flags: g.Flags()})
}

// RemoveIf removes every goal f reports true for, stopping the running ones (removeAllGoals).
func (gs *GoalSelector) RemoveIf(m *Mob, f func(Goal) bool) {
	if gs.iterating {
		gs.pending = append(gs.pending, func() { gs.RemoveIf(m, f) })
		return
	}
	kept := gs.goals[:0]
	for _, w := range gs.goals {
		if f(w.g) {
			if w.running {
				w.stop(m)
			}
			continue
		}
		kept = append(kept, w)
	}
	for i := len(kept); i < len(gs.goals); i++ {
		gs.goals[i] = nil
	}
	gs.goals = kept
}

// Remove removes the goal (removeGoal).
func (gs *GoalSelector) Remove(m *Mob, g Goal) { gs.RemoveIf(m, func(o Goal) bool { return o == g }) }

// SetControlFlag enables or disables a flag (setControlFlag): goals with a disabled flag stop and do not start.
func (gs *GoalSelector) SetControlFlag(f Flags, enabled bool) {
	if enabled {
		gs.disabled &^= f
	} else {
		gs.disabled |= f
	}
}

// Running returns the running goals in insertion order.
func (gs *GoalSelector) Running() []Goal {
	var out []Goal
	for _, w := range gs.goals {
		if w.running {
			out = append(out, w.g)
		}
	}
	return out
}

// IsRunning reports whether g runs.
func (gs *GoalSelector) IsRunning(g Goal) bool {
	for _, w := range gs.goals {
		if w.g == g {
			return w.running
		}
	}
	return false
}

// Goals returns every goal in insertion order.
func (gs *GoalSelector) Goals() []Goal {
	out := make([]Goal, len(gs.goals))
	for i, w := range gs.goals {
		out[i] = w.g
	}
	return out
}

// StopAll stops every running goal.
func (gs *GoalSelector) StopAll(m *Mob) {
	for _, w := range gs.goals {
		w.stop(m)
	}
}

func flagIndexes(f Flags) []int {
	var out []int
	for i := 0; i < 4; i++ {
		if f&(1<<i) != 0 {
			out = append(out, i)
		}
	}
	return out
}

// tick is GoalSelector.tick: stop goals that may not go on, start the goals that may, tick the running ones.
func (gs *GoalSelector) tick(m *Mob) {
	gs.iterating = true
	for _, w := range gs.goals {
		if w.running && (w.flags&gs.disabled != 0 || !w.canContinue(m)) {
			w.stop(m)
		}
	}
	for i, l := range gs.locked {
		if l != nil && !l.running {
			gs.locked[i] = nil
		}
	}
	for _, w := range gs.goals {
		if w.running || w.flags&gs.disabled != 0 || !gs.replaceable(w) || !w.g.CanUse(m) {
			continue
		}
		for _, i := range flagIndexes(w.flags) {
			if l := gs.locked[i]; l != nil {
				l.stop(m)
			}
			gs.locked[i] = w
		}
		w.start(m)
	}
	gs.iterating = false
	gs.flush()
	gs.tickRunning(m, true)
}

// replaceable is goalCanBeReplacedForAllFlags.
func (gs *GoalSelector) replaceable(w *wrappedGoal) bool {
	for _, i := range flagIndexes(w.flags) {
		l := gs.locked[i]
		if l == nil {
			continue
		}
		if !(l.interruptible() && w.prio < l.prio) {
			return false
		}
	}
	return true
}

// tickRunning is GoalSelector.tickRunningGoals.
func (gs *GoalSelector) tickRunning(m *Mob, all bool) {
	gs.iterating = true
	for _, w := range gs.goals {
		if w.running && (all || w.everyTick) {
			w.tickGoal(m)
			if m.gone() {
				break
			}
		}
	}
	gs.iterating = false
	gs.flush()
}

func (gs *GoalSelector) flush() {
	for len(gs.pending) > 0 {
		p := gs.pending
		gs.pending = nil
		for _, f := range p {
			f()
		}
	}
}

// String lists the goals for debugging.
func (gs *GoalSelector) String() string {
	s := ""
	for _, w := range gs.goals {
		s += fmt.Sprintf("%d %T running=%v\n", w.prio, w.g, w.running)
	}
	return s
}
