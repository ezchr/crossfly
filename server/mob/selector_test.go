//go:build crossfly_full

package mob

import (
	"slices"
	"strings"
	"testing"
)

// fakeGoal records its calls. use/cont/intr are read live, so a test can
// flip them between ticks.
type fakeGoal struct {
	name   string
	log    *[]string
	use    bool
	cont   *bool // nil: BDS default (CanUse)
	intr   bool
	flags  ControlFlags
	target bool
	onTick func()
}

func (g *fakeGoal) CanUse(*Mob) bool    { return g.use }
func (g *fakeGoal) Flags() ControlFlags { return g.flags }
func (g *fakeGoal) IsTargetGoal() bool  { return g.target }
func (g *fakeGoal) CanBeInterrupted(*Mob) bool {
	return g.intr
}
func (g *fakeGoal) Start(*Mob) { *g.log = append(*g.log, "start "+g.name) }
func (g *fakeGoal) Stop(*Mob)  { *g.log = append(*g.log, "stop "+g.name) }
func (g *fakeGoal) Tick(*Mob) {
	*g.log = append(*g.log, "tick "+g.name)
	if g.onTick != nil {
		g.onTick()
	}
}

// continuerGoal adds CanContinueToUse to fakeGoal.
type continuerGoal struct{ *fakeGoal }

func (g continuerGoal) CanContinueToUse(*Mob) bool { return *g.cont }

// plainGoal implements only the required methods, for the defaults.
type plainGoal struct {
	use bool
	GoalBase
}

func (g *plainGoal) CanUse(*Mob) bool { return g.use }

type goalDef struct {
	name   string
	prio   int
	use    bool
	cont   *bool
	intr   bool
	flags  ControlFlags
	target bool
}

func build(log *[]string, defs ...goalDef) (*GoalSelector, map[string]*fakeGoal) {
	s := NewGoalSelector()
	gs := map[string]*fakeGoal{}
	for _, d := range defs {
		g := &fakeGoal{name: d.name, log: log, use: d.use, cont: d.cont, intr: d.intr, flags: d.flags, target: d.target}
		gs[d.name] = g
		if d.cont != nil {
			s.Add(d.name, d.prio, continuerGoal{g})
		} else {
			s.Add(d.name, d.prio, g)
		}
	}
	return s, gs
}

func ptr(b bool) *bool { return &b }

func TestSelectorTable(t *testing.T) {
	const mv, lk, jp = FlagMove, FlagLook, FlagJump
	cases := []struct {
		name  string
		goals []goalDef
		// between runs before every tick after the first.
		between func(gs map[string]*fakeGoal, s *GoalSelector)
		ticks   int
		want    []string
		running []string
	}{
		{
			name:    "a usable goal starts then ticks",
			goals:   []goalDef{{name: "a", use: true, intr: true, flags: mv}},
			ticks:   2,
			want:    []string{"start a", "tick a", "tick a"},
			running: []string{"a"},
		},
		{
			name:  "canContinueToUse defaults to canUse",
			goals: []goalDef{{name: "a", use: true, intr: true, flags: mv}},
			between: func(gs map[string]*fakeGoal, _ *GoalSelector) {
				gs["a"].use = false
			},
			ticks: 2,
			want:  []string{"start a", "tick a", "stop a"},
		},
		{
			name:  "a stopped goal restarts in the same pass when canUse holds",
			goals: []goalDef{{name: "a", use: true, cont: ptr(true), intr: true, flags: mv}},
			between: func(gs map[string]*fakeGoal, _ *GoalSelector) {
				*gs["a"].cont = false
			},
			ticks:   2,
			want:    []string{"start a", "tick a", "stop a", "start a", "tick a"},
			running: []string{"a"},
		},
		{
			name: "first in list wins a tie on shared flags",
			goals: []goalDef{
				{name: "a", prio: 2, use: true, intr: true, flags: mv},
				{name: "b", prio: 2, use: true, intr: true, flags: mv | lk},
			},
			ticks:   1,
			want:    []string{"start a", "tick a"},
			running: []string{"a"},
		},
		{
			name: "same priority with disjoint flags run together",
			goals: []goalDef{
				{name: "a", prio: 2, use: true, intr: true, flags: mv},
				{name: "b", prio: 2, use: true, intr: true, flags: lk | jp},
			},
			ticks:   1,
			want:    []string{"start a", "tick a", "start b", "tick b"},
			running: []string{"a", "b"},
		},
		{
			name: "less important goal cannot start over a running one",
			goals: []goalDef{
				{name: "hi", prio: 1, use: true, intr: true, flags: mv},
				{name: "lo", prio: 5, use: true, intr: true, flags: mv},
			},
			ticks:   2,
			want:    []string{"start hi", "tick hi", "tick hi"},
			running: []string{"hi"},
		},
		{
			name: "more important goal interrupts: pass 3 stops the loser before start",
			goals: []goalDef{
				{name: "lo", prio: 5, use: true, intr: true, flags: mv},
				{name: "hi", prio: 1, use: false, intr: true, flags: mv},
			},
			between: func(gs map[string]*fakeGoal, _ *GoalSelector) { gs["hi"].use = true },
			ticks:   2,
			want:    []string{"start lo", "tick lo", "stop lo", "start hi", "tick hi"},
			running: []string{"hi"},
		},
		{
			name: "uninterruptible running goal blocks a more important one",
			goals: []goalDef{
				{name: "lo", prio: 5, use: true, intr: false, flags: mv},
				{name: "hi", prio: 1, use: false, intr: true, flags: lk},
			},
			between: func(gs map[string]*fakeGoal, _ *GoalSelector) { gs["hi"].use = true },
			ticks:   2,
			want:    []string{"start lo", "tick lo", "tick lo"},
			running: []string{"lo"},
		},
		{
			name: "interruption by priority does not need shared flags",
			goals: []goalDef{
				{name: "lo", prio: 5, use: true, intr: true, flags: mv},
				{name: "hi", prio: 1, use: false, intr: true, flags: lk},
			},
			between: func(gs map[string]*fakeGoal, _ *GoalSelector) { gs["hi"].use = true },
			ticks:   2,
			want:    []string{"start lo", "tick lo", "tick lo", "start hi", "tick hi"},
			running: []string{"lo", "hi"},
		},
		{
			name: "target goals run first and don't share flags with main goals",
			goals: []goalDef{
				{name: "main", prio: 1, use: true, intr: true, flags: mv},
				{name: "tgt", prio: 1, use: true, intr: true, flags: mv, target: true},
			},
			ticks:   1,
			want:    []string{"start tgt", "tick tgt", "start main", "tick main"},
			running: []string{"main", "tgt"},
		},
		{
			name: "an uninterruptible less important target goal blocks a main goal (BDS checks priority across kinds)",
			goals: []goalDef{
				{name: "tgt", prio: 3, use: true, intr: false, flags: 0, target: true},
				{name: "main", prio: 1, use: true, intr: true, flags: mv},
			},
			ticks:   1,
			want:    []string{"start tgt", "tick tgt"},
			running: []string{"tgt"},
		},
		{
			name: "flags changing at run time: the earlier goal in the list yields",
			goals: []goalDef{
				{name: "a", prio: 2, use: true, intr: true, flags: mv},
				{name: "b", prio: 2, use: true, intr: true, flags: lk},
			},
			between: func(gs map[string]*fakeGoal, _ *GoalSelector) { gs["a"].flags = lk },
			ticks:   2,
			want:    []string{"start a", "tick a", "start b", "tick b", "stop a", "tick b"},
			running: []string{"b"},
		},
		{
			name: "replacing a goal moves it to the end of the list without stopping it",
			goals: []goalDef{
				{name: "a", prio: 2, use: true, intr: true, flags: mv},
				{name: "b", prio: 2, use: true, intr: true, flags: mv},
			},
			between: func(gs map[string]*fakeGoal, s *GoalSelector) {
				s.Add("a", 2, gs["a"]) // same goal value, fresh entry (not used)
			},
			ticks: 2,
			// Tick 1: a wins the tie. Then a is re-added at the end: b is now
			// first and not blocked (the new a entry is not used).
			want:    []string{"start a", "tick a", "start b", "tick b"},
			running: []string{"b"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var log []string
			s, gs := build(&log, c.goals...)
			for i := 0; i < c.ticks; i++ {
				if i > 0 && c.between != nil {
					c.between(gs, s)
				}
				s.Tick(nil)
			}
			if !slices.Equal(log, c.want) {
				t.Errorf("calls:\n got %q\nwant %q", log, c.want)
			}
			got := s.RunningGoals()
			slices.Sort(got)
			want := slices.Clone(c.running)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("running: got %q, want %q", got, want)
			}
		})
	}
}

// TestSelectorDefaults checks the BDS Goal defaults for a goal implementing
// only CanUse and Flags: interruptible, not a target goal, continue = canUse.
func TestSelectorDefaults(t *testing.T) {
	s := NewGoalSelector()
	g := &plainGoal{use: true, GoalBase: GoalBase{Control: FlagMove}}
	s.Add("lo", 5, g)
	s.Tick(nil)
	if !s.Running("lo") {
		t.Fatal("plain goal did not start")
	}
	var log []string
	hi := &fakeGoal{name: "hi", log: &log, use: true, intr: true, flags: FlagMove}
	s.Add("hi", 1, hi)
	s.Tick(nil)
	if s.Running("lo") || !s.Running("hi") {
		t.Fatalf("default goal was not interruptible: running %q", s.RunningGoals())
	}
	g.use = false
	s.Remove("hi")
	s.Tick(nil)
	if s.Running("lo") {
		t.Fatal("default canContinueToUse did not follow canUse")
	}
}

// TestSelectorDeferredChanges checks that goals added or removed while a pass
// runs (an event fired from a goal) only change the list after the pass.
func TestSelectorDeferredChanges(t *testing.T) {
	var log []string
	s, gs := build(&log, goalDef{name: "a", use: true, intr: true, flags: FlagMove})
	added := &fakeGoal{name: "n", log: &log, use: true, intr: true, flags: FlagLook}
	gs["a"].onTick = func() {
		gs["a"].onTick = nil
		s.Add("n", 0, added)
		if slices.Contains(s.Keys(), "n") {
			t.Error("goal added during a pass is visible during the pass")
		}
	}
	s.Tick(nil)
	if !slices.Equal(s.Keys(), []string{"a", "n"}) {
		t.Fatalf("keys after pass: %q", s.Keys())
	}
	s.Tick(nil)
	want := []string{"tick a", "tick a", "start n", "tick n"}
	if got := log[1:]; !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	gs["a"].onTick = func() { s.Remove("a") }
	s.Tick(nil)
	if !slices.Equal(s.Keys(), []string{"n"}) {
		t.Fatalf("keys after remove: %q", s.Keys())
	}
}

func TestSelectorClearAndStop(t *testing.T) {
	var log []string
	s, _ := build(&log,
		goalDef{name: "t", prio: 1, use: true, intr: true, flags: FlagMove, target: true},
		goalDef{name: "m", prio: 1, use: true, intr: true, flags: FlagMove},
	)
	s.Tick(nil)
	log = nil
	s.StopNonTargeted(nil)
	if !slices.Equal(log, []string{"stop m"}) || !s.Running("t") || s.Running("m") {
		t.Fatalf("StopNonTargeted: log %q running %q", log, s.RunningGoals())
	}
	s.ClearTargetGoals()
	if !slices.Equal(s.Keys(), []string{"m"}) {
		t.Fatalf("ClearTargetGoals left %q", s.Keys())
	}
	s.ClearNonTargetedGoals()
	if s.Len() != 0 {
		t.Fatalf("ClearNonTargetedGoals left %q", s.Keys())
	}
}

func TestSelectorDebugInfo(t *testing.T) {
	s := NewGoalSelector()
	if got := s.DebugInfo(); got != "(no active goals)\n" {
		t.Fatalf("empty: %q", got)
	}
	var log []string
	s, _ = build(&log,
		goalDef{name: "a", use: true, intr: true, flags: FlagMove},
		goalDef{name: "b", use: true, intr: true, flags: FlagLook},
	)
	s.Tick(nil)
	if got := s.DebugInfo(); got != "a\n, b\n" || !strings.HasSuffix(got, "\n") {
		t.Fatalf("got %q", got)
	}
}

func TestParseGoalSpec(t *testing.T) {
	spec, err := ParseGoalSpec("minecraft:behavior.x", []byte(`{"priority": 4, "control_flags": ["move", "jump"], "speed_multiplier": 1.2}`))
	if err != nil {
		t.Fatal(err)
	}
	if spec.Priority != 4 || !spec.HasControlFlags || spec.ControlFlags != FlagMove|FlagJump {
		t.Fatalf("%+v", spec)
	}
	spec, _ = ParseGoalSpec("minecraft:behavior.x", []byte(`{}`))
	if spec.Priority != 0 || spec.HasControlFlags || spec.FlagsOr(FlagLook) != FlagLook {
		t.Fatalf("%+v", spec)
	}
}
