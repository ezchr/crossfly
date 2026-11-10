package jbrain

import (
	"bytes"
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

type body struct {
	log []string
}

var (
	testMemA    = RegisterMemory[int]("test:a")
	testMemB    = RegisterMemory[int]("test:b")
	testMemList = RegisterMemory[[]int]("test:list")
	testMemSave = RegisterSavedMemory("test:saved", LongCodec)
	testMemNope = RegisterMemory[int]("test:unregistered")
)

func newMem(ids ...MemoryID) *Memories {
	m := &Memories{}
	for _, id := range ids {
		m.Register(id)
	}
	return m
}

func TestMemoryExpiry(t *testing.T) {
	m := newMem(testMemA.ID())
	SetWithExpiry(m, testMemA, 7, 2)
	// MemorySlot.tick: ttl 2 -> 1 -> 0 -> cleared on the third tick.
	for i := range 2 {
		m.tick()
		if v, ok := Get(m, testMemA); !ok || v != 7 {
			t.Fatalf("tick %d: memory gone early", i+1)
		}
	}
	m.tick()
	if m.Has(testMemA.ID()) {
		t.Fatal("memory with ttl 2 still present after 3 ticks")
	}
	Set(m, testMemA, 3)
	for range 100 {
		m.tick()
	}
	if !m.Has(testMemA.ID()) {
		t.Fatal("memory without expiry expired")
	}
}

func TestMemoryEmptyCollectionAndUnregistered(t *testing.T) {
	m := newMem(testMemList.ID(), testMemA.ID())
	Set(m, testMemList, []int{1})
	if !m.Has(testMemList.ID()) {
		t.Fatal("list not set")
	}
	Set(m, testMemList, []int{})
	if m.Has(testMemList.ID()) {
		t.Fatal("empty list should clear the memory (Brain.isEmptyCollection)")
	}
	Set(m, testMemNope, 5)
	if _, ok := Get(m, testMemNope); ok {
		t.Fatal("unregistered memory stored a value")
	}
	if m.Check(testMemNope.ID(), Registered) || m.Check(testMemNope.ID(), ValueAbsent) {
		t.Fatal("unregistered memory passed a condition")
	}
	if !m.Check(testMemA.ID(), ValueAbsent) || !m.Check(testMemA.ID(), Registered) || m.Check(testMemA.ID(), ValuePresent) {
		t.Fatal("conditions of an empty registered memory wrong")
	}
}

// countTask counts calls and runs while keep is true.
type countTask struct {
	TaskBase[*body]
	starts, ticks, stops int
	keep                 bool
}

func (c *countTask) Start(*Ctx, *body)            { c.starts++ }
func (c *countTask) Tick(*Ctx, *body)             { c.ticks++ }
func (c *countTask) Stop(*Ctx, *body)             { c.stops++ }
func (c *countTask) CanStillUse(*Ctx, *body) bool { return c.keep }

func TestBehaviorDuration(t *testing.T) {
	task := &countTask{keep: true}
	b := NewBehavior[*body]("Count", []Req{Present(testMemA)}, 5, 5, task)
	p := &Provider[*body]{Activities: func(*body) []ActivityData[*body] {
		return []ActivityData[*body]{{Activity: ActCore, Behaviours: Seq[*body](0, b)}}
	}}
	e := &body{}
	br := p.MakeBrain(e, rand.New(rand.NewPCG(1, 0)), nil)
	c := Ctx{Rand: rand.New(rand.NewPCG(2, 0))}
	br.Tick(c, e)
	if task.starts != 0 {
		t.Fatal("started without its entry memory")
	}
	SetM(br, testMemA, 1)
	// Started at time 1: end = 6; ticks at 1..6, stops at 7 (timestamp > end).
	for c.Time = 1; c.Time <= 7; c.Time++ {
		br.Tick(c, e)
	}
	if task.starts != 1 || task.ticks != 6 || task.stops != 1 {
		t.Fatalf("starts/ticks/stops = %d/%d/%d, want 1/6/1", task.starts, task.ticks, task.stops)
	}
	// The default CanStillUse (false) stops a behaviour in its first tick.
	task2 := &struct{ countTask }{}
	b2 := NewBehavior[*body]("Once", nil, 60, 60, &task2.countTask)
	br.AddActivity(ActivityData[*body]{Activity: ActCore, Behaviours: Seq[*body](1, b2)})
	br.Tick(c, e)
	if task2.starts != 1 || task2.ticks != 0 || task2.stops != 1 {
		t.Fatalf("starts/ticks/stops = %d/%d/%d, want 1/0/1", task2.starts, task2.ticks, task2.stops)
	}
}

func TestActivityHashOrder(t *testing.T) {
	// java.util.HashMap iteration order of these activity names, measured
	// with OpenJDK 25: [play, core, rest, hide, meet, idle, pre_raid, work, panic, raid].
	ins := []Activity{ActPanic, ActCore, ActIdle, ActRaid, ActPreRaid, ActHide, ActMeet, ActRest, ActWork, ActPlay}
	want := []Activity{ActPlay, ActCore, ActRest, ActHide, ActMeet, ActIdle, ActPreRaid, ActWork, ActPanic, ActRaid}
	br := (&Provider[*body]{}).MakeBrain(&body{}, rand.New(rand.NewPCG(1, 0)), nil)
	for _, a := range ins {
		br.AddActivity(ActivityData[*body]{Activity: a, Behaviours: Seq[*body](0, Trigger[*body]("x", nil, func(*Ctx, *body) bool { return false }))})
	}
	var got []Activity
	for _, ab := range br.prios[0].acts {
		got = append(got, ab.act)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order %v, want %v", got, want)
	}
	ins = []Activity{ActCore, ActFight, ActCelebrate, ActAdmireItem, ActAvoid, ActRide, ActIdle}
	want = []Activity{ActCore, ActAdmireItem, ActCelebrate, ActIdle, ActAvoid, ActRide, ActFight}
	br = (&Provider[*body]{}).MakeBrain(&body{}, rand.New(rand.NewPCG(1, 0)), nil)
	for _, a := range ins {
		br.AddActivity(ActivityData[*body]{Activity: a, Behaviours: Seq[*body](0, Trigger[*body]("x", nil, func(*Ctx, *body) bool { return false }))})
	}
	got = got[:0]
	for _, ab := range br.prios[0].acts {
		got = append(got, ab.act)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order %v, want %v", got, want)
	}
}

func TestPriorityOrderAndRunningOutsideActivity(t *testing.T) {
	e := &body{}
	mk := func(name string) *OneShot[*body] {
		return Trigger[*body](name, nil, func(_ *Ctx, b *body) bool { b.log = append(b.log, name); return true })
	}
	long := &countTask{keep: true}
	p := &Provider[*body]{Activities: func(*body) []ActivityData[*body] {
		return []ActivityData[*body]{
			{Activity: ActCore, Behaviours: []Prioritized[*body]{{Priority: 5, Control: mk("core5")}, {Priority: 0, Control: mk("core0")}}},
			{Activity: ActIdle, Behaviours: []Prioritized[*body]{{Priority: 2, Control: mk("idle2")}, {Priority: 3, Control: NewBehavior[*body]("Long", nil, 100, 100, long)}}},
			{Activity: ActWork, Behaviours: Seq[*body](1, mk("work1"))},
		}
	}}
	br := p.MakeBrain(e, rand.New(rand.NewPCG(1, 0)), nil)
	c := Ctx{Rand: rand.New(rand.NewPCG(3, 0))}
	br.Tick(c, e)
	if want := []string{"core0", "idle2", "core5"}; !reflect.DeepEqual(e.log, want) {
		t.Fatalf("log %v, want %v", e.log, want)
	}
	if long.starts != 1 {
		t.Fatal("idle behaviour did not start")
	}
	// Work has no requirements: switching works and the idle behaviour keeps
	// running although idle is no longer active (Brain.getRunningBehaviors).
	br.SetActiveActivityIfPossible(ActWork)
	if !br.IsActive(ActWork) || br.IsActive(ActIdle) || !br.IsActive(ActCore) {
		t.Fatalf("active %v", br.ActiveActivities())
	}
	e.log = nil
	c.Time++
	br.Tick(c, e)
	if want := []string{"core0", "work1", "core5"}; !reflect.DeepEqual(e.log, want) {
		t.Fatalf("log %v, want %v", e.log, want)
	}
	if long.ticks != 2 || long.stops != 0 {
		t.Fatalf("long behaviour ticks/stops %d/%d, want 2/0", long.ticks, long.stops)
	}
}

func TestActivityConditionsAndErase(t *testing.T) {
	e := &body{}
	// Activity conditions do not register memories (Brain.addActivity only
	// registers behaviour memories), so the provider lists it.
	p := &Provider[*body]{Memories: []MemoryID{testMemA.ID()}, Activities: func(*body) []ActivityData[*body] {
		return []ActivityData[*body]{
			ActivityErasing[*body](ActPanic, 0, testMemA.ID(), Trigger[*body]("p", nil, func(*Ctx, *body) bool { return false })),
			{Activity: ActIdle},
		}
	}}
	br := p.MakeBrain(e, rand.New(rand.NewPCG(1, 0)), nil)
	br.SetActiveActivityToFirstValid(ActPanic, ActIdle)
	if br.IsActive(ActPanic) {
		t.Fatal("panic active without its memory")
	}
	SetM(br, testMemA, 1)
	br.SetActiveActivityToFirstValid(ActPanic, ActIdle)
	if !br.IsActive(ActPanic) {
		t.Fatal("panic not active")
	}
	br.SetActiveActivityIfPossible(ActIdle)
	if br.Mem().Has(testMemA.ID()) {
		t.Fatal("memory to erase when stopped survived the switch")
	}
}

func TestSchedule(t *testing.T) {
	e := &body{}
	p := &Provider[*body]{Activities: func(*body) []ActivityData[*body] {
		return []ActivityData[*body]{{Activity: ActIdle}, {Activity: ActRest}}
	}}
	br := p.MakeBrain(e, rand.New(rand.NewPCG(1, 0)), nil)
	calls := 0
	br.SetSchedule(func(*Ctx) (Activity, bool) { calls++; return ActRest, true })
	for tm := int64(0); tm < 50; tm++ {
		br.UpdateActivityFromSchedule(&Ctx{Time: tm})
	}
	// lastScheduleUpdate starts at -9999: updates at 0, 21, 42.
	if calls != 3 || !br.IsActive(ActRest) {
		t.Fatalf("calls %d, rest active %v", calls, br.IsActive(ActRest))
	}
}

type countSensor struct{ n *int }

func (s countSensor) Requires() []MemoryID  { return []MemoryID{testMemB.ID()} }
func (s countSensor) Sense(c *Ctx, _ *body) { *s.n++; Set(c.Mem, testMemB, *s.n) }

func TestSensorRates(t *testing.T) {
	n := 0
	st := &SensorType[*body]{Name: "count", Key: "countsensor", New: func() Sensor[*body] { return countSensor{&n} }}
	p := &Provider[*body]{EntityType: "testmob", Sensors: []*SensorType[*body]{st}}
	e := &body{}
	br := p.MakeBrain(e, rand.New(rand.NewPCG(1, 0)), nil)
	for range 200 {
		br.Tick(Ctx{}, e)
	}
	if n < 10 || n > 11 {
		t.Fatalf("sensor ran %d times in 200 ticks, want 10 (every 20)", n)
	}
	if v, _ := GetM(br, testMemB); v != n {
		t.Fatal("sensor memory not written")
	}
	SetPaperSensorRate("testmob", "countsensor", 40)
	defer SetPaperSensorRate("testmob", "countsensor", -1)
	n = 0
	br = p.MakeBrain(e, rand.New(rand.NewPCG(1, 0)), nil)
	for range 400 {
		br.Tick(Ctx{}, e)
	}
	if n < 10 || n > 11 {
		t.Fatalf("Paper-rated sensor ran %d times in 400 ticks, want 10 (every 40)", n)
	}
}

func TestRunOneWeights(t *testing.T) {
	counts := map[string]int{}
	mk := func(name string) *OneShot[*body] {
		return Trigger[*body](name, nil, func(*Ctx, *body) bool { counts[name]++; return true })
	}
	g := RunOne[*body](nil, W[*body](mk("a"), 1), W[*body](mk("b"), 3))
	m := newMem()
	c := &Ctx{Rand: rand.New(rand.NewPCG(9, 0)), Mem: m}
	e := &body{}
	for range 4000 {
		g.TryStart(c, e)
		g.TickOrStop(c, e)
	}
	if counts["a"]+counts["b"] != 4000 {
		t.Fatalf("RunOne ran %d children in 4000 starts", counts["a"]+counts["b"])
	}
	// P(b first) = 3/4 for weights 1 and 3.
	if r := float64(counts["b"]) / 4000; r < 0.72 || r > 0.78 {
		t.Fatalf("weight 3 child chosen %.3f of the time, want 0.75", r)
	}
	if g.State() != Stopped {
		t.Fatal("gate still running after its one-shot children stopped")
	}
}

func TestGateEraseOnStop(t *testing.T) {
	m := newMem(testMemA.ID())
	Set(m, testMemA, 1)
	g := NewGate[*body](nil, []MemoryID{testMemA.ID()}, Ordered, TryAll, []Weighted[*body]{W[*body](Trigger[*body]("t", nil, func(*Ctx, *body) bool { return true }), 1)})
	c := &Ctx{Rand: rand.New(rand.NewPCG(1, 0)), Mem: m}
	g.TryStart(c, &body{})
	g.TickOrStop(c, &body{})
	if m.Has(testMemA.ID()) {
		t.Fatal("gate did not erase its exit memory")
	}
}

func TestPackRoundTrip(t *testing.T) {
	p := &Provider[*body]{Memories: []MemoryID{MemHome.ID(), MemLastSlept.ID(), MemGolemDetectedRecently.ID(), MemAngryAt.ID(), testMemA.ID(), testMemSave.ID()}}
	br := p.MakeBrain(&body{}, rand.New(rand.NewPCG(1, 0)), nil)
	home := GlobalPos{Dim: world.Overworld, Pos: cube.Pos{10, 64, -3}}
	u := uuid.MustParse("01234567-89ab-cdef-0123-456789abcdef")
	SetM(br, MemHome, home)
	SetWithExpiry(br.Mem(), MemGolemDetectedRecently, true, 600)
	SetM(br, MemLastSlept, 12345)
	SetM(br, MemAngryAt, u)
	SetM(br, testMemA, 9) // not saved: no codec
	SetM(br, testMemSave, 77)

	data, err := nbt.MarshalEncoding(br.Pack().NBT(), nbt.LittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := nbt.NewDecoderWithEncoding(bytes.NewReader(data), nbt.LittleEndian).Decode(&back); err != nil {
		t.Fatal(err)
	}
	br2 := p.MakeBrain(&body{}, rand.New(rand.NewPCG(1, 0)), Unpack(back))
	if v, ok := GetM(br2, MemHome); !ok || v != home {
		t.Fatalf("home %v %v", v, ok)
	}
	if v, ok := GetM(br2, MemGolemDetectedRecently); !ok || !v || br2.Mem().TimeUntilExpiry(MemGolemDetectedRecently.ID()) != 600 {
		t.Fatal("golem_detected_recently or its ttl lost")
	}
	if v, _ := GetM(br2, MemLastSlept); v != 12345 {
		t.Fatal("last_slept lost")
	}
	if v, _ := GetM(br2, MemAngryAt); v != u {
		t.Fatal("angry_at lost")
	}
	if _, ok := GetM(br2, testMemA); ok {
		t.Fatal("memory without codec was saved")
	}
	if v, _ := GetM(br2, testMemSave); v != 77 {
		t.Fatal("custom saved memory lost")
	}
}

func TestSequenceAndTriggerIf(t *testing.T) {
	m := newMem(testMemA.ID())
	c := &Ctx{Mem: m}
	ran := 0
	then := Trigger[*body]("then", []Req{Present(testMemA)}, func(*Ctx, *body) bool { ran++; return true })
	gate := true
	o := TriggerIf[*body](func(*Ctx, *body) bool { return gate }, then)
	if o.TryStart(c, &body{}) || ran != 0 {
		t.Fatal("ran without its memory")
	}
	Set(m, testMemA, 1)
	if !o.TryStart(c, &body{}) || ran != 1 {
		t.Fatal("did not run")
	}
	o.DoStop(c, &body{})
	gate = false
	if o.TryStart(c, &body{}) || ran != 1 {
		t.Fatal("ran with a false predicate")
	}
}
