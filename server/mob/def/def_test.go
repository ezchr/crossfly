//go:build crossfly_full

package def

import (
	"encoding/json"
	"math/rand/v2"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/mob/def/molang"
	"github.com/df-mc/dragonfly/server/mob/event"
	"github.com/df-mc/dragonfly/server/mob/filter"
	"github.com/go-gl/mathgl/mgl64"
)

var vanilla *Set

func loadVanilla(t *testing.T) *Set {
	t.Helper()
	if vanilla != nil {
		return vanilla
	}
	s, err := LoadFS(os.DirFS("testdata"), "entities")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	vanilla = s
	return s
}

func TestLoadAll(t *testing.T) {
	s := loadVanilla(t)
	if s.Len() != 129 {
		t.Fatalf("loaded %d definitions, want 129", s.Len())
	}
	unknown := map[string]int{}
	actions := map[string]int{}
	for _, id := range s.IDs() {
		d := s.Get(id)
		for _, ev := range d.Events() {
			d.Event(ev).Walk(func(n *event.Response) {
				for _, a := range n.Actions {
					actions[a.Key()]++
					if u, ok := a.(event.Unknown); ok {
						unknown[u.Name]++
					}
				}
				for _, g := range append(append([]string{}, n.Add...), n.Remove...) {
					if !d.HasGroup(g) {
						t.Logf("%s: event %s references missing group %q (BDS ignores it)", id, ev, g)
					}
				}
			})
		}
		inst := d.NewInstance()
		if len(inst.Components()) != len(d.BaseComponents()) {
			t.Fatalf("%s: new instance should have exactly the base components", id)
		}
	}
	if len(unknown) > 0 {
		t.Fatalf("unknown event responses: %v", unknown)
	}
	t.Logf("event actions: %v", actions)
	cow := s.Get("minecraft:cow")
	if cow.SpawnCategory != "creature" || !cow.Spawnable || len(cow.Properties) != 2 ||
		cow.Properties[0].Name != "minecraft:climate_variant" || cow.Properties[0].Default != "temperate" || !cow.Properties[0].ClientSync {
		t.Fatalf("cow description: %+v %+v", cow, cow.Properties)
	}
	creaking := s.Get("minecraft:creaking").Property("minecraft:creaking_swaying_ticks")
	if creaking.Type != PropInt || creaking.Min != 0 || creaking.Max != 6 || creaking.Default != 0 {
		t.Fatalf("creaking property %+v", creaking)
	}
	if bee := s.Get("minecraft:bee").Property("minecraft:has_nectar"); bee.DefaultExpr == nil {
		t.Fatal("bee has_nectar default is Molang")
	}
}

// ---- stack semantics on a synthetic definition ----

const testDef = `{
  "format_version": "1.21.0",
  "minecraft:entity": {
    "description": {"identifier": "test:thing", "is_spawnable": true, "is_summonable": true,
      "properties": {
        "test:mode": {"type": "enum", "values": ["a", "b", "c"], "default": "a", "client_sync": true},
        "test:n": {"type": "int", "range": [0, 10], "default": 20},
        "test:f": {"type": "float", "range": [0, 1], "default": 0.1},
        "test:has_g1": {"type": "bool", "default": "query.had_component_group('g1')"}
      }},
    "components": {"c:a": {"v": "base"}, "c:b": {"v": "base"}},
    "component_groups": {
      "g1": {"c:a": {"v": "g1"}, "c:c": {"v": "g1"}},
      "g2": {"c:c": {"v": "g2"}},
      "g3": {"c:b": {}},
      "g4": {"c:d": {}},
      "empty": {}
    },
    "events": {}
  }
}`

func names(cs []Component) string {
	var out []string
	for _, c := range cs {
		g := c.Group()
		if g == "" {
			g = "base"
		}
		out = append(out, c.Name()+"@"+g)
	}
	return strings.Join(out, " ")
}

func stack(i *Instance) string {
	var out []string
	for _, e := range i.Stack() {
		out = append(out, e.String())
	}
	return strings.Join(out, " ")
}

func TestStack(t *testing.T) {
	d, err := Parse([]byte(testDef))
	if err != nil {
		t.Fatal(err)
	}
	i := d.NewInstance()
	if got := names(i.Components()); got != "c:a@base c:b@base" {
		t.Fatalf("base: %s", got)
	}
	diff := i.CommitDiff()
	if len(diff.Added) != 2 || len(diff.Removed) != 0 {
		t.Fatalf("first commit diff %+v", diff)
	}
	i.AddGroup("g1")
	if got := names(i.Components()); got != "c:a@g1 c:b@base c:c@g1" {
		t.Fatalf("after g1: %s", got)
	}
	diff = i.CommitDiff()
	if names(diff.Added) != "c:c@g1" || names(diff.Reinit) != "c:a@g1" {
		t.Fatalf("g1 diff added=%s reinit=%s", names(diff.Added), names(diff.Reinit))
	}
	// g2 overrides c:c; g1 still provides c:a so it stays on the stack.
	i.AddGroup("g2")
	if got := stack(i); got != "+test:thing + +g1 +g2" {
		t.Fatalf("stack %s", got)
	}
	// Adding g1 again: the old +g1, and +g2 whose only component g1
	// overrides now, are fully covered by the new entry and go.
	i.AddGroup("g1")
	if got := stack(i); got != "+test:thing + +g1" {
		t.Fatalf("stack after re-adding g1: %s", got)
	}
	if c, _ := i.Component("c:c"); c.Group() != "g1" {
		t.Fatal("re-added g1 overrides g2's c:c")
	}
	diff = i.CommitDiff()
	// c:a and c:c now come from a newer entry: rebuild them, as BDS does.
	if names(diff.Reinit) != "c:a@g1 c:c@g1" {
		t.Fatalf("re-add reinit: %s", names(diff.Reinit))
	}
	// Removing g3 removes c:b although only the base components had it.
	i.RemoveGroup("g3")
	if i.HasComponent("c:b") {
		t.Fatal("removing a group must remove its component names from the base too")
	}
	if got := stack(i); got != "+test:thing + +g1 -g3" {
		t.Fatalf("stack %s", got)
	}
	diff = i.CommitDiff()
	if names(diff.Removed) != "c:b@base" {
		t.Fatalf("removed %s", names(diff.Removed))
	}
	// A removal that overlaps nothing is dropped.
	i.RemoveGroup("g4")
	if got := stack(i); got != "+test:thing + +g1 -g3" {
		t.Fatalf("stack after no-op removal %s", got)
	}
	// Removing g1 removes c:a (base had one too) and c:c.
	i.RemoveGroup("g1")
	if got := names(i.Components()); got != "" {
		t.Fatalf("components after removing g1: %q", got)
	}
	if i.AddGroup("missing") || !i.AddGroup("g3") {
		t.Fatal("AddGroup result")
	}
	if !i.HasComponent("c:b") || i.HasGroup("g1") || !i.HasGroup("g3") {
		t.Fatal("re-adding g3")
	}
	// An empty group is "covered" by anything after it and is compacted away.
	i.AddGroup("empty")
	i.AddGroup("g4")
	if strings.Contains(stack(i), "empty") {
		t.Fatalf("empty group should be compacted: %s", stack(i))
	}
}

func TestProperties(t *testing.T) {
	d, err := Parse([]byte(testDef))
	if err != nil {
		t.Fatal(err)
	}
	i := d.NewInstance()
	if v, _ := i.PropertyValue("test:n"); v != 10 {
		t.Fatalf("int default should clamp to 10, got %v", v)
	}
	if v, _ := i.PropertyValue("test:f"); v != float64(float32(0.1)) {
		t.Fatalf("float default %v", v)
	}
	if v, _ := i.PropertyValue("test:has_g1"); v != false {
		t.Fatalf("expression default %v", v)
	}
	if err := i.SetProperty("test:mode", molang.Str("d")); err == nil {
		t.Fatal("unknown enum value accepted")
	}
	if err := i.SetProperty("test:n", molang.Num(11)); err == nil {
		t.Fatal("out of range accepted")
	}
	if err := i.SetProperty("test:nope", molang.Num(1)); err == nil {
		t.Fatal("unknown property accepted")
	}
	if err := i.SetProperty("test:mode", molang.Str("b")); err != nil {
		t.Fatal(err)
	}
	if v, _ := i.PropertyValue("test:mode"); v != "a" {
		t.Fatal("set_property must wait for Commit")
	}
	i.Commit()
	if v, _ := i.PropertyValue("test:mode"); v != "b" {
		t.Fatal("Commit applies queued properties")
	}
	if err := i.SetPropertyNow("test:n", int32(4)); err != nil {
		t.Fatal(err)
	}
	if v, _ := i.PropertyValue("test:n"); v != 4 {
		t.Fatalf("SetPropertyNow %v", v)
	}
	// Order of Properties is file order.
	if d.Properties[0].Name != "test:mode" || d.Properties[3].Name != "test:has_g1" {
		t.Fatal("property order")
	}
}

func TestNBT(t *testing.T) {
	d, _ := Parse([]byte(testDef))
	i := d.NewInstance()
	i.AddGroup("g1")
	i.AddGroup("g2")
	i.RemoveGroup("g3")
	_ = i.SetPropertyNow("test:mode", "c")
	_ = i.SetPropertyNow("test:n", 7)
	saved := i.Save()
	if got := strings.Join(saved[NBTDefinitions].([]string), " "); got != "+test:thing + +g1 +g2 -g3" {
		t.Fatalf("saved definitions %s", got)
	}
	props := saved[NBTProperties].(map[string]any)
	if props["test:n"] != int32(7) || props["test:has_g1"] != uint8(0) || props["test:mode"] != "c" {
		t.Fatalf("saved properties %#v", props)
	}
	// Simulate the NBT round trip: lists come back as []any, bytes as uint8.
	back := map[string]any{
		NBTDefinitions: []any{"+test:thing", "+", "+g1", "+g2", "-g3", "+unknown"},
		NBTProperties:  map[string]any{"test:n": int32(7), "test:mode": "c", "test:f": float32(0.5), "test:nope": int32(1)},
	}
	j := d.NewInstance()
	if err := j.Load(back); err == nil {
		t.Fatal("unknown group/property should be reported")
	}
	if names(j.Components()) != names(i.Components()) || stack(j) != stack(i) {
		t.Fatalf("loaded %s / %s, want %s / %s", names(j.Components()), stack(j), names(i.Components()), stack(i))
	}
	if v, _ := j.PropertyValue("test:has_g1"); v != true {
		t.Fatal("expression default sees the loaded groups")
	}
	if v, _ := j.PropertyValue("test:f"); v != 0.5 {
		t.Fatalf("float %v", v)
	}
	if diff := j.CommitDiff(); len(diff.Added) != len(j.Components()) {
		t.Fatal("first commit after load reports all components added")
	}
}

// ---- vanilla scenarios ----

// fakeMob is the minimum a mob needs for events: filter.Actor plus the
// instance as event.State.
type fakeMob struct {
	inst  *Instance
	pos   mgl64.Vec3
	flags map[filter.Flag]bool
	nums  map[filter.Number]float64
	equip map[filter.Domain][]filter.Item
}

func newFake(t *testing.T, id string) *fakeMob {
	d := loadVanilla(t).Get(id)
	if d == nil {
		t.Fatalf("no %s", id)
	}
	return &fakeMob{inst: d.NewInstance(), flags: map[filter.Flag]bool{}, nums: map[filter.Number]float64{}, equip: map[filter.Domain][]filter.Item{}}
}

func (m *fakeMob) Position() mgl64.Vec3 { return m.pos }
func (m *fakeMob) HasFamily(f string) bool {
	c, ok := m.inst.Component("minecraft:type_family")
	if !ok {
		return false
	}
	var tf struct{ Family []string }
	_ = json.Unmarshal(c.Raw(), &tf)
	return slices.Contains(tf.Family, f)
}
func (m *fakeMob) HasComponent(n string) bool { return m.inst.HasComponent(n) }
func (m *fakeMob) Flag(f filter.Flag) bool {
	if f == filter.FlagBaby {
		return m.inst.HasComponent("minecraft:is_baby")
	}
	return m.flags[f]
}
func (m *fakeMob) Number(n filter.Number) (float64, bool) {
	v, ok := m.nums[n]
	if n == filter.NumberRiderCount {
		return v, true
	}
	return v, ok
}
func (m *fakeMob) PropertyValue(n string) (any, bool)      { return m.inst.PropertyValue(n) }
func (m *fakeMob) Equipment(d filter.Domain) []filter.Item { return m.equip[d] }
func (m *fakeMob) EventState() event.State                 { return m.inst }

type testWorld struct {
	filter.NopWorld
	tags       map[string]bool
	difficulty int
	day        bool
}

func (w testWorld) BiomeHasTag(_ mgl64.Vec3, t string) bool { return w.tags[t] }
func (w testWorld) Difficulty() int                         { return w.difficulty }
func (w testWorld) Daytime() bool                           { return w.day }

func plains() testWorld {
	return testWorld{tags: map[string]bool{"plains": true, "animal": true}, difficulty: 2, day: true}
}

func fctx(self filter.Actor, w filter.World, seed uint64) *filter.Context {
	return &filter.Context{Self: self, World: w, Rand: rand.New(rand.NewPCG(seed, seed^7))}
}

func prop(t *testing.T, m *fakeMob, name string) any {
	t.Helper()
	v, ok := m.inst.PropertyValue(name)
	if !ok {
		t.Fatalf("no property %s", name)
	}
	return v
}

func has(m *fakeMob, cs ...string) bool {
	for _, c := range cs {
		if !m.inst.HasComponent(c) {
			return false
		}
	}
	return true
}

// componentTriggers returns the {event, filters} triggers of a sensor-like
// component (environment_sensor "triggers").
func componentTriggers(t *testing.T, m *fakeMob, name string) []*event.Trigger {
	t.Helper()
	c, ok := m.inst.Component(name)
	if !ok {
		t.Fatalf("no %s", name)
	}
	var v struct {
		Triggers any `json:"triggers"`
	}
	if err := json.Unmarshal(c.Raw(), &v); err != nil {
		t.Fatal(err)
	}
	var list []any
	switch tr := v.Triggers.(type) {
	case []any:
		list = tr
	case map[string]any:
		list = []any{tr}
	}
	var out []*event.Trigger
	for _, e := range list {
		tr, err := event.ParseTrigger(e)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, tr)
	}
	return out
}

// sense runs one environment_sensor pass: every trigger whose filters pass fires.
func sense(t *testing.T, m *fakeMob, ctx *filter.Context) []string {
	var fired []string
	for _, tr := range componentTriggers(t, m, "minecraft:environment_sensor") {
		if tr.Filters.Eval(ctx) {
			fired = append(fired, tr.Event)
			event.Fire(m, tr.Event, ctx)
		}
	}
	return fired
}

func TestCowClimateAndAge(t *testing.T) {
	for _, c := range []struct {
		tag, want string
	}{
		{"spawns_warm_variant_farm_animals", "warm"},
		{"spawns_cold_variant_farm_animals", "cold"},
		{"", "temperate"},
	} {
		for _, id := range []string{"minecraft:cow", "minecraft:pig", "minecraft:chicken"} {
			w := plains()
			if c.tag != "" {
				w.tags[c.tag] = true
			}
			m := newFake(t, id)
			event.Fire(m, event.EntitySpawned, fctx(m, w, 3))
			if got := prop(t, m, "minecraft:climate_variant"); got != c.want {
				t.Errorf("%s in %q: climate %v, want %s", id, c.tag, got, c.want)
			}
		}
	}
	babies, sounds := 0, map[any]int{}
	for seed := uint64(0); seed < 2000; seed++ {
		m := newFake(t, "minecraft:cow")
		event.Fire(m, event.EntitySpawned, fctx(m, plains(), seed))
		adult, baby := m.inst.HasGroup("minecraft:cow_adult"), m.inst.HasGroup("minecraft:cow_baby")
		if adult == baby {
			t.Fatalf("seed %d: adult=%v baby=%v", seed, adult, baby)
		}
		if baby {
			babies++
			if !has(m, "minecraft:is_baby", "minecraft:ageable") || has(m, "minecraft:breedable") {
				t.Fatal("baby cow components")
			}
			if prop(t, m, "minecraft:sound_variant") != "default" {
				t.Fatal("baby path has no sound trigger")
			}
			// Grow up.
			event.Fire(m, event.AgeableGrowUp, fctx(m, plains(), seed))
			if has(m, "minecraft:is_baby") || !has(m, "minecraft:breedable", "minecraft:behavior.breed") {
				t.Fatal("grown up cow components")
			}
			continue
		}
		sounds[prop(t, m, "minecraft:sound_variant")]++
		if has(m, "minecraft:is_baby") || !has(m, "minecraft:breedable") {
			t.Fatal("adult cow components")
		}
	}
	if babies < 60 || babies > 145 {
		t.Fatalf("%d baby cows of 2000, want about 100 (5%%)", babies)
	}
	if sounds["default"] < 800 || sounds["moody"] < 800 {
		t.Fatalf("sound variants %v", sounds)
	}
}

func TestChickenEggs(t *testing.T) {
	m := newFake(t, "minecraft:chicken")
	event.Fire(m, "minecraft:spawn_adult", fctx(m, plains(), 1))
	c, ok := m.inst.Component("minecraft:spawn_entity")
	if !ok {
		t.Fatal("adult chicken has spawn_entity")
	}
	var se struct {
		Entities []struct {
			SpawnItem   string `json:"spawn_item"`
			MinWaitTime int    `json:"min_wait_time"`
			MaxWaitTime int    `json:"max_wait_time"`
			Filters     any    `json:"filters"`
		} `json:"entities"`
	}
	if err := json.Unmarshal(c.Raw(), &se); err != nil {
		t.Fatal(err)
	}
	egg := func() []string {
		var out []string
		for _, e := range se.Entities {
			f, err := filter.ParseValue(e.Filters)
			if err != nil {
				t.Fatal(err)
			}
			if f.Eval(fctx(m, plains(), 1)) {
				out = append(out, e.SpawnItem)
			}
		}
		return out
	}
	if got := egg(); len(got) != 1 || got[0] != "egg" || se.Entities[0].MinWaitTime != 300 || se.Entities[0].MaxWaitTime != 600 {
		t.Fatalf("temperate chicken lays %v", got)
	}
	event.Fire(m, event.HatchWarm, fctx(m, plains(), 1))
	if got := egg(); len(got) != 1 || got[0] != "brown_egg" {
		t.Fatalf("warm chicken lays %v", got)
	}
	_ = m.inst.SetPropertyNow("minecraft:climate_variant", "cold")
	if got := egg(); len(got) != 1 || got[0] != "blue_egg" {
		t.Fatalf("cold chicken lays %v", got)
	}
	m.nums[filter.NumberRiderCount] = 1
	if got := egg(); len(got) != 0 {
		t.Fatalf("ridden chicken lays %v", got)
	}
	baby := newFake(t, "minecraft:chicken")
	event.Fire(baby, event.EntityBorn, fctx(baby, plains(), 1))
	if baby.inst.HasComponent("minecraft:spawn_entity") {
		t.Fatal("baby chickens don't lay")
	}
}

type fakePlayer struct {
	held []filter.Item
}

func (p *fakePlayer) Position() mgl64.Vec3                    { return mgl64.Vec3{1, 0, 0} }
func (p *fakePlayer) HasFamily(f string) bool                 { return f == "player" }
func (p *fakePlayer) HasComponent(string) bool                { return false }
func (p *fakePlayer) Flag(filter.Flag) bool                   { return false }
func (p *fakePlayer) Number(filter.Number) (float64, bool)    { return 20, true }
func (p *fakePlayer) Equipment(d filter.Domain) []filter.Item { return p.held }

func interactFilter(t *testing.T, m *fakeMob) (*filter.Filter, string) {
	c, ok := m.inst.Component("minecraft:interact")
	if !ok {
		t.Fatal("no interact")
	}
	var v struct {
		Interactions json.RawMessage `json:"interactions"`
	}
	_ = json.Unmarshal(c.Raw(), &v)
	var list []struct {
		OnInteract struct {
			Filters any    `json:"filters"`
			Event   string `json:"event"`
		} `json:"on_interact"`
	}
	if err := json.Unmarshal(v.Interactions, &list); err != nil {
		var one struct {
			OnInteract struct {
				Filters any    `json:"filters"`
				Event   string `json:"event"`
			} `json:"on_interact"`
		}
		_ = json.Unmarshal(v.Interactions, &one)
		list = append(list, one)
	}
	f, err := filter.ParseValue(list[0].OnInteract.Filters)
	if err != nil {
		t.Fatal(err)
	}
	return f, list[0].OnInteract.Event
}

func TestSheepShearing(t *testing.T) {
	m := newFake(t, "minecraft:sheep")
	event.Fire(m, event.EntitySpawned, fctx(m, plains(), 9))
	if !m.inst.HasGroup("minecraft:sheep_adult") {
		event.Fire(m, "spawn_adult", fctx(m, plains(), 9))
		m.inst.RemoveGroup("minecraft:sheep_baby")
	}
	colours := 0
	for _, g := range m.inst.ActiveGroups() {
		if strings.HasPrefix(g, "minecraft:sheep_") && slices.Contains([]string{"white", "black", "gray", "light_gray", "brown", "pink"}, strings.TrimPrefix(g, "minecraft:sheep_")) {
			colours++
		}
	}
	if colours != 1 || !has(m, "minecraft:color") {
		t.Fatalf("sheep needs exactly one colour group: %v", m.inst.ActiveGroups())
	}
	player := &fakePlayer{held: []filter.Item{{Name: "minecraft:shears"}}}
	f, ev := interactFilter(t, m)
	ctx := fctx(m, plains(), 1)
	ctx.Other = player
	if !f.Eval(ctx) {
		t.Fatal("adult wooly sheep + player with shears must pass")
	}
	player.held = []filter.Item{{Name: "minecraft:wheat"}}
	if f.Eval(ctx) {
		t.Fatal("no shears")
	}
	player.held = []filter.Item{{Name: "minecraft:shears"}}
	event.Fire(m, ev, ctx)
	if !has(m, "minecraft:is_sheared") || has(m, "minecraft:is_dyeable") {
		t.Fatal("sheared sheep components")
	}
	if c, _ := m.inst.Component("minecraft:loot"); !strings.Contains(string(c.Raw()), "sheared") {
		t.Fatalf("sheared loot table: %s", c.Raw())
	}
	if f.Eval(ctx) {
		t.Fatal("a sheared sheep cannot be sheared again")
	}
	event.Fire(m, "minecraft:on_eat_block", ctx)
	if has(m, "minecraft:is_sheared") || !has(m, "minecraft:is_dyeable") {
		t.Fatal("eating grass regrows wool")
	}
	if c, _ := m.inst.Component("minecraft:loot"); strings.Contains(string(c.Raw()), "sheared") {
		t.Fatalf("wooly loot after regrowth: %s", c.Raw())
	}
	baby := newFake(t, "minecraft:sheep")
	event.Fire(baby, event.EntityBorn, fctx(baby, plains(), 1))
	bctx := fctx(baby, plains(), 1)
	bctx.Other = player
	if f.Eval(bctx) {
		t.Fatal("babies cannot be sheared")
	}
	event.Fire(baby, "minecraft:on_eat_block", bctx)
	if baby.inst.HasGroup("minecraft:loot_wooly") {
		t.Fatal("baby eating grass must not get the adult wooly loot")
	}
}

func mustRaw(m *fakeMob, n string) json.RawMessage {
	c, _ := m.inst.Component(n)
	if !c.Valid() {
		return nil
	}
	return c.Raw()
}

func TestZombieDrowned(t *testing.T) {
	for _, baby := range []bool{false, true} {
		m := newFake(t, "minecraft:zombie")
		start := "minecraft:as_adult"
		into := "minecraft:drowned<minecraft:as_adult>"
		if baby {
			start, into = "minecraft:as_baby", "minecraft:drowned<minecraft:as_baby>"
		}
		event.Fire(m, start, fctx(m, plains(), 1))
		ctx := fctx(m, plains(), 1)
		if fired := sense(t, m, ctx); len(fired) != 0 {
			t.Fatalf("dry zombie fired %v", fired)
		}
		m.flags[filter.FlagUnderwater] = true
		if fired := sense(t, m, ctx); len(fired) != 1 || fired[0] != "minecraft:start_transforming_into_drowned" {
			t.Fatalf("underwater zombie fired %v", fired)
		}
		timer := mustRaw(m, "minecraft:timer")
		if !strings.Contains(string(timer), `"time":30`) || !strings.Contains(string(timer), "minecraft:convert_to_drowned") {
			t.Fatalf("drowned timer %s", timer)
		}
		// Leaving the water stops the transformation.
		m.flags[filter.FlagUnderwater] = false
		if fired := sense(t, m, ctx); len(fired) != 1 || fired[0] != "minecraft:stop_transforming" {
			t.Fatalf("zombie out of water fired %v", fired)
		}
		if has(m, "minecraft:timer") {
			t.Fatal("stop_transforming removes the timer")
		}
		m.flags[filter.FlagUnderwater] = true
		sense(t, m, ctx)
		// The timer ran out.
		event.Fire(m, "minecraft:convert_to_drowned", ctx)
		tr := mustRaw(m, "minecraft:transformation")
		var v struct{ Into string }
		_ = json.Unmarshal(tr, &v)
		if v.Into != into {
			t.Fatalf("baby=%v transforms into %q, want %q", baby, v.Into, into)
		}
		id, ev := SplitEvent(v.Into)
		if id != "minecraft:drowned" || !strings.HasPrefix(ev, "minecraft:as_") {
			t.Fatal("SplitEvent")
		}
		// Removing start_drowned_transformation removes environment_sensor
		// and timer names entirely (the base sensor too), as in BDS.
		if has(m, "minecraft:timer") || has(m, "minecraft:environment_sensor") {
			t.Fatal("converting zombie keeps no sensor or timer")
		}
		if loadVanilla(t).Get(id) == nil {
			t.Fatal("drowned definition")
		}
	}
	// Spawn mix: 95% adult, 4.25% baby, 0.75% baby jockey; 10% can break doors.
	adults, doors := 0, 0
	for seed := uint64(0); seed < 4000; seed++ {
		m := newFake(t, "minecraft:zombie")
		event.Fire(m, event.EntitySpawned, fctx(m, plains(), seed))
		if m.inst.HasGroup("minecraft:zombie_adult") {
			adults++
		}
		if m.inst.HasGroup("minecraft:can_break_doors") {
			doors++
		}
	}
	if adults < 3700 || adults > 3900 || doors < 320 || doors > 480 {
		t.Fatalf("zombie spawns: %d adults, %d door breakers of 4000", adults, doors)
	}
}

func TestSkeleton(t *testing.T) {
	for _, c := range []struct {
		difficulty int
		group      string
	}{{3, "minecraft:ranged_attack_hard"}, {2, "minecraft:ranged_attack"}, {1, "minecraft:ranged_attack"}} {
		w := plains()
		w.difficulty = c.difficulty
		m := newFake(t, "minecraft:skeleton")
		event.Fire(m, event.EntitySpawned, fctx(m, w, 1))
		if got := m.inst.ActiveGroups(); len(got) != 1 || got[0] != c.group {
			t.Fatalf("difficulty %d: groups %v", c.difficulty, got)
		}
	}
	// Losing the bow switches to melee, which removes the ranged groups.
	m := newFake(t, "minecraft:skeleton")
	event.Fire(m, event.EntitySpawned, fctx(m, plains(), 1))
	ctx := fctx(m, plains(), 1)
	fired := sense(t, m, ctx)
	if !slices.Contains(fired, "minecraft:melee_mode") || !m.inst.HasGroup("minecraft:melee_attack") || m.inst.HasGroup("minecraft:ranged_attack") {
		t.Fatalf("no bow: fired %v groups %v", fired, m.inst.ActiveGroups())
	}
	m.flags[filter.FlagRangedWeapon] = true
	fired = sense(t, m, ctx)
	if !slices.Contains(fired, "minecraft:ranged_mode") || !m.inst.HasGroup("minecraft:ranged_attack") {
		t.Fatalf("bow again: fired %v groups %v", fired, m.inst.ActiveGroups())
	}
	// Lightning immunity uses the legacy other_with_families form.
	event.Fire(m, event.SpringTrap, ctx)
	c, _ := m.inst.Component("minecraft:damage_sensor")
	var ds struct {
		Triggers struct {
			OnDamage struct{ Filters any } `json:"on_damage"`
		}
	}
	_ = json.Unmarshal(c.Raw(), &ds)
	f, err := filter.ParseValue(ds.Triggers.OnDamage.Filters)
	if err != nil {
		t.Fatal(err)
	}
	ctx.Other = &fakePlayer{}
	if f.Eval(ctx) {
		t.Fatal("a player is not lightning")
	}
	ctx.Other = &lightning{}
	if !f.Eval(ctx) {
		t.Fatal("lightning damage is ignored")
	}
}

type lightning struct{ fakePlayer }

func (*lightning) HasFamily(f string) bool { return f == "lightning" }

func TestSpiderJockeys(t *testing.T) {
	jockeys := map[string]int{}
	for seed := uint64(0); seed < 3000; seed++ {
		w := plains()
		w.day = false
		w.tags["swamp"] = true
		m := newFake(t, "minecraft:spider")
		event.Fire(m, event.EntitySpawned, fctx(m, w, seed))
		for _, g := range m.inst.ActiveGroups() {
			if strings.Contains(g, "jockey") {
				jockeys[g]++
			}
		}
		if !m.inst.HasGroup("minecraft:spider_neutral") {
			t.Fatalf("seed %d: every spider is neutral at spawn: %v", seed, m.inst.ActiveGroups())
		}
	}
	// 1% jockeys: 80% of them biome specific (bogged in a swamp at night),
	// 20% default.
	if total := jockeys["minecraft:spider_bogged_jockey"] + jockeys["minecraft:spider_jockey"]; total < 12 || total > 50 || jockeys["minecraft:spider_stray_jockey"] != 0 {
		t.Fatalf("jockeys %v", jockeys)
	}
}

func TestCreeper(t *testing.T) {
	m := newFake(t, "minecraft:creeper")
	ctx := fctx(m, plains(), 1)
	f, ev := interactFilter(t, m)
	ctx.Other = &fakePlayer{held: []filter.Item{{Name: "minecraft:flint_and_steel"}}}
	if !f.Eval(ctx) {
		t.Fatal("flint and steel ignites")
	}
	event.Fire(m, ev, ctx)
	if !m.inst.HasGroup("minecraft:forced_exploding") || !has(m, "minecraft:explode") {
		t.Fatalf("forced exploding: %v", m.inst.ActiveGroups())
	}
	if f.Eval(ctx) {
		t.Fatal("already exploding")
	}
	c := newFake(t, "minecraft:creeper")
	event.Fire(c, "minecraft:become_charged", ctx.WithSelf(c))
	event.Fire(c, "minecraft:start_exploding", ctx.WithSelf(c))
	if !c.inst.HasGroup("minecraft:charged_exploding") || c.inst.HasGroup("minecraft:exploding") {
		t.Fatalf("charged creeper explodes charged: %v", c.inst.ActiveGroups())
	}
}

// TestPhase1EventsRun fires every event of the Phase 1 mobs in a full
// context and checks that every filter on the way uses only known tests.
func TestPhase1EventsRun(t *testing.T) {
	ids := []string{"minecraft:cow", "minecraft:pig", "minecraft:sheep", "minecraft:chicken",
		"minecraft:zombie", "minecraft:skeleton", "minecraft:spider", "minecraft:creeper"}
	for _, id := range ids {
		d := loadVanilla(t).Get(id)
		for _, ev := range d.Events() {
			d.Event(ev).WalkFilters(func(f *filter.Filter) {
				f.Walk(func(l *filter.Leaf) {
					if l.Def == nil {
						t.Errorf("%s %s: unknown filter test %s", id, ev, l.Name)
					}
				})
			})
			m := newFake(t, id)
			event.Fire(m, event.EntitySpawned, fctx(m, plains(), 1))
			ctx := fctx(m, plains(), 2)
			ctx.Other = &fakePlayer{}
			if !event.Fire(m, ev, ctx) {
				t.Errorf("%s: event %s did not run", id, ev)
			}
			m.inst.Commit()
		}
	}
}
