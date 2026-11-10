package jbrain

import (
	"sort"
	"strings"
)

// ActivityData is one activity of a brain (Java ActivityData): its
// behaviours with priorities, the memory conditions that must hold to switch
// to it and the memories erased when another activity replaces it.
type ActivityData[E any] struct {
	Activity    Activity
	Behaviours  []Prioritized[E]
	Conditions  []Req
	EraseOnStop []MemoryID
}

// ActivityErasing is ActivityData.create(activity, prio, list, memory): the
// activity needs memory to have a value and erases it when it stops.
func ActivityErasing[E any](a Activity, first int, mem MemoryID, cs ...Control[E]) ActivityData[E] {
	return ActivityData[E]{Activity: a, Behaviours: Seq(first, cs...), Conditions: []Req{{Mem: mem, Status: ValuePresent}}, EraseOnStop: []MemoryID{mem}}
}

// Provider is Java's Brain.Provider: the memories, sensors and activities a
// mob type's brains are made of. Build one per mob type (package level) and
// call MakeBrain per mob.
type Provider[E any] struct {
	// EntityType is the Java entity type path ("villager"), used for Paper's
	// per-entity tick-rate overrides.
	EntityType string
	Memories   []MemoryID
	Sensors    []*SensorType[E]
	// Activities builds the activities of a new brain (Brain.ActivitySupplier).
	// Behaviours carry state, so it must return new values every call. Nil
	// means none (the mob sets them up itself, e.g. Villager.refreshBrain).
	Activities func(e E) []ActivityData[E]
}

// MakeBrain is Provider.makeBrain: a new brain for body e. saved, when not
// nil, are memories restored from NBT (see Unpack). r seeds the sensors'
// random start delays (the entity random).
func (p *Provider[E]) MakeBrain(e E, r Random, saved *Packed) *Brain[E] {
	b := &Brain[E]{entityType: p.EntityType, def: ActIdle, lastScheduleUpdate: -9999}
	for _, m := range p.Memories {
		b.mem.Register(m)
	}
	for _, st := range p.Sensors {
		s := st.New()
		rate := st.ScanRate
		if rate <= 0 {
			rate = DefaultScanRate
		}
		ss := &sensorSlot[E]{s: s, rate: rate}
		if pr, ok := paperSensorRate(p.EntityType, st.Key); ok {
			ss.rate = pr
		}
		// randomlyDelayStart draws from the construction-time scan rate.
		ss.timeLeft = int64(r.IntN(rate))
		b.sensors = append(b.sensors, ss)
		for _, m := range s.Requires() {
			b.mem.Register(m)
		}
	}
	if p.Activities != nil {
		for _, a := range p.Activities(e) {
			b.AddActivity(a)
		}
	}
	if saved != nil {
		saved.restore(&b.mem)
	}
	b.core = setOf(ActCore)
	b.UseDefaultActivity()
	return b
}

// activityBehaviours are the behaviours of one activity at one priority.
type activityBehaviours[E any] struct {
	act    Activity
	bucket int
	seq    int
	list   []Control[E]
}

type priorityGroup[E any] struct {
	prio int
	acts []*activityBehaviours[E]
}

// Brain is Java's Brain: memories, sensors and behaviours grouped by
// activity and priority. A brain belongs to one mob and is ticked from that
// mob's tick (Mob.customServerAiStep / the mob's brain tick).
type Brain[E any] struct {
	mem                Memories
	entityType         string
	sensors            []*sensorSlot[E]
	prios              []*priorityGroup[E]
	requirements       map[Activity][]Req
	eraseOnStop        map[Activity][]MemoryID
	core               activitySet
	active             activitySet
	def                Activity
	schedule           func(c *Ctx) (Activity, bool)
	lastScheduleUpdate int64
	seq                int
	running            []Control[E]
}

// Mem returns the brain's memory store.
func (b *Brain[E]) Mem() *Memories { return &b.mem }

// RegisterMemory registers a memory outside the provider (registerMemory).
func (b *Brain[E]) RegisterMemory(id MemoryID) { b.mem.Register(id) }

// AddActivity is Brain.addActivity. Within one priority, activities run in
// java.util.HashMap order of their names' hashes, as in Java.
func (b *Brain[E]) AddActivity(a ActivityData[E]) {
	if b.requirements == nil {
		b.requirements = map[Activity][]Req{}
		b.eraseOnStop = map[Activity][]MemoryID{}
	}
	b.requirements[a.Activity] = a.Conditions
	if len(a.EraseOnStop) > 0 {
		b.eraseOnStop[a.Activity] = a.EraseOnStop
	}
	for _, p := range a.Behaviours {
		for _, m := range p.Control.RequiredMemories() {
			b.mem.Register(m)
		}
		b.applyPaperRate(p.Control)
		g := b.group(p.Priority)
		var ab *activityBehaviours[E]
		for _, x := range g.acts {
			if x.act == a.Activity {
				ab = x
				break
			}
		}
		if ab == nil {
			b.seq++
			ab = &activityBehaviours[E]{act: a.Activity, bucket: javaBucket(a.Activity.javaHash()), seq: b.seq}
			g.acts = append(g.acts, ab)
			sort.SliceStable(g.acts, func(i, j int) bool {
				if g.acts[i].bucket != g.acts[j].bucket {
					return g.acts[i].bucket < g.acts[j].bucket
				}
				return g.acts[i].seq < g.acts[j].seq
			})
		}
		dup := false
		for _, c := range ab.list {
			if c == p.Control {
				dup = true
				break
			}
		}
		if !dup {
			ab.list = append(ab.list, p.Control)
		}
	}
}

func (b *Brain[E]) applyPaperRate(c Control[E]) {
	switch x := c.(type) {
	case *Behavior[E]:
		x.paperRate = PaperBehaviorRate(b.entityType, strings.ToLower(x.name))
	case *Gate[E]:
		for _, ch := range x.list {
			b.applyPaperRate(ch.c)
		}
	}
}

func (b *Brain[E]) group(prio int) *priorityGroup[E] {
	i := sort.Search(len(b.prios), func(i int) bool { return b.prios[i].prio >= prio })
	if i < len(b.prios) && b.prios[i].prio == prio {
		return b.prios[i]
	}
	g := &priorityGroup[E]{prio: prio}
	b.prios = append(b.prios, nil)
	copy(b.prios[i+1:], b.prios[i:])
	b.prios[i] = g
	return g
}

// RemoveAllBehaviours is Brain.removeAllBehaviors (used when a villager
// rebuilds its brain after a profession change).
func (b *Brain[E]) RemoveAllBehaviours() { b.prios = nil }

// SetSchedule sets the function that picks the scheduled activity
// (Brain.setSchedule; Java 26 reads it from an environment attribute at the
// mob's position). ok=false means no schedule: idle.
func (b *Brain[E]) SetSchedule(f func(c *Ctx) (Activity, bool)) { b.schedule = f }

// SetCoreActivities is Brain.setCoreActivities.
func (b *Brain[E]) SetCoreActivities(acts ...Activity) { b.core = setOf(acts...) }

// SetDefaultActivity is Brain.setDefaultActivity.
func (b *Brain[E]) SetDefaultActivity(a Activity) { b.def = a }

// UseDefaultActivity is Brain.useDefaultActivity.
func (b *Brain[E]) UseDefaultActivity() { b.setActive(b.def) }

// IsActive is Brain.isActive.
func (b *Brain[E]) IsActive(a Activity) bool { return b.active.has(a) }

// ActiveActivities returns the active activities in registry order.
func (b *Brain[E]) ActiveActivities() []Activity {
	var out []Activity
	for a := Activity(0); a < 64; a++ {
		if b.active.has(a) {
			out = append(out, a)
		}
	}
	return out
}

// ActiveNonCoreActivity is Brain.getActiveNonCoreActivity.
func (b *Brain[E]) ActiveNonCoreActivity() (Activity, bool) {
	for a := Activity(0); a < 64; a++ {
		if b.active.has(a) && !b.core.has(a) {
			return a, true
		}
	}
	return 0, false
}

// SetActiveActivityIfPossible is Brain.setActiveActivityIfPossible.
func (b *Brain[E]) SetActiveActivityIfPossible(a Activity) {
	if b.requirementsMet(a) {
		b.setActive(a)
	} else {
		b.UseDefaultActivity()
	}
}

// SetActiveActivityToFirstValid is Brain.setActiveActivityToFirstValid.
func (b *Brain[E]) SetActiveActivityToFirstValid(acts ...Activity) {
	for _, a := range acts {
		if b.requirementsMet(a) {
			b.setActive(a)
			return
		}
	}
}

func (b *Brain[E]) setActive(a Activity) {
	if b.active.has(a) {
		return
	}
	// eraseMemoriesForOtherActivitesThan
	for old := Activity(0); old < 64; old++ {
		if old != a && b.active.has(old) {
			for _, m := range b.eraseOnStop[old] {
				b.mem.Erase(m)
			}
		}
	}
	b.active = b.core
	b.active.add(a)
}

func (b *Brain[E]) requirementsMet(a Activity) bool {
	reqs, ok := b.requirements[a]
	if !ok {
		return false
	}
	return b.mem.CheckAll(reqs)
}

// scheduleUpdateDelay is Brain.SCHEDULE_UPDATE_DELAY.
const scheduleUpdateDelay = 20

// UpdateActivityFromSchedule is Brain.updateActivityFromSchedule: at most
// every 20 ticks, switch to the scheduled activity if it is not active.
func (b *Brain[E]) UpdateActivityFromSchedule(c *Ctx) {
	if c.Time-b.lastScheduleUpdate <= scheduleUpdateDelay {
		return
	}
	b.lastScheduleUpdate = c.Time
	scheduled := ActIdle
	if b.schedule != nil {
		if a, ok := b.schedule(c); ok {
			scheduled = a
		}
	}
	if !b.active.has(scheduled) {
		b.SetActiveActivityIfPossible(scheduled)
	}
}

// Tick is Brain.tick: forget expired memories, run due sensors, try to start
// every stopped behaviour of the active activities (by priority), then tick
// every running behaviour (including ones whose activity is no longer
// active: they stop by themselves, as in Java). c.Mem is set to this brain.
func (b *Brain[E]) Tick(c Ctx, e E) {
	c.Mem = &b.mem
	b.mem.tick()
	for _, s := range b.sensors {
		s.tick(&c, e)
	}
	for _, g := range b.prios {
		for _, ab := range g.acts {
			if !b.active.has(ab.act) {
				continue
			}
			for _, bh := range ab.list {
				if bh.State() == Stopped {
					bh.TryStart(&c, e)
				}
			}
		}
	}
	running := b.collectRunning(b.running[:0])
	for _, bh := range running {
		bh.TickOrStop(&c, e)
	}
	clear(running)
	b.running = running[:0]
}

func (b *Brain[E]) collectRunning(dst []Control[E]) []Control[E] {
	for _, g := range b.prios {
		for _, ab := range g.acts {
			for _, bh := range ab.list {
				if bh.State() == Running {
					dst = append(dst, bh)
				}
			}
		}
	}
	return dst
}

// RunningBehaviours is Brain.getRunningBehaviors.
func (b *Brain[E]) RunningBehaviours() []Control[E] { return b.collectRunning(nil) }

// StopAll is Brain.stopAll.
func (b *Brain[E]) StopAll(c Ctx, e E) {
	c.Mem = &b.mem
	for _, bh := range b.collectRunning(nil) {
		bh.DoStop(&c, e)
	}
}

// Ctx returns c with Mem pointing at this brain, for calling behaviours or
// sensors from outside Tick (e.g. a mob's interaction handler).
func (b *Brain[E]) Ctx(c Ctx) *Ctx {
	c.Mem = &b.mem
	return &c
}

// IsBrainDead is Brain.isBrainDead.
func (b *Brain[E]) IsBrainDead() bool {
	return len(b.mem.order) == 0 && len(b.sensors) == 0 && len(b.prios) == 0
}

// Copy helpers so callers need not reach into Mem().

// GetM is Get on b's memories.
func GetM[T any, E any](b *Brain[E], m Memory[T]) (T, bool) { return Get(&b.mem, m) }

// SetM is Set on b's memories.
func SetM[T any, E any](b *Brain[E], m Memory[T], v T) { Set(&b.mem, m, v) }
