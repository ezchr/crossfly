//go:build crossfly_full

package event

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/mob/def/molang"
	"github.com/df-mc/dragonfly/server/mob/filter"
	"github.com/go-gl/mathgl/mgl64"
)

// fakeEntity is an entity whose "groups" are a set of names; group "g:X"
// gives component "X".
type fakeEntity struct {
	name    string
	events  map[string]*Response
	groups  map[string]bool
	log     []string
	props   map[string]molang.Value
	pending map[string]molang.Value
	actions []Action
}

func newEntity(t *testing.T, name string, events string) *fakeEntity {
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(events), &m); err != nil {
		t.Fatal(err)
	}
	e := &fakeEntity{name: name, events: map[string]*Response{}, groups: map[string]bool{},
		props: map[string]molang.Value{}, pending: map[string]molang.Value{}}
	for k, v := range m {
		r, err := Parse(v)
		if err != nil {
			t.Fatalf("event %s: %v", k, err)
		}
		e.events[k] = r
	}
	return e
}

func (e *fakeEntity) Position() mgl64.Vec3                 { return mgl64.Vec3{} }
func (e *fakeEntity) HasFamily(f string) bool              { return f == e.name }
func (e *fakeEntity) HasComponent(n string) bool           { return e.groups[n] }
func (e *fakeEntity) Flag(filter.Flag) bool                { return false }
func (e *fakeEntity) Number(filter.Number) (float64, bool) { return 0, false }
func (e *fakeEntity) PropertyValue(n string) (any, bool) {
	v, ok := e.props[n]
	if !ok {
		return nil, false
	}
	return v.Float(), true
}
func (e *fakeEntity) EventState() State { return e }
func (e *fakeEntity) NameTag() string   { return "" }

func (e *fakeEntity) Event(n string) *Response { return e.events[n] }
func (e *fakeEntity) AddGroup(n string) bool {
	e.groups[n] = true
	e.log = append(e.log, "+"+n)
	return true
}
func (e *fakeEntity) RemoveGroup(n string) bool {
	delete(e.groups, n)
	e.log = append(e.log, "-"+n)
	return true
}
func (e *fakeEntity) SetProperty(n string, v molang.Value) error {
	if n == "bad" {
		return fmt.Errorf("bad")
	}
	e.pending[n] = v
	return nil
}
func (e *fakeEntity) Commit() {
	for k, v := range e.pending {
		e.props[k] = v
		e.log = append(e.log, fmt.Sprintf("%s=%s", k, v))
	}
	e.pending = map[string]molang.Value{}
	e.log = append(e.log, "commit")
}
func (e *fakeEntity) HandleAction(a Action, _ *filter.Context) { e.actions = append(e.actions, a) }
func (e *fakeEntity) Query(name string, args []molang.Value) (molang.Value, bool) {
	if name == "property" && len(args) == 1 {
		return e.props[args[0].Str], true
	}
	return molang.Value{}, false
}

func ctx(seed uint64) *filter.Context {
	return &filter.Context{Rand: rand.New(rand.NewPCG(seed, seed)), World: filter.NopWorld{}}
}

func TestParseShapes(t *testing.T) {
	r, err := Parse(json.RawMessage(`{"randomize":[{"weight":3,"add":{"component_groups":["a"]}},{"remove":{"component_groups":["b"]},"trigger":"x"}],"sequence":[{"add":{"component_groups":["c"]}}],"add":{"component_groups":["ignored"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if r.Kind != Randomize || len(r.Children) != 2 || r.Children[0].Weight != 3 || r.Children[1].Weight != 1 || len(r.Add) != 0 {
		t.Fatalf("randomize parse: %+v", r)
	}
	if r.Children[1].Trigger.Event != "x" || r.Children[1].Trigger.Target != filter.SubjectSelf {
		t.Fatal("trigger parse")
	}
	r, _ = Parse(json.RawMessage(`{"trigger":{"event":"e","target":"other","filters":{"test":"is_family","value":"a"}},"set_property":{"p:a":"warm","p:b":true,"p:c":3,"p:d":"math.clamp(query.property('p:d') + 1, 0, 6)"},"queue_command":{"command":["say hi"]},"play_sound":{"sound":"plop"},"emit_vibration":{"vibration":"entity_act"},"reset_target":{},"weird_new_key":{"x":1}}`))
	if r.Kind != Leaf || r.Trigger.Target != filter.SubjectOther || r.Trigger.Filters == nil {
		t.Fatal("trigger object parse")
	}
	keys := []string{}
	for _, a := range r.Actions {
		keys = append(keys, a.Key())
	}
	if strings.Join(keys, ",") != "set_property,queue_command,play_sound,emit_vibration,reset_target,weird_new_key" {
		t.Fatalf("action order %v", keys)
	}
	sp := r.Actions[0].(SetProperty)
	if sp.Values[0].Literal.Str != "warm" || sp.Values[1].Literal.Num != 1 || sp.Values[2].Literal.Num != 3 || sp.Values[3].Expr == nil {
		t.Fatalf("set_property parse %+v", sp)
	}
	r, _ = Parse(json.RawMessage(`{"sequence":[]}`))
	if r.Kind != Invalid {
		t.Fatal("empty collection should be invalid")
	}
}

func TestSequenceFiltersSeeStateBeforeEvent(t *testing.T) {
	e := newEntity(t, "cow", `{
		"ev": {"sequence": [
			{"add": {"component_groups": ["x"]}},
			{"filters": {"test": "has_component", "value": "x"}, "add": {"component_groups": ["y"]}},
			{"filters": {"test": "has_component", "operator": "!=", "value": "x"}, "add": {"component_groups": ["z"]}}
		]}}`)
	Fire(e, "ev", ctx(1))
	if e.groups["y"] || !e.groups["z"] || !e.groups["x"] {
		t.Fatalf("groups %v: filters must be evaluated before any child runs", e.groups)
	}
}

func TestRemoveThenAddAndTriggersAfterCommit(t *testing.T) {
	e := newEntity(t, "cow", `{
		"ev": {"add": {"component_groups": ["a"]}, "remove": {"component_groups": ["a"]},
		       "set_property": {"p": 2},
		       "trigger": {"event": "after", "filters": {"test": "has_component", "value": "a"}}},
		"after": {"add": {"component_groups": ["after_ran"]}}}`)
	Fire(e, "ev", ctx(1))
	want := "-a,+a,p=2,commit,+after_ran,commit"
	if got := strings.Join(e.log, ","); got != want {
		t.Fatalf("log %s, want %s", got, want)
	}
}

func TestRandomizeExcludesFailingChildren(t *testing.T) {
	e := newEntity(t, "cow", `{"ev": {"randomize": [
		{"weight": 100000, "filters": {"test": "is_family", "value": "pig"}, "add": {"component_groups": ["never"]}},
		{"weight": 1, "add": {"component_groups": ["always"]}}]}}`)
	for i := uint64(0); i < 50; i++ {
		e.groups = map[string]bool{}
		Fire(e, "ev", ctx(i))
		if e.groups["never"] || !e.groups["always"] {
			t.Fatalf("seed %d: %v", i, e.groups)
		}
	}
}

func TestRandomizeWeights(t *testing.T) {
	e := newEntity(t, "cow", `{"ev": {"randomize": [
		{"weight": 95, "add": {"component_groups": ["adult"]}},
		{"weight": 5, "add": {"component_groups": ["baby"]}}]}}`)
	c := ctx(42)
	babies := 0
	for i := 0; i < 20000; i++ {
		e.groups = map[string]bool{}
		e.log = nil
		Fire(e, "ev", c)
		if e.groups["baby"] {
			babies++
		}
	}
	if babies < 800 || babies > 1200 {
		t.Fatalf("%d babies of 20000, want about 1000", babies)
	}
}

func TestFirstValid(t *testing.T) {
	e := newEntity(t, "cow", `{"ev": {"first_valid": [
		{"filters": {"test": "is_family", "value": "pig"}, "add": {"component_groups": ["pig"]}},
		{"filters": {"test": "is_family", "value": "cow"}, "add": {"component_groups": ["cow"]}},
		{"add": {"component_groups": ["fallback"]}}]}}`)
	Fire(e, "ev", ctx(1))
	if !e.groups["cow"] || e.groups["fallback"] || e.groups["pig"] {
		t.Fatalf("%v", e.groups)
	}
}

func TestRootFilter(t *testing.T) {
	e := newEntity(t, "cow", `{"ev": {"filters": {"test": "is_family", "value": "pig"}, "add": {"component_groups": ["a"]}}}`)
	if !Fire(e, "ev", ctx(1)) || e.groups["a"] {
		t.Fatal("root filter must gate the event, and Fire still reports the event ran")
	}
	if Fire(e, "missing", ctx(1)) {
		t.Fatal("missing event")
	}
}

func TestLoopDetection(t *testing.T) {
	e := newEntity(t, "cow", `{
		"a": {"add": {"component_groups": ["a"]}, "trigger": "a"},
		"b": {"sequence": [{"trigger": "c"}, {"trigger": "c"}]},
		"c": {"set_property": {"n": "query.property('n') + 1"}}}`)
	Fire(e, "a", ctx(1))
	if got := strings.Count(strings.Join(e.log, ","), "+a"); got != 1 {
		t.Fatalf("a ran %d times", got)
	}
	Fire(e, "b", ctx(1))
	// BDS keeps every (event, name tag) of the chain: the second trigger of
	// c in the same chain is treated as a loop.
	if e.props["n"].Float() != 1 {
		t.Fatalf("c ran %v times", e.props["n"].Float())
	}
	Fire(e, "c", ctx(1))
	if e.props["n"].Float() != 2 {
		t.Fatal("a new Fire is a new chain")
	}
}

func TestTriggerTargets(t *testing.T) {
	self := newEntity(t, "cow", `{
		"to_other": {"trigger": {"event": "hit", "target": "other"}},
		"to_baby": {"trigger": {"event": "born", "target": "baby"}},
		"to_parent": {"trigger": {"event": "hit", "target": "parent"}}}`)
	other := newEntity(t, "pig", `{
		"hit": {"filters": {"test": "is_family", "value": "pig"}, "add": {"component_groups": ["was_hit"]}},
		"born": {"filters": {"test": "is_family", "subject": "other", "value": "cow"}, "add": {"component_groups": ["has_parent"]}}}`)
	c := ctx(1)
	c.Other = other
	Fire(self, "to_other", c)
	if !other.groups["was_hit"] {
		t.Fatal("trigger target other: event must run on other with self = other")
	}
	c = ctx(1)
	c.Baby = other
	Fire(self, "to_baby", c)
	if !other.groups["has_parent"] {
		t.Fatal("trigger target baby: other must be the parent")
	}
	other.groups = map[string]bool{}
	c = ctx(1)
	c.Parent = other
	Fire(self, "to_parent", c)
	if other.groups["was_hit"] {
		t.Fatal("BDS 1.21.3 ignores trigger target parent")
	}
}

func TestSetPropertyAndActions(t *testing.T) {
	e := newEntity(t, "creaking", `{
		"tick": {"set_property": {"s": "math.clamp(query.property('s') + 1, 0, 6)"}},
		"bad": {"set_property": {"bad": 1}, "queue_command": {"command": "say x"}, "emit_vibration": {"vibration": "entity_act"}}}`)
	for i := 0; i < 8; i++ {
		Fire(e, "tick", ctx(1))
	}
	if e.props["s"].Float() != 6 {
		t.Fatalf("s = %v", e.props["s"])
	}
	Fire(e, "bad", ctx(1))
	if len(e.actions) != 2 || e.actions[0].(QueueCommand).Commands[0] != "say x" {
		t.Fatalf("actions %v", e.actions)
	}
}
