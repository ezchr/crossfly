//go:build crossfly_full

package combat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	_ "github.com/df-mc/dragonfly/server/mob/component/combat"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// The tests run without the other mob packages: a tiny filter compiler, a
// fake navigation that walks in a straight line, and look/move controls
// that turn and move at once stand in for A2, B and C.

// ---- filters ----

type testFilter struct {
	test, subject, op string
	value             any
	all, any, none    []testFilter
}

func compileTest(raw json.RawMessage) (mob.Filter, error) {
	var f testFilter
	if err := parseTestFilter(raw, &f); err != nil {
		// Legacy forms (AND/OR, numeric subjects) are A2's job: false here.
		return mob.FilterFunc(func(*mob.FilterContext) bool { return false }), nil
	}
	return mob.FilterFunc(f.eval), nil
}

func parseTestFilter(raw json.RawMessage, f *testFilter) error {
	if len(raw) > 0 && raw[0] == '[' {
		var list []json.RawMessage
		if err := json.Unmarshal(raw, &list); err != nil {
			return err
		}
		for _, r := range list {
			var c testFilter
			if err := parseTestFilter(r, &c); err != nil {
				return err
			}
			f.all = append(f.all, c)
		}
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return err
	}
	for k, dst := range map[string]*[]testFilter{"all_of": &f.all, "any_of": &f.any, "none_of": &f.none} {
		if r, ok := m[k]; ok {
			var sub testFilter
			if err := parseTestFilter(r, &sub); err != nil {
				return err
			}
			if len(r) > 0 && r[0] == '[' {
				*dst = append(*dst, sub.all...)
			} else {
				*dst = append(*dst, sub)
			}
		}
	}
	_ = json.Unmarshal(m["test"], &f.test)
	_ = json.Unmarshal(m["subject"], &f.subject)
	_ = json.Unmarshal(m["operator"], &f.op)
	if v, ok := m["value"]; ok {
		_ = json.Unmarshal(v, &f.value)
	}
	if f.test == "" && f.all == nil && f.any == nil && f.none == nil {
		return fmt.Errorf("empty test filter %s", raw)
	}
	return nil
}

func (f testFilter) eval(ctx *mob.FilterContext) bool {
	for _, c := range f.all {
		if !c.eval(ctx) {
			return false
		}
	}
	if len(f.any) > 0 && !slices.ContainsFunc(f.any, func(c testFilter) bool { return c.eval(ctx) }) {
		return false
	}
	if slices.ContainsFunc(f.none, func(c testFilter) bool { return c.eval(ctx) }) {
		return false
	}
	if f.test == "" {
		return true
	}
	var subj world.Entity = ctx.Self
	switch f.subject {
	case "other":
		subj = ctx.Other
	case "target":
		subj = ctx.Target
	}
	var r bool
	switch f.test {
	case "is_family":
		r = subj != nil && hasFamily(subj, f.value.(string))
	case "has_component":
		m, ok := mob.Of(subj)
		r = ok && m.HasComponent(f.value.(string))
	case "is_daytime":
		r = sense.IsDay(ctx.Tx)
	default:
		return false
	}
	if b, ok := f.value.(bool); ok && !b {
		r = !r
	}
	if f.op == "not" || f.op == "!=" {
		r = !r
	}
	return r
}

// ---- controls ----

type fakePath struct {
	end      cube.Pos
	complete bool
}

func (p *fakePath) Len() int          { return 1 }
func (p *fakePath) Index() int        { return 0 }
func (p *fakePath) Node(int) cube.Pos { return p.end }
func (p *fakePath) End() cube.Pos     { return p.end }
func (p *fakePath) Complete() bool    { return p.complete }
func (p *fakePath) Done() bool        { return false }

// fakeNav walks straight to its goal at speed x 0.1 blocks a tick.
type fakeNav struct {
	goal     mgl64.Vec3
	follow   *world.EntityHandle
	speed    float64
	active   bool
	moves    int
	stops    int
	noPath   bool
	lastMove string
}

func (n *fakeNav) Tick(m *mob.Mob) {
	if !n.active {
		return
	}
	if n.follow != nil {
		if e, ok := n.follow.Entity(m.Tx()); ok {
			n.goal = e.Position()
		}
	}
	d := n.goal.Sub(m.Pos())
	d[1] = 0
	step := 0.1 * n.speed
	if d.Len() <= step {
		m.SetPosition(mgl64.Vec3{n.goal[0], m.Pos()[1], n.goal[2]})
		if n.follow == nil {
			n.active = false
		}
		return
	}
	m.SetYaw(sense.YawTowards(m.Pos(), n.goal))
	m.SetPosition(m.Pos().Add(d.Normalize().Mul(step)))
}
func (n *fakeNav) CreatePath(_ *mob.Mob, pos mgl64.Vec3) mob.Path {
	if n.noPath {
		return nil
	}
	return &fakePath{end: cube.PosFromVec3(pos), complete: true}
}
func (n *fakeNav) CreatePathToEntity(m *mob.Mob, e world.Entity) mob.Path {
	return n.CreatePath(m, e.Position())
}
func (n *fakeNav) MoveTo(_ *mob.Mob, pos mgl64.Vec3, speed float64) bool {
	n.goal, n.follow, n.speed, n.active = pos, nil, speed, true
	n.moves++
	n.lastMove = "pos"
	return true
}
func (n *fakeNav) MoveToEntity(_ *mob.Mob, e world.Entity, speed float64) bool {
	n.goal, n.follow, n.speed, n.active = e.Position(), e.H(), speed, true
	n.moves++
	n.lastMove = "entity"
	return true
}
func (n *fakeNav) MoveAlong(m *mob.Mob, p mob.Path, speed float64) bool {
	if fp, ok := p.(*fakePath); ok {
		n.goal = fp.end.Vec3Middle()
	}
	n.speed, n.active = speed, true
	n.moves++
	return true
}
func (n *fakeNav) Path() mob.Path {
	if !n.active {
		return nil
	}
	return &fakePath{end: cube.PosFromVec3(n.goal), complete: true}
}
func (n *fakeNav) Stop(*mob.Mob) {
	n.active, n.follow = false, nil
	n.stops++
}
func (n *fakeNav) Done() bool             { return !n.active }
func (n *fakeNav) SetSpeed(speed float64) { n.speed = speed }

// fakeLook turns head and pitch at once.
type fakeLook struct {
	at     mgl64.Vec3
	entity *world.EntityHandle
	want   bool
}

func (l *fakeLook) Tick(m *mob.Mob) {
	if !l.want {
		return
	}
	at := l.at
	if l.entity != nil {
		if e, ok := l.entity.Entity(m.Tx()); ok {
			at = sense.EyePos(e)
		}
	}
	lookAt(m, at)
	l.want = false
}
func (l *fakeLook) SetLookAt(pos mgl64.Vec3, _, _ float64) { l.at, l.entity, l.want = pos, nil, true }
func (l *fakeLook) SetLookAtEntity(e world.Entity, _, _ float64) {
	l.entity, l.want = e.H(), true
}
func (l *fakeLook) HasWanted() bool { return l.want }

func lookAt(m *mob.Mob, at mgl64.Vec3) {
	from := m.Pos()
	d := at.Sub(from)
	m.SetHeadYaw(sense.YawTowards(from, at))
	m.SetPitch(-mgl64.RadToDeg(math.Atan2(d[1], math.Hypot(d[0], d[2]))))
}

type fakeMove struct{ stops int }

func (*fakeMove) Tick(*mob.Mob)                         {}
func (*fakeMove) SetWantedPosition(mgl64.Vec3, float64) {}
func (*fakeMove) HasWanted() bool                       { return false }
func (*fakeMove) WantedPosition() mgl64.Vec3            { return mgl64.Vec3{} }
func (*fakeMove) Speed() float64                        { return 0 }
func (*fakeMove) Strafe(float64, float64)               {}
func (m *fakeMove) Stop()                               { m.stops++ }

type navKey struct{}

func navOf(m *mob.Mob) *fakeNav { v, _ := m.Data(navKey{}); return v.(*fakeNav) }

// ---- definitions ----

// testDef is a definition with base components, named groups and events
// that add/remove groups (the part of package mob/def the tests need).
type testDef struct {
	id     string
	base   []mob.RawComponent
	groups map[string][]mob.RawComponent
	events map[string]testEvent
}

type testEvent struct{ add, remove []string }

func (d *testDef) Identifier() string { return d.id }
func (d *testDef) Description() mob.Description {
	return mob.Description{Identifier: d.id}
}
func (d *testDef) ComponentKeys() []string {
	var keys []string
	for _, c := range d.base {
		keys = append(keys, c.Key)
	}
	for _, g := range d.groups {
		for _, c := range g {
			keys = append(keys, c.Key)
		}
	}
	return keys
}
func (d *testDef) NewInstance() mob.Instance { return &testInst{d: d} }

type testInst struct {
	d      *testDef
	active []string
	fired  []string
}

func (i *testInst) Components() []mob.RawComponent {
	out := slices.Clone(i.d.base)
	for _, g := range i.active {
		for _, c := range i.d.groups[g] {
			idx := slices.IndexFunc(out, func(o mob.RawComponent) bool { return o.Key == c.Key })
			if idx >= 0 {
				out[idx] = c
			} else {
				out = append(out, c)
			}
		}
	}
	return out
}
func (i *testInst) Fire(_ *mob.Mob, ev string, _ *mob.FilterContext) bool {
	i.fired = append(i.fired, ev)
	e, ok := i.d.events[ev]
	if !ok {
		return false
	}
	changed := false
	for _, g := range e.remove {
		if idx := slices.Index(i.active, g); idx >= 0 {
			i.active = slices.Delete(i.active, idx, idx+1)
			changed = true
		}
	}
	for _, g := range e.add {
		if !slices.Contains(i.active, g) {
			i.active = append(i.active, g)
			changed = true
		}
	}
	return changed
}
func (i *testInst) Property(string) (any, bool)     { return nil, false }
func (i *testInst) SetProperty(string, any) error   { return nil }
func (i *testInst) Properties() []mob.PropertyValue { return nil }
func (i *testInst) EncodeNBT() map[string]any       { return nil }
func (i *testInst) DecodeNBT(map[string]any)        {}

func fired(m *mob.Mob) []string { return m.Instance().(*testInst).fired }

// comps parses `{"key": {...}, ...}` into ordered components.
func comps(js string) []mob.RawComponent {
	c, err := mob.ParseOrderedObject([]byte(js))
	if err != nil {
		panic(err)
	}
	return c
}

var testTypes = map[string]*testDef{}

func def(id, base string, groups map[string]string, events map[string]testEvent) {
	d := &testDef{id: id, base: comps(base), groups: map[string][]mob.RawComponent{}, events: events}
	for g, js := range groups {
		d.groups[g] = comps(js)
	}
	testTypes[id] = d
	mob.RegisterType(d)
}

func init() {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	mob.SetFilterCompiler(compileTest)
	mob.RegisterInitHook(mob.InitHook{Name: "test/controls", Init: func(m *mob.Mob) {
		n := &fakeNav{}
		m.SetData(navKey{}, n)
		m.SetNavigation(n)
		m.SetLookControl(&fakeLook{})
		m.SetMoveControl(&fakeMove{})
	}})
	// Families without package mob/component/core: "test:family" sets them.
	mob.RegisterComponent("test:family", func(_ string, raw json.RawMessage) (mob.Component, error) {
		var v struct{ Family []string }
		_ = json.Unmarshal(raw, &v)
		return familyComp(v.Family), nil
	})
	mob.RegisterComponent("minecraft:attack", func(string, json.RawMessage) (mob.Component, error) {
		return familyComp(nil), nil
	})
	defineMobs()
}

type familyComp []string

func (f familyComp) Attach(m *mob.Mob) {
	if f != nil {
		m.SetFamilies(f)
	}
}
func (familyComp) Detach(*mob.Mob) {}

// ---- world ----

type env struct {
	t  *testing.T
	w  *world.World
	tx *world.Tx
}

func run(t *testing.T, f func(e *env)) {
	t.Helper()
	reg := entity.DefaultRegistry.Config().New(append(entity.DefaultRegistry.Types(), mob.Types()...))
	w := world.Config{Entities: reg, Provider: world.NopProvider{}, Generator: world.NopGenerator{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}.New()
	t.Cleanup(func() { _ = w.Close() })
	w.SetDifficulty(world.DifficultyNormal)
	w.SetTime(18000) // night unless a test wants day
	w.StopTime()
	_, _ = world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
		for x := -24; x <= 24; x++ {
			for z := -24; z <= 24; z++ {
				tx.SetBlock(cube.Pos{x, 9, z}, block.Stone{}, nil)
			}
		}
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(failNow); !ok {
					panic(r)
				}
			}
		}()
		f(&env{t: t, w: w, tx: tx})
		return struct{}{}, nil
	})
}

func (e *env) spawn(id string, pos mgl64.Vec3) *mob.Mob {
	e.t.Helper()
	m, err := mob.Spawn(e.tx, id, pos, mob.SpawnOptions{})
	if err != nil {
		fatal(e.t, err)
	}
	m.SetRandSeed(1, 2)
	return m
}

func (e *env) player(pos mgl64.Vec3) *player.Player {
	h := world.EntitySpawnOpts{Position: pos}.New(player.Type, player.Config{Name: "p", Position: pos, GameMode: world.GameModeSurvival})
	return e.tx.AddEntity(h).(*player.Player)
}

// tick ticks the mobs n times; stop ends early when it returns true.
func (e *env) tick(n int, ms []*mob.Mob, stop func(i int) bool) int {
	for i := 0; i < n; i++ {
		for _, m := range ms {
			if mm, ok := mob.OfHandle(e.tx, m.H()); ok {
				mm.Tick(e.tx, int64(i))
			}
		}
		if stop != nil && stop(i) {
			return i
		}
	}
	return n
}

// failNow stops a test from inside a world transaction: t.Fatal there
// would end the world's goroutine and hang world.Call.
type failNow struct{}

func fatal(t *testing.T, args ...any) {
	t.Helper()
	t.Error(args...)
	panic(failNow{})
}

func fatalf(t *testing.T, format string, args ...any) {
	t.Helper()
	t.Errorf(format, args...)
	panic(failNow{})
}
