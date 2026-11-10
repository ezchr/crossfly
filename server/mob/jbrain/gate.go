package jbrain

import (
	"math"
	"sort"
)

// OrderPolicy is GateBehavior.OrderPolicy.
type OrderPolicy uint8

const (
	// Ordered keeps the order the behaviours were added in.
	Ordered OrderPolicy = iota
	// Shuffled reorders by weight every time the gate starts
	// (ShufflingList.shuffle).
	Shuffled
)

// RunningPolicy is GateBehavior.RunningPolicy.
type RunningPolicy uint8

const (
	// RunOnePolicy starts the first stopped behaviour that starts.
	RunOnePolicy RunningPolicy = iota
	// TryAll tries to start every stopped behaviour.
	TryAll
)

// Weighted is a behaviour with its weight in a gate (Pair<BehaviorControl, Integer>).
type Weighted[E any] struct {
	Control Control[E]
	Weight  int
}

// W is shorthand for a Weighted entry.
func W[E any](c Control[E], weight int) Weighted[E] { return Weighted[E]{Control: c, Weight: weight} }

type shuffleEntry[E any] struct {
	c      Control[E]
	weight int
	rand   float64
}

// Gate is Java's GateBehavior (and RunOne): it starts its children by a
// policy and runs while any of them runs.
type Gate[E any] struct {
	name    string
	entry   []Req
	erase   []MemoryID
	order   OrderPolicy
	running RunningPolicy
	list    []shuffleEntry[E]
	state   RunState
}

// NewGate builds a GateBehavior. exitErase lists the memories erased when it
// stops.
func NewGate[E any](entry []Req, exitErase []MemoryID, order OrderPolicy, running RunningPolicy, children []Weighted[E]) *Gate[E] {
	g := &Gate[E]{name: "GateBehavior", entry: entry, erase: exitErase, order: order, running: running}
	for _, w := range children {
		g.list = append(g.list, shuffleEntry[E]{c: w.Control, weight: w.Weight})
	}
	return g
}

// RunOne is Java's RunOne: shuffled by weight, the first child that starts
// runs.
func RunOne[E any](entry []Req, children ...Weighted[E]) *Gate[E] {
	g := NewGate(entry, nil, Shuffled, RunOnePolicy, children)
	g.name = "RunOne"
	return g
}

func (g *Gate[E]) State() RunState { return g.state }

func (g *Gate[E]) Name() string {
	s := g.name + ": ["
	first := true
	for _, e := range g.list {
		if e.c.State() == Running {
			if !first {
				s += ", "
			}
			first = false
			s += e.c.Name()
		}
	}
	return s + "]"
}

// Children returns the gate's behaviours in their current order.
func (g *Gate[E]) Children() []Control[E] {
	out := make([]Control[E], len(g.list))
	for i, e := range g.list {
		out[i] = e.c
	}
	return out
}

func (g *Gate[E]) RequiredMemories() []MemoryID {
	var ids []MemoryID
	for _, r := range g.entry {
		ids = append(ids, r.Mem)
	}
	for _, e := range g.list {
		ids = append(ids, e.c.RequiredMemories()...)
	}
	return ids
}

// shuffle is ShufflingList.shuffle: every entry gets -(r^(1/weight)) and the
// list is sorted ascending (stable, as List.sort is).
func (g *Gate[E]) shuffle(r Random) {
	for i := range g.list {
		g.list[i].rand = -math.Pow(float64(r.Float32()), 1/float64(g.list[i].weight))
	}
	sort.SliceStable(g.list, func(i, j int) bool { return g.list[i].rand < g.list[j].rand })
}

func (g *Gate[E]) TryStart(c *Ctx, e E) bool {
	if !c.Mem.CheckAll(g.entry) {
		return false
	}
	g.state = Running
	if g.order == Shuffled {
		g.shuffle(c.Rand)
	}
	for _, s := range g.list {
		if s.c.State() != Stopped {
			continue
		}
		if s.c.TryStart(c, e) && g.running == RunOnePolicy {
			break
		}
	}
	return true
}

func (g *Gate[E]) TickOrStop(c *Ctx, e E) {
	for _, s := range g.list {
		if s.c.State() == Running {
			s.c.TickOrStop(c, e)
		}
	}
	for _, s := range g.list {
		if s.c.State() == Running {
			return
		}
	}
	g.DoStop(c, e)
}

func (g *Gate[E]) DoStop(c *Ctx, e E) {
	g.state = Stopped
	for _, s := range g.list {
		if s.c.State() == Running {
			s.c.DoStop(c, e)
		}
	}
	for _, id := range g.erase {
		c.Mem.Erase(id)
	}
}
