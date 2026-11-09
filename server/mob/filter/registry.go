//go:build crossfly_full

package filter

import (
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/go-gl/mathgl/mgl64"
)

// ValueKind is the type a test's "value" is parsed as.
type ValueKind uint8

// Value kinds.
const (
	KindBool ValueKind = iota
	KindInt
	KindFloat
	KindString
)

// Input is a test's parsed parameters.
type Input struct {
	Name    string
	Subject Subject
	Op      Operator

	// Domain is the "domain" field as a string ("hand", property name, game
	// rule, ...). DomainNum/DomainIsNum hold a numeric domain.
	Domain      string
	DomainNum   int
	DomainIsNum bool

	// The value, converted to the test's ValueKind.
	Bool  bool
	Int   int
	Float float64
	Str   string

	// HasValue reports whether "value" was present.
	HasValue bool
	// RawDomain and RawValue are the JSON values as decoded by encoding/json.
	RawDomain, RawValue any

	// Extra is set by a test's Setup (precomputed data).
	Extra any
}

func (in *Input) setValue(k ValueKind, v any) error {
	in.HasValue = v != nil
	switch k {
	case KindBool:
		in.Bool = true
		switch v := v.(type) {
		case bool:
			in.Bool = v
		case float64:
			in.Bool = v != 0
		case string:
			b, err := strconv.ParseBool(v)
			if err != nil {
				return fmt.Errorf("value %q is not a boolean", v)
			}
			in.Bool = b
		}
	case KindInt, KindFloat:
		switch v := v.(type) {
		case bool:
			if v {
				in.Float = 1
			}
		case float64:
			in.Float = v
		case string:
			f, err := strconv.ParseFloat(v, 64)
			if err != nil && k == KindInt {
				// Some int tests take preset strings (is_difficulty); Setup
				// handles those through RawValue.
				break
			}
			in.Float = f
		}
		in.Int = int(in.Float)
	case KindString:
		switch v := v.(type) {
		case string:
			in.Str = v
		case float64:
			in.Str = strconv.FormatFloat(v, 'f', -1, 64)
			in.Float, in.Int = v, int(v)
		case bool:
			in.Str = strconv.FormatBool(v)
		}
	}
	return nil
}

// CmpBool is FilterTest::_testValuesWithOperator(bool, bool) with the filter
// value as the right side.
func (in *Input) CmpBool(raw bool) bool { return cmpBool(in.Op, raw, in.Bool) }

// CmpTrue compares raw with true, which is what the string-valued tests
// (is_family, has_component, has_equipment, ...) do.
func (in *Input) CmpTrue(raw bool) bool { return cmpBool(in.Op, raw, true) }

// CmpInt is _testValuesWithOperator(int, int).
func (in *Input) CmpInt(raw int) bool { return cmpInt(in.Op, raw, in.Int) }

// CmpFloat is _testValuesWithOperator(float, float); BDS compares float32.
func (in *Input) CmpFloat(raw float64) bool { return cmpFloat(in.Op, float32(raw), float32(in.Float)) }

// CmpString compares strings with == or != (other operators are false).
func (in *Input) CmpString(raw string) bool {
	switch in.Op {
	case OpEqual:
		return raw == in.Str
	case OpNotEqual:
		return raw != in.Str
	}
	return false
}

func cmpBool(op Operator, a, b bool) bool {
	switch op {
	case OpEqual:
		return a == b
	case OpNotEqual:
		return a != b
	case OpGreater:
		return a && !b
	case OpLess:
		return b && !a
	case OpGreaterEqual:
		return a || !b
	case OpLessEqual:
		return b || !a
	}
	return false
}

func cmpInt(op Operator, a, b int) bool {
	switch op {
	case OpEqual:
		return a == b
	case OpNotEqual:
		return a != b
	case OpGreater:
		return a > b
	case OpLess:
		return a < b
	case OpGreaterEqual:
		return a >= b
	case OpLessEqual:
		return a <= b
	}
	return false
}

func cmpFloat(op Operator, a, b float32) bool {
	switch op {
	case OpEqual:
		return a == b
	case OpNotEqual:
		return a != b
	case OpGreater:
		return a > b
	case OpLess:
		return a < b
	case OpGreaterEqual:
		return a >= b
	case OpLessEqual:
		return a <= b
	}
	return false
}

// Scope is what a test's Eval sees: the resolved subject and position.
type Scope struct {
	Ctx *Context
	// Actor is the resolved subject (Self for subject "block").
	Actor Actor
	// Pos is the actor's position, or the block's for subject "block".
	Pos mgl64.Vec3
	// Block is set for subject "block".
	Block Block
}

// World returns the context world (may be nil).
func (s *Scope) World() World { return s.Ctx.World }

// Def describes a filter test.
type Def struct {
	Name string
	Kind ValueKind
	// Setup validates and precomputes the input at parse time. Optional.
	Setup func(in *Input) error
	// Eval evaluates the test for the resolved subject.
	Eval func(in *Input, s *Scope) bool
	// Source documents where the semantics come from ("bds 1.21.3",
	// "docs", "java").
	Source string
}

var (
	regMu    sync.RWMutex
	registry = map[string]*Def{}

	unknownMu   sync.Mutex
	unknownSeen = map[string]bool{}

	// Logger receives one warning per unknown test name.
	Logger = slog.Default()
)

// Register adds or replaces a test. Packages outside filter may register
// their own tests (custom tests of their own namespace) from init.
func Register(d *Def) {
	regMu.Lock()
	defer regMu.Unlock()
	registry[d.Name] = d
}

// Lookup returns the test with the name, or nil.
func Lookup(name string) *Def {
	regMu.RLock()
	defer regMu.RUnlock()
	return registry[name]
}

// Registered returns the names of all registered tests, sorted.
func Registered() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Unknown returns the unknown test names seen while parsing, sorted.
func Unknown() []string {
	unknownMu.Lock()
	defer unknownMu.Unlock()
	out := make([]string, 0, len(unknownSeen))
	for k := range unknownSeen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func noteUnknown(name string) {
	unknownMu.Lock()
	seen := unknownSeen[name]
	unknownSeen[name] = true
	unknownMu.Unlock()
	if !seen && Logger != nil {
		Logger.Warn("mob filter: unknown filter test, it evaluates false", "test", name)
	}
}

// ParseDomain maps an equipment domain name or number to a Domain.
func ParseDomain(in *Input) Domain {
	if in.DomainIsNum {
		if in.DomainNum >= 0 && in.DomainNum <= int(DomainBody) {
			return Domain(in.DomainNum)
		}
		return domainInvalid
	}
	return domainByName(in.Domain)
}

func domainByName(s string) Domain {
	s = strings.ToLower(strings.TrimPrefix(s, "slot."))
	switch s {
	case "", "any":
		return DomainAny
	case "hand", "weapon":
		return DomainHand
	case "armor":
		return DomainArmor
	case "head", "armor.head":
		return DomainHead
	case "torso", "chest", "armor.chest":
		return DomainTorso
	case "leg", "legs", "armor.legs":
		return DomainLeg
	case "feet", "armor.feet":
		return DomainFeet
	case "inventory":
		return DomainInventory
	case "body", "armor.body":
		return DomainBody
	case "main_hand", "mainhand", "weapon.mainhand":
		return DomainMainHand
	case "off_hand", "offhand", "weapon.offhand":
		return DomainOffHand
	}
	return domainInvalid
}

// itemsIn returns the subject's items in d.
func itemsIn(a Actor, d Domain) []Item {
	eq, ok := a.(Equipped)
	if !ok || d == domainInvalid {
		return nil
	}
	return eq.Equipment(d)
}

// related returns a's relation r or nil.
func related(a Actor, r Relation) Actor {
	if rel, ok := a.(Related); ok {
		return rel.Related(r)
	}
	return nil
}

func number(a Actor, n Number) (float64, bool) { return a.Number(n) }

// ns adds the minecraft: namespace to bare names.
func ns(s string) string {
	if s == "" || strings.Contains(s, ":") {
		return s
	}
	return "minecraft:" + s
}

// stripNS removes a minecraft: prefix.
func stripNS(s string) string { return strings.TrimPrefix(s, "minecraft:") }

func trunc(f float64) int { return int(math.Trunc(f)) }
