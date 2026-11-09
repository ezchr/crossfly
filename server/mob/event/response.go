//go:build crossfly_full

// Package event parses and runs entity event responses (the "events" block
// of an entity definition) with the semantics of BDS's
// ActorDefinitionEventLoader, ActorDefinitionEvent::_evaluateGroups and
// ActorDefinitionDescriptor::_executeEvent.
package event

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/df-mc/dragonfly/server/mob/def/molang"
	"github.com/df-mc/dragonfly/server/mob/filter"
)

// Kind is the type of a response node.
type Kind uint8

// Kinds. Leaf, Sequence and Randomize are BDS's types 0, 1 and 2; FirstValid
// is newer than the 1.21.3 dump; Invalid (BDS type 3) nodes do nothing and are
// dropped from collections.
const (
	Leaf Kind = iota
	Sequence
	Randomize
	FirstValid
	Invalid
)

func (k Kind) String() string {
	return [...]string{"leaf", "sequence", "randomize", "first_valid", "invalid"}[k]
}

// Response is one node of an event.
type Response struct {
	// Weight is the randomize weight (default 1).
	Weight float64
	// Filters gate this node. For a child of sequence/randomize/first_valid
	// the parent evaluates them; for the root, Execute does.
	Filters *filter.Filter
	Kind    Kind
	// Children of a Sequence, Randomize or FirstValid node.
	Children []*Response

	// Leaf data.
	Add, Remove []string
	Trigger     *Trigger
	Actions     []Action
}

// Trigger runs another event.
type Trigger struct {
	Event string
	// Target is the subject the event runs on: self (default), other,
	// target, baby or block. BDS 1.21.3 ignores player/parent/damager.
	Target  filter.Subject
	Filters *filter.Filter
}

// Parse parses one event (the value under "events": {"name": ...}).
func Parse(raw json.RawMessage) (*Response, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("event: %w", err)
	}
	return ParseValue(v)
}

// ParseValue parses an already decoded event.
func ParseValue(v any) (*Response, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return &Response{Kind: Invalid, Weight: 1}, fmt.Errorf("event: not a JSON object")
	}
	r := &Response{Weight: 1}
	var errs []error
	if w, ok := m["weight"]; ok {
		switch w := w.(type) {
		case float64:
			r.Weight = w
		default:
			errs = append(errs, fmt.Errorf("event: weight must be a number"))
		}
	}
	if f, ok := m["filters"]; ok {
		flt, err := filter.ParseValue(f)
		if err != nil {
			errs = append(errs, err)
		}
		r.Filters = flt
	}
	collection := func(key string, k Kind) {
		arr, _ := m[key].([]any)
		for _, e := range arr {
			if _, ok := e.(map[string]any); !ok {
				continue
			}
			c, err := ParseValue(e)
			if err != nil {
				errs = append(errs, err)
			}
			if c.Kind != Invalid {
				r.Children = append(r.Children, c)
			}
		}
		r.Kind = Invalid
		if len(r.Children) > 0 {
			r.Kind = k
		}
	}
	_, isRand := m["randomize"]
	_, isSeq := m["sequence"]
	_, isFirst := m["first_valid"]
	switch {
	case isRand:
		// "Item event has both randomize and sequence tags. Sequence will be
		// ignored." BDS also ignores add/remove/trigger next to a collection.
		collection("randomize", Randomize)
	case isSeq:
		collection("sequence", Sequence)
	case isFirst:
		collection("first_valid", FirstValid)
	default:
		if err := r.parseLeaf(m); err != nil {
			errs = append(errs, err)
		}
	}
	return r, joinErrs(errs)
}

func groupList(v any) []string {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	arr, _ := m["component_groups"].([]any)
	var out []string
	for _, g := range arr {
		if s, ok := g.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func (r *Response) parseLeaf(m map[string]any) error {
	var errs []error
	r.Add = groupList(m["add"])
	r.Remove = groupList(m["remove"])
	if t, ok := m["trigger"]; ok && t != nil {
		tr, err := parseTrigger(t)
		if err != nil {
			errs = append(errs, err)
		}
		r.Trigger = tr
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		switch k {
		case "add", "remove", "trigger", "filters", "weight":
			continue
		}
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		oi, oj := actionOrder(keys[i]), actionOrder(keys[j])
		if oi != oj {
			return oi < oj
		}
		return keys[i] < keys[j]
	})
	for _, k := range keys {
		a, err := parseAction(k, m[k])
		if err != nil {
			errs = append(errs, err)
			continue
		}
		r.Actions = append(r.Actions, a)
	}
	return joinErrs(errs)
}

// ParseTrigger parses a trigger: "event_name" or {"event", "target", "filters"}.
// Components that fire events (environment_sensor, timer, interact, ...)
// use the same shape.
func ParseTrigger(v any) (*Trigger, error) { return parseTrigger(v) }

func parseTrigger(v any) (*Trigger, error) {
	t := &Trigger{Target: filter.SubjectSelf}
	switch v := v.(type) {
	case string:
		t.Event = v
		return t, nil
	case map[string]any:
		t.Event, _ = v["event"].(string)
		if tg, ok := v["target"]; ok {
			s, err := filter.ParseSubject(tg)
			if err != nil {
				return t, fmt.Errorf("event: trigger: %w", err)
			}
			t.Target = s
		}
		if f, ok := v["filters"]; ok {
			flt, err := filter.ParseValue(f)
			t.Filters = flt
			if err != nil {
				return t, err
			}
		}
		return t, nil
	}
	return nil, fmt.Errorf("event: invalid trigger %v", v)
}

// Walk calls fn for r and every descendant.
func (r *Response) Walk(fn func(n *Response)) {
	if r == nil {
		return
	}
	fn(r)
	for _, c := range r.Children {
		c.Walk(fn)
	}
}

// WalkFilters calls fn for every filter in the tree (node and trigger filters).
func (r *Response) WalkFilters(fn func(f *filter.Filter)) {
	r.Walk(func(n *Response) {
		if n.Filters != nil {
			fn(n.Filters)
		}
		if n.Trigger != nil && n.Trigger.Filters != nil {
			fn(n.Trigger.Filters)
		}
	})
}

// PropertyValue is one entry of set_property.
type PropertyValue struct {
	Name string
	// Literal is set for JSON bools/numbers and strings that are not Molang
	// (enum values such as "warm").
	Literal molang.Value
	// Expr is set for Molang strings.
	Expr *molang.Expr
}

// Value evaluates the entry.
func (p PropertyValue) Value(env molang.Env) molang.Value {
	if p.Expr != nil {
		return p.Expr.Eval(env)
	}
	return p.Literal
}

func joinErrs(errs []error) error { return errors.Join(errs...) }
