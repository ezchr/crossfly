//go:build crossfly_full

// Package filter parses and evaluates Bedrock entity filters (the "filters"
// objects of events, sensors, goals and interactions), following BDS's
// FilterGroup/ActorFilterGroup and the FilterTest classes.
//
// Parse once at load time, evaluate many times:
//
//	f, err := filter.Parse(raw)
//	ok := f.Eval(&filter.Context{Self: mob, Other: player, World: w})
//
// A nil *Filter evaluates to true (no filter).
package filter

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Subject selects which actor a test looks at. The values are BDS's
// Puv::Legacy::FilterSubject, so numeric "subject" fields in the JSON map
// directly.
type Subject uint8

// Subjects.
const (
	SubjectSelf    Subject = 0
	SubjectOther   Subject = 1
	SubjectPlayer  Subject = 2
	SubjectTarget  Subject = 3
	SubjectParent  Subject = 4
	SubjectBaby    Subject = 5
	SubjectBlock   Subject = 6
	SubjectDamager Subject = 7
)

var subjectNames = map[string]Subject{
	"self": SubjectSelf, "other": SubjectOther, "player": SubjectPlayer,
	"target": SubjectTarget, "parent": SubjectParent, "baby": SubjectBaby,
	"block": SubjectBlock, "damager": SubjectDamager,
}

// ParseSubject parses a subject name ("self", "other", ...) or BDS number.
func ParseSubject(v any) (Subject, error) {
	switch v := v.(type) {
	case nil:
		return SubjectSelf, nil
	case string:
		if s, ok := subjectNames[strings.ToLower(v)]; ok {
			return s, nil
		}
	case float64:
		// FilterGroup::_parseFilterInputs keeps 1,2,3,4,6,7 and maps
		// everything else to self.
		switch n := int(v); n {
		case 1, 2, 3, 4, 6, 7:
			return Subject(n), nil
		}
		return SubjectSelf, nil
	}
	return 0, fmt.Errorf("filter: invalid subject %v", v)
}

func (s Subject) String() string {
	for k, v := range subjectNames {
		if v == s {
			return k
		}
	}
	return strconv.Itoa(int(s))
}

// Operator compares the tested value with the filter value. The values are
// BDS's (FilterTest::_testValuesWithOperator).
type Operator uint8

// Operators.
const (
	OpEqual        Operator = 0
	OpNotEqual     Operator = 1
	OpGreater      Operator = 2
	OpLess         Operator = 3
	OpGreaterEqual Operator = 4
	OpLessEqual    Operator = 5
)

var operatorNames = map[string]Operator{
	"==": OpEqual, "=": OpEqual, "equals": OpEqual,
	"!=": OpNotEqual, "<>": OpNotEqual, "not": OpNotEqual,
	">": OpGreater, "<": OpLess, ">=": OpGreaterEqual, "<=": OpLessEqual,
}

// ParseOperator parses an operator string or BDS number.
func ParseOperator(v any) (Operator, error) {
	switch v := v.(type) {
	case nil:
		return OpEqual, nil
	case string:
		if o, ok := operatorNames[strings.ToLower(v)]; ok {
			return o, nil
		}
	case float64:
		if n := int(v); n >= 0 && n <= 5 {
			return Operator(n), nil
		}
		return OpEqual, nil
	}
	return 0, fmt.Errorf("filter: invalid operator %v", v)
}

func (o Operator) String() string {
	return [...]string{"==", "!=", ">", "<", ">=", "<="}[o]
}

// Kind is the collection type of a filter group.
type Kind uint8

// Kinds.
const (
	AllOf  Kind = 0
	AnyOf  Kind = 1
	NoneOf Kind = 2
)

// Filter is a filter group: nested groups plus leaf tests, combined by Kind.
type Filter struct {
	Kind   Kind
	Groups []*Filter
	Tests  []*Leaf
}

// Leaf is one parsed test.
type Leaf struct {
	Input
	// Def is nil for an unknown test; the leaf then evaluates false.
	Def *Def
}

// Empty reports whether the filter has no tests at all.
func (f *Filter) Empty() bool {
	return f == nil || len(f.Groups) == 0 && len(f.Tests) == 0
}

// Eval evaluates the filter. A nil filter is true.
//
// Semantics are FilterGroup::evaluate: all_of fails on the first failing child
// group or test, any_of passes on the first passing one (an empty any_of is
// false), none_of fails on the first passing one. Child groups are evaluated
// before the group's own tests. A test whose subject is missing from the
// context fails before its operator is applied.
func (f *Filter) Eval(ctx *Context) bool {
	if f == nil {
		return true
	}
	switch f.Kind {
	case AllOf:
		for _, g := range f.Groups {
			if !g.Eval(ctx) {
				return false
			}
		}
		for _, t := range f.Tests {
			if !t.eval(ctx) {
				return false
			}
		}
		return true
	case AnyOf:
		for _, g := range f.Groups {
			if g.Eval(ctx) {
				return true
			}
		}
		for _, t := range f.Tests {
			if t.eval(ctx) {
				return true
			}
		}
		return false
	default:
		for _, g := range f.Groups {
			if g.Eval(ctx) {
				return false
			}
		}
		for _, t := range f.Tests {
			if t.eval(ctx) {
				return false
			}
		}
		return true
	}
}

func (l *Leaf) eval(ctx *Context) bool {
	if l.Def == nil || ctx == nil || ctx.Self == nil {
		return false
	}
	s := Scope{Ctx: ctx, Actor: ctx.Subject(l.Subject)}
	if s.Actor == nil {
		return false
	}
	s.Pos = s.Actor.Position()
	if l.Subject == SubjectBlock {
		if ctx.Block == nil {
			return false
		}
		s.Block = ctx.Block
		s.Pos = ctx.Block.Position()
	}
	return l.Def.Eval(&l.Input, &s)
}

// Walk calls fn for every leaf in the filter tree.
func (f *Filter) Walk(fn func(l *Leaf)) {
	if f == nil {
		return
	}
	for _, g := range f.Groups {
		g.Walk(fn)
	}
	for _, t := range f.Tests {
		fn(t)
	}
}

// Parse parses a filter. raw may be an object (a test, or groups and legacy
// members), an array (implicit all_of) or null/empty (nil filter).
func Parse(raw json.RawMessage) (*Filter, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("filter: %w", err)
	}
	return ParseValue(v)
}

// ParseValue parses an already decoded JSON value (from encoding/json into any).
func ParseValue(v any) (*Filter, error) {
	if v == nil {
		return nil, nil
	}
	f := &Filter{Kind: AllOf}
	if err := f.parseObject(v); err != nil {
		return f, err
	}
	return f, nil
}

// MustParse parses a JSON string and panics on error. For tests and constants.
func MustParse(s string) *Filter {
	f, err := Parse(json.RawMessage(s))
	if err != nil {
		panic(err)
	}
	return f
}

// parseObject is FilterGroup::_parseObject: arrays merge their elements into
// this group, an object with "test" adds one test, any other object is a set
// of members.
func (f *Filter) parseObject(v any) error {
	switch v := v.(type) {
	case nil:
		return fmt.Errorf("filter: null filter")
	case []any:
		for _, e := range v {
			if err := f.parseObject(e); err != nil {
				return err
			}
		}
		return nil
	case map[string]any:
		if _, ok := v["test"]; ok {
			return f.parseTest(v)
		}
		// Members are processed in sorted order, like Json::Value::getMemberNames.
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if err := f.parseMember(k, v[k]); err != nil {
				return err
			}
		}
		return nil
	}
	// Non-object, non-array values are ignored (BDS returns success).
	return nil
}

func groupKind(name string) (Kind, bool) {
	switch name {
	case "all_of", "all", "AND":
		return AllOf, true
	case "any_of", "any", "OR":
		return AnyOf, true
	case "none_of", "NOT":
		return NoneOf, true
	}
	return 0, false
}

// parseMember is FilterGroup::_parseMember. A member of the same kind as this
// group merges into it (so none_of inside none_of flattens, as in BDS).
func (f *Filter) parseMember(name string, v any) error {
	if v == nil {
		return fmt.Errorf("filter: member %q is null", name)
	}
	k, ok := groupKind(name)
	if !ok {
		return f.parseLegacy(name, v)
	}
	if k == f.Kind {
		return f.parseObject(v)
	}
	sub := &Filter{Kind: k}
	if err := sub.parseObject(v); err != nil {
		return err
	}
	f.Groups = append(f.Groups, sub)
	return nil
}

func (f *Filter) parseTest(m map[string]any) error {
	name, _ := m["test"].(string)
	if name == "" {
		return fmt.Errorf("filter: test name must be a string")
	}
	l, err := NewLeaf(name, m["subject"], m["operator"], m["domain"], m["value"])
	if err != nil {
		return err
	}
	f.Tests = append(f.Tests, l)
	return nil
}

// NewLeaf builds a test from its JSON fields (each may be nil).
func NewLeaf(name string, subject, operator, domain, value any) (*Leaf, error) {
	l := &Leaf{}
	l.Name = name
	var err error
	if l.Subject, err = ParseSubject(subject); err != nil {
		return nil, fmt.Errorf("filter %s: %w", name, err)
	}
	if l.Op, err = ParseOperator(operator); err != nil {
		return nil, fmt.Errorf("filter %s: %w", name, err)
	}
	l.RawDomain, l.RawValue = domain, value
	switch d := domain.(type) {
	case string:
		l.Domain = d
	case float64:
		l.Domain = strconv.Itoa(int(d))
		l.DomainNum, l.DomainIsNum = int(d), true
	case bool:
		l.Domain = strconv.FormatBool(d)
	}
	def := Lookup(name)
	if def == nil {
		noteUnknown(name)
		return l, nil
	}
	l.Def = def
	if err := l.Input.setValue(def.Kind, value); err != nil {
		return nil, fmt.Errorf("filter %s: %w", name, err)
	}
	if def.Setup != nil {
		if err := def.Setup(&l.Input); err != nil {
			return nil, fmt.Errorf("filter %s: %w", name, err)
		}
	}
	return l, nil
}
