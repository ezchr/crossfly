package jbrain

import (
	"github.com/df-mc/dragonfly/server/world"
)

// Random is the random source behaviours and sensors draw from. *rand.Rand
// (math/rand) satisfies it, as does the Java-compatible random of the mob
// base.
type Random interface {
	Intn(n int) int
	Float32() float32
	Float64() float64
}

// Ctx is what a brain passes to every sensor and behaviour of one tick: the
// ServerLevel, the game time (Level.getGameTime) and the level random of the
// Java signatures, plus the ticking brain's memories (body.getBrain()).
type Ctx struct {
	// Tx is the transaction the mob is ticked in. Tests may leave it nil.
	Tx *world.Tx
	// Time is the level's game time in ticks.
	Time int64
	// Rand is the level random (ServerLevel.getRandom).
	Rand Random
	// Mem is the memory store of the brain being ticked.
	Mem *Memories
}

// RunState is Java's Behavior.Status.
type RunState uint8

const (
	// Stopped behaviours are tried every tick while their activity is active.
	Stopped RunState = iota
	// Running behaviours are ticked every tick until they stop.
	Running
)

// Control is anything a brain can run (Java BehaviorControl). Implementations
// keep per-brain state: build a new value for every brain.
type Control[E any] interface {
	State() RunState
	// RequiredMemories lists the memories the behaviour reads or writes; the
	// brain registers them when the behaviour is added.
	RequiredMemories() []MemoryID
	TryStart(c *Ctx, e E) bool
	TickOrStop(c *Ctx, e E)
	DoStop(c *Ctx, e E)
	Name() string
}

// Task holds the overridable parts of a long running behaviour (the
// protected methods of Java's Behavior). Embed TaskBase to get Java's
// defaults and override what the behaviour needs.
type Task[E any] interface {
	// CanStart is checkExtraStartConditions, tried after the entry
	// conditions passed.
	CanStart(c *Ctx, e E) bool
	Start(c *Ctx, e E)
	// CanStillUse is checked every tick before Tick; false (the default)
	// stops the behaviour right away.
	CanStillUse(c *Ctx, e E) bool
	Tick(c *Ctx, e E)
	Stop(c *Ctx, e E)
}

// TimeoutOverride lets a Task replace Behavior.timedOut (timestamp >
// endTimestamp by default).
type TimeoutOverride interface {
	TimedOut(now, end int64) bool
}

// TaskBase provides Java's Behavior defaults: always may start, never
// continues, no-op start/tick/stop.
type TaskBase[E any] struct{}

func (TaskBase[E]) CanStart(*Ctx, E) bool    { return true }
func (TaskBase[E]) Start(*Ctx, E)            {}
func (TaskBase[E]) CanStillUse(*Ctx, E) bool { return false }
func (TaskBase[E]) Tick(*Ctx, E)             {}
func (TaskBase[E]) Stop(*Ctx, E)             {}

// DefaultDuration is Behavior.DEFAULT_DURATION.
const DefaultDuration = 60

// Behavior is Java's Behavior: a task with entry conditions that runs for a
// random duration in [min, max] ticks unless CanStillUse fails first.
type Behavior[E any] struct {
	name     string
	entry    []Req
	min, max int
	task     Task[E]
	timeout  TimeoutOverride
	state    RunState
	end      int64
	// paperRate is Paper's tick-rates.behavior override, -1 for none.
	paperRate int
}

// NewBehavior wraps task. name is the Java class name ("AcquirePoi"): its
// lower case form is Paper's tick-rate config key. entry are the memory conditions checked before
// CanStart; min/max the duration range (Java's timeOutDuration constructors:
// pass the same value twice; DefaultDuration when the Java class passes
// none).
func NewBehavior[E any](name string, entry []Req, minDuration, maxDuration int, task Task[E]) *Behavior[E] {
	b := &Behavior[E]{name: name, entry: entry, min: minDuration, max: maxDuration, task: task, paperRate: -1}
	if t, ok := task.(TimeoutOverride); ok {
		b.timeout = t
	}
	return b
}

// Task returns the wrapped task.
func (b *Behavior[E]) Task() Task[E] { return b.task }

func (b *Behavior[E]) State() RunState { return b.state }
func (b *Behavior[E]) Name() string    { return b.name }

func (b *Behavior[E]) RequiredMemories() []MemoryID {
	ids := make([]MemoryID, len(b.entry))
	for i, r := range b.entry {
		ids[i] = r.Mem
	}
	return ids
}

// TryStart is Behavior.tryStart: the duration is drawn from the level random
// only once the behaviour starts.
func (b *Behavior[E]) TryStart(c *Ctx, e E) bool {
	// Paper: with tick-rates.behavior.<entity>.<class> set, a behaviour
	// waits that many ticks after its last end time.
	if b.paperRate > -1 && c.Time < b.end+int64(b.paperRate) {
		return false
	}
	if !c.Mem.CheckAll(b.entry) || !b.task.CanStart(c, e) {
		return false
	}
	b.state = Running
	d := b.min
	if b.max > b.min {
		d += c.Rand.Intn(b.max + 1 - b.min)
	}
	b.end = c.Time + int64(d)
	b.task.Start(c, e)
	return true
}

func (b *Behavior[E]) timedOut(now int64) bool {
	if b.timeout != nil {
		return b.timeout.TimedOut(now, b.end)
	}
	return now > b.end
}

// TickOrStop is Behavior.tickOrStop.
func (b *Behavior[E]) TickOrStop(c *Ctx, e E) {
	if !b.timedOut(c.Time) && b.task.CanStillUse(c, e) {
		b.task.Tick(c, e)
	} else {
		b.DoStop(c, e)
	}
}

// DoStop is Behavior.doStop.
func (b *Behavior[E]) DoStop(c *Ctx, e E) {
	b.state = Stopped
	b.task.Stop(c, e)
}

// OneShot is Java's OneShot built by BehaviorBuilder.create: its memory
// conditions are checked in order and, if they all hold, the trigger runs;
// a successful trigger leaves the behaviour Running until the brain's
// running pass stops it in the same tick.
type OneShot[E any] struct {
	name    string
	reqs    []Req
	extra   []MemoryID
	trigger func(c *Ctx, e E) bool
	state   RunState
}

// Trigger builds a OneShot. reqs are the BehaviorBuilder memory conditions
// (Present/Absent/Reg); fn is the body of the apply lambda and returns
// whether it did something.
func Trigger[E any](name string, reqs []Req, fn func(c *Ctx, e E) bool) *OneShot[E] {
	return &OneShot[E]{name: name, reqs: reqs, trigger: fn}
}

// Fire runs the trigger without touching the run state (Trigger.trigger:
// used when one behaviour drives another, e.g. BehaviorBuilder.sequence).
func (o *OneShot[E]) Fire(c *Ctx, e E) bool {
	if !c.Mem.CheckAll(o.reqs) {
		return false
	}
	return o.trigger(c, e)
}

func (o *OneShot[E]) State() RunState { return o.state }
func (o *OneShot[E]) Name() string    { return o.name }

func (o *OneShot[E]) RequiredMemories() []MemoryID {
	ids := make([]MemoryID, 0, len(o.reqs)+len(o.extra))
	for _, r := range o.reqs {
		ids = append(ids, r.Mem)
	}
	return append(ids, o.extra...)
}

func (o *OneShot[E]) TryStart(c *Ctx, e E) bool {
	if o.Fire(c, e) {
		o.state = Running
		return true
	}
	return false
}

func (o *OneShot[E]) TickOrStop(c *Ctx, e E) { o.DoStop(c, e) }
func (o *OneShot[E]) DoStop(*Ctx, E)         { o.state = Stopped }

// Sequence is BehaviorBuilder.sequence: second is fired only when first
// fired.
func Sequence[E any](first, second *OneShot[E]) *OneShot[E] {
	o := &OneShot[E]{name: "OneShot[stuff]", reqs: first.reqs, trigger: func(c *Ctx, e E) bool {
		return first.trigger(c, e) && second.Fire(c, e)
	}}
	o.extra = second.RequiredMemories()
	return o
}

// TriggerIf is BehaviorBuilder.triggerIf(predicate, behaviour); a nil then
// gives the plain predicate trigger.
func TriggerIf[E any](pred func(c *Ctx, e E) bool, then *OneShot[E]) *OneShot[E] {
	p := Trigger[E]("TriggerIf", nil, pred)
	if then == nil {
		return p
	}
	return Sequence(p, then)
}

// Prioritized is a behaviour with its priority in an activity.
type Prioritized[E any] struct {
	Priority int
	Control  Control[E]
}

// Seq is ActivityData.createPriorityPairs: priorities first, first+1, ...
func Seq[E any](first int, cs ...Control[E]) []Prioritized[E] {
	out := make([]Prioritized[E], len(cs))
	for i, c := range cs {
		out[i] = Prioritized[E]{Priority: first + i, Control: c}
	}
	return out
}
