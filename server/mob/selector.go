//go:build crossfly_full

package mob

import (
	"strings"
)

// GoalSelector is BDS's GoalSelectorComponent: one list holding the goals of
// both kinds (target goals and the others), each with a priority and the
// used/toStart bits of PrioritizedGoal. The list is in insertion order, not
// sorted: Add appends, and replacing a goal of the same key moves it to the
// end (GoalSelectorComponent::addGoal erases, then emplaces at the back).
//
// Tick runs the four passes of GoalSelectorSystem::_tickGoalSelectorComponent:
// tryStartGoals(target), tickGoals(target), tryStartGoals(main),
// tickGoals(main). Lower priority numbers are more important.
type GoalSelector struct {
	goals []*goalEntry

	// iterating is set while a pass walks the list. BDS moves the vector out
	// of the component during a pass; goals added or removed meanwhile (an
	// event fired by a goal changed the definition) are queued here and
	// applied when the pass ends.
	iterating bool
	pending   []pendingOp
}

type goalEntry struct {
	key      string
	priority int
	g        Goal
	target   bool
	used     bool
	toStart  bool

	cont  GoalContinuer
	intr  GoalInterruptible
	start GoalStarter
	stop  GoalStopper
	tick  GoalTicker
}

type pendingOp struct {
	add *goalEntry
	del string
}

func newEntry(key string, priority int, g Goal) *goalEntry {
	e := &goalEntry{key: key, priority: priority, g: g}
	e.cont, _ = g.(GoalContinuer)
	e.intr, _ = g.(GoalInterruptible)
	e.start, _ = g.(GoalStarter)
	e.stop, _ = g.(GoalStopper)
	e.tick, _ = g.(GoalTicker)
	if t, ok := g.(TargetGoal); ok {
		e.target = t.IsTargetGoal()
	}
	return e
}

func (e *goalEntry) canContinueToUse(m *Mob) bool {
	if e.cont != nil {
		return e.cont.CanContinueToUse(m)
	}
	return e.g.CanUse(m)
}

func (e *goalEntry) canBeInterrupted(m *Mob) bool {
	if e.intr != nil {
		return e.intr.CanBeInterrupted(m)
	}
	return true
}

func (e *goalEntry) doStart(m *Mob) {
	if e.start != nil {
		e.start.Start(m)
	}
}

func (e *goalEntry) doStop(m *Mob) {
	if e.stop != nil {
		e.stop.Stop(m)
	}
}

func (e *goalEntry) doTick(m *Mob) {
	if e.tick != nil {
		e.tick.Tick(m)
	}
}

// NewGoalSelector returns an empty selector. Mobs own one each (Mob.Goals);
// this is exported for tests.
func NewGoalSelector() *GoalSelector { return &GoalSelector{} }

// Add adds goal g under key with a priority, replacing (without stopping, as
// BDS) a goal already under key. The goal goes to the end of the list.
func (s *GoalSelector) Add(key string, priority int, g Goal) {
	e := newEntry(key, priority, g)
	if s.iterating {
		s.pending = append(s.pending, pendingOp{add: e})
		return
	}
	s.remove(key)
	s.goals = append(s.goals, e)
}

// Remove removes the goal under key, without stopping it (BDS removeGoal).
// It reports whether there was one.
func (s *GoalSelector) Remove(key string) bool {
	if s.iterating {
		s.pending = append(s.pending, pendingOp{del: key})
		return s.index(key) >= 0
	}
	return s.remove(key)
}

func (s *GoalSelector) remove(key string) bool {
	if i := s.index(key); i >= 0 {
		s.goals = append(s.goals[:i], s.goals[i+1:]...)
		return true
	}
	return false
}

func (s *GoalSelector) index(key string) int {
	for i, e := range s.goals {
		if e.key == key {
			return i
		}
	}
	return -1
}

// Goal returns the goal under key.
func (s *GoalSelector) Goal(key string) (Goal, bool) {
	if i := s.index(key); i >= 0 {
		return s.goals[i].g, true
	}
	return nil, false
}

// Priority returns the priority of the goal under key (lower runs first).
func (s *GoalSelector) Priority(key string) (int, bool) {
	if i := s.index(key); i >= 0 {
		return s.goals[i].priority, true
	}
	return 0, false
}

// Running reports whether the goal under key is in use (started or about to
// start).
func (s *GoalSelector) Running(key string) bool {
	i := s.index(key)
	return i >= 0 && s.goals[i].used
}

// RunningGoals returns the keys of the goals in use, in list order.
func (s *GoalSelector) RunningGoals() []string {
	var out []string
	for _, e := range s.goals {
		if e.used {
			out = append(out, e.key)
		}
	}
	return out
}

// Keys returns every goal key in list order.
func (s *GoalSelector) Keys() []string {
	out := make([]string, len(s.goals))
	for i, e := range s.goals {
		out[i] = e.key
	}
	return out
}

// Len returns the number of goals.
func (s *GoalSelector) Len() int { return len(s.goals) }

// Tick runs the selector's four passes, as GoalSelectorSystem does for every
// alive, non-removed mob each tick.
func (s *GoalSelector) Tick(m *Mob) {
	s.TryStartGoals(m, true)
	s.TickGoals(m, true)
	s.TryStartGoals(m, false)
	s.TickGoals(m, false)
}

// canUseInSystem is GoalSelectorUtility::canUseInSystem: e may run unless a
// running goal o (o != e) blocks it. If e is more important (lower number),
// o must be interruptible, whatever its kind; otherwise, when both are of the
// same kind, they must not share a control flag.
func (s *GoalSelector) canUseInSystem(m *Mob, e *goalEntry) bool {
	for _, o := range s.goals {
		if o == e || !o.used {
			continue
		}
		if e.priority < o.priority {
			if !o.canBeInterrupted(m) {
				return false
			}
		} else if e.target == o.target && e.g.Flags()&o.g.Flags() != 0 {
			return false
		}
	}
	return true
}

// TryStartGoals is GoalSelectorUtility::tryStartGoals for one kind:
//  1. a running goal keeps running while canUseInSystem and canContinueToUse
//     hold; otherwise it is stopped (used cleared, toStart kept);
//  2. a goal not running (or just stopped) is marked used and toStart when
//     canUseInSystem and canUse hold, so a stopped goal may restart at once;
//  3. if any goal was marked, every running goal of either kind that
//     canUseInSystem now rejects is stopped (used and toStart cleared).
func (s *GoalSelector) TryStartGoals(m *Mob, target bool) {
	s.iterating = true
	changed := false
	for _, e := range s.goals {
		if e.g == nil || e.target != target {
			continue
		}
		if e.used {
			if s.canUseInSystem(m, e) && e.canContinueToUse(m) {
				continue
			}
			e.doStop(m)
			e.used = false
		}
		if s.canUseInSystem(m, e) && e.g.CanUse(m) {
			e.used, e.toStart = true, true
			changed = true
		}
	}
	if changed {
		for _, e := range s.goals {
			if e.used && !s.canUseInSystem(m, e) {
				e.doStop(m)
				e.used, e.toStart = false, false
			}
		}
	}
	s.endPass()
}

// TickGoals is GoalSelectorUtility::tickGoals for one kind: goals marked
// toStart are started, then every used goal is ticked, in list order.
func (s *GoalSelector) TickGoals(m *Mob, target bool) {
	s.iterating = true
	for _, e := range s.goals {
		if e.g == nil || e.target != target {
			continue
		}
		if e.toStart {
			e.doStart(m)
			e.toStart = false
		}
		if e.used {
			e.doTick(m)
		}
	}
	s.endPass()
}

func (s *GoalSelector) endPass() {
	s.iterating = false
	if len(s.pending) == 0 {
		return
	}
	ops := s.pending
	s.pending = nil
	for _, op := range ops {
		if op.add != nil {
			s.remove(op.add.key)
			s.goals = append(s.goals, op.add)
		} else {
			s.remove(op.del)
		}
	}
}

// StopAll stops every running goal (GoalSelectorComponent::stopAllGoals:
// used cleared, toStart kept).
func (s *GoalSelector) StopAll(m *Mob) {
	for _, e := range s.goals {
		if e.used {
			e.doStop(m)
			e.used = false
		}
	}
}

// StopNonTargeted stops the running goals that are not target goals.
func (s *GoalSelector) StopNonTargeted(m *Mob) {
	for _, e := range s.goals {
		if !e.target && e.used {
			e.doStop(m)
			e.used = false
		}
	}
}

// ClearAll removes every goal without stopping them (clearAllGoals).
func (s *GoalSelector) ClearAll() { s.goals = nil }

// ClearTargetGoals removes the target goals without stopping them.
func (s *GoalSelector) ClearTargetGoals() { s.erase(func(e *goalEntry) bool { return e.target }) }

// ClearNonTargetedGoals removes the other goals without stopping them.
func (s *GoalSelector) ClearNonTargetedGoals() {
	s.erase(func(e *goalEntry) bool { return !e.target })
}

func (s *GoalSelector) erase(f func(e *goalEntry) bool) {
	out := s.goals[:0]
	for _, e := range s.goals {
		if !f(e) {
			out = append(out, e)
		}
	}
	clear(s.goals[len(out):])
	s.goals = out
}

// DebugInfo is GoalSelectorComponent::buildDebugInfo: each used goal's debug
// text on its own line, separated by ", ", or "(no active goals)\n".
func (s *GoalSelector) DebugInfo() string {
	var b strings.Builder
	n := 0
	for _, e := range s.goals {
		if !e.used {
			continue
		}
		if n > 0 {
			b.WriteString(", ")
		}
		if d, ok := e.g.(GoalDebugger); ok {
			d.AppendDebugInfo(&b)
		} else {
			b.WriteString(e.key)
		}
		b.WriteByte('\n')
		n++
	}
	if n == 0 {
		return "(no active goals)\n"
	}
	return b.String()
}
