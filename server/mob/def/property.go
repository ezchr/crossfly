//go:build crossfly_full

package def

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"

	"github.com/df-mc/dragonfly/server/mob/def/molang"
)

// PropertyType is the type of an actor property.
type PropertyType uint8

// Property types.
const (
	PropInt PropertyType = iota
	PropFloat
	PropBool
	PropEnum
)

func (t PropertyType) String() string {
	return [...]string{"int", "float", "bool", "enum"}[t]
}

// Limits from ActorPropertiesDescription (1.21.x).
const (
	maxProperties   = 32
	maxEnumValues   = 16
	maxEnumValueLen = 32
)

var (
	propNameRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]+:[a-zA-Z0-9_.-]+$`)
	enumValRe  = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`)
)

// PropertyDef is one entry of description.properties.
type PropertyDef struct {
	Name       string
	Type       PropertyType
	ClientSync bool
	// Min/Max bound int and float properties.
	Min, Max float64
	// Values are the enum values in declaration order.
	Values []string
	// Default is the literal default (int, float64, bool or string) used
	// when DefaultExpr is nil.
	Default any
	// RawDefault is "default" as written in the JSON (bool, float64 or
	// string, possibly Molang).
	RawDefault any
	// DefaultExpr is a Molang default, evaluated when an instance is created
	// (after its initial groups are known, see Instance.InitProperties).
	DefaultExpr *molang.Expr
	// Index is the position in Definition.Properties.
	Index int
}

func parseProperty(name string, v any) (*PropertyDef, error) {
	if !propNameRe.MatchString(name) {
		return nil, fmt.Errorf("property name %q must have valid characters and include a namespace", name)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("JSON node for property %q is missing", name)
	}
	p := &PropertyDef{Name: name}
	typ, _ := m["type"].(string)
	switch typ {
	case "int":
		p.Type = PropInt
	case "float":
		p.Type = PropFloat
	case "bool":
		p.Type = PropBool
	case "enum":
		p.Type = PropEnum
	default:
		return nil, fmt.Errorf("property %q: type must be int, float, bool or enum", name)
	}
	if cs, ok := m["client_sync"]; ok {
		b, ok := cs.(bool)
		if !ok {
			return nil, fmt.Errorf("property %q: client_sync value must be boolean", name)
		}
		p.ClientSync = b
	}
	switch p.Type {
	case PropInt, PropFloat:
		r, ok := m["range"].([]any)
		if !ok || len(r) != 2 {
			return nil, fmt.Errorf("property %q: 'range' array should have two elements", name)
		}
		lo, ok1 := r[0].(float64)
		hi, ok2 := r[1].(float64)
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("property %q: 'range' array should contain numbers", name)
		}
		if p.Type == PropInt && (lo != math.Trunc(lo) || hi != math.Trunc(hi)) {
			return nil, fmt.Errorf("property %q: int range must contain integers", name)
		}
		if hi < lo {
			return nil, fmt.Errorf("property %q: range max is less than range min", name)
		}
		p.Min, p.Max = lo, hi
	case PropBool:
		if _, ok := m["range"]; ok {
			return nil, fmt.Errorf("property %q: 'bool' type was specified but a range field was still provided", name)
		}
	case PropEnum:
		vals, ok := m["values"].([]any)
		if !ok {
			return nil, fmt.Errorf("property %q: missing a 'values' array", name)
		}
		if len(vals) < 2 || len(vals) > maxEnumValues {
			return nil, fmt.Errorf("property %q: string enum must have more than 1 and no more than %d elements", name, maxEnumValues)
		}
		seen := map[string]bool{}
		for _, e := range vals {
			s, ok := e.(string)
			if !ok {
				return nil, fmt.Errorf("property %q: enum values must be strings", name)
			}
			if len(s) > maxEnumValueLen {
				return nil, fmt.Errorf("property %q: value %q is too long", name, s)
			}
			if !enumValRe.MatchString(s) {
				return nil, fmt.Errorf("property %q: value %q must start with a letter and be alphanumeric or underscore", name, s)
			}
			if seen[s] {
				return nil, fmt.Errorf("property %q: duplicate value %q", name, s)
			}
			seen[s] = true
			p.Values = append(p.Values, s)
		}
	}
	d, ok := m["default"]
	if !ok {
		return nil, fmt.Errorf("property %q: missing a 'default' value", name)
	}
	p.RawDefault = d
	if err := p.parseDefault(d); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *PropertyDef) parseDefault(d any) error {
	switch d := d.(type) {
	case string:
		if p.Type == PropEnum && p.enumIndex(d) >= 0 {
			p.Default = d
			return nil
		}
		e, err := molang.Parse(d)
		if err != nil {
			if p.Type == PropEnum {
				return fmt.Errorf("property %q: default value not in 'values' array and is not a valid Molang expression", p.Name)
			}
			return fmt.Errorf("property %q: 'default' value does not match the type %s", p.Name, p.Type)
		}
		if e.Constant() {
			return p.parseDefault(literal(e.Eval(nil)))
		}
		p.DefaultExpr = e
		p.Default = p.zero()
		return nil
	case bool:
		if p.Type != PropBool {
			return fmt.Errorf("property %q: 'default' value does not match the type %s", p.Name, p.Type)
		}
		p.Default = d
	case float64:
		switch p.Type {
		case PropInt:
			// "default %d is not between %d and %d, clamping to nearest value"
			p.Default = int(math.Max(p.Min, math.Min(p.Max, math.Trunc(d))))
		case PropFloat:
			p.Default = float64(float32(math.Max(p.Min, math.Min(p.Max, d))))
		default:
			return fmt.Errorf("property %q: 'default' value does not match the type %s", p.Name, p.Type)
		}
	default:
		return fmt.Errorf("property %q: unsupported default %v", p.Name, d)
	}
	return nil
}

func literal(v molang.Value) any {
	if v.IsStr {
		return v.Str
	}
	return v.Num
}

func (p *PropertyDef) zero() any {
	switch p.Type {
	case PropInt:
		return int(p.Min)
	case PropFloat:
		return p.Min
	case PropBool:
		return false
	}
	return p.Values[0]
}

func (p *PropertyDef) enumIndex(s string) int {
	for i, v := range p.Values {
		if v == s {
			return i
		}
	}
	return -1
}

// Convert turns a Molang value into this property's Go value (int, float64
// rounded to float32 precision, bool or string), with the checks BDS's ActorPropertyUtils makes:
// numbers out of range and unknown enum values are errors.
func (p *PropertyDef) Convert(v molang.Value) (any, error) {
	switch p.Type {
	case PropBool:
		if v.IsStr {
			return nil, fmt.Errorf("property %s: expected a bool, got %q", p.Name, v.Str)
		}
		return v.Num != 0, nil
	case PropInt:
		if v.IsStr {
			return nil, fmt.Errorf("property %s: expected an int, got %q", p.Name, v.Str)
		}
		n := math.Trunc(v.Num)
		if n < p.Min || n > p.Max {
			return nil, fmt.Errorf("actor event is trying to set property %s to %d, which is out of range", p.Name, int64(n))
		}
		return int(n), nil
	case PropFloat:
		if v.IsStr {
			return nil, fmt.Errorf("property %s: expected a float, got %q", p.Name, v.Str)
		}
		if v.Num < p.Min || v.Num > p.Max {
			return nil, fmt.Errorf("actor event is trying to set property %s to %f, which is out of range", p.Name, v.Num)
		}
		return float64(float32(v.Num)), nil
	}
	if !v.IsStr || p.enumIndex(v.Str) < 0 {
		return nil, fmt.Errorf("actor event failed to set property %s (no enum entry found with that exact value %q)", p.Name, v.String())
	}
	return v.Str, nil
}

// Valid reports whether a stored Go value fits the property (NBT loading).
func (p *PropertyDef) Valid(v any) bool {
	switch p.Type {
	case PropInt:
		n, ok := v.(int)
		return ok && float64(n) >= p.Min && float64(n) <= p.Max
	case PropFloat:
		f, ok := v.(float64)
		return ok && f >= p.Min && f <= p.Max
	case PropBool:
		_, ok := v.(bool)
		return ok
	}
	s, ok := v.(string)
	return ok && p.enumIndex(s) >= 0
}

func parseProperties(raw json.RawMessage) ([]*PropertyDef, error) {
	members, err := orderedObject(raw)
	if err != nil {
		return nil, fmt.Errorf("description.properties must be an object: %w", err)
	}
	if len(members) > maxProperties {
		return nil, fmt.Errorf("too many properties on the definition (limit is %d)", maxProperties)
	}
	var out []*PropertyDef
	var errs []error
	for _, m := range members {
		var v any
		if err := json.Unmarshal(m.raw, &v); err != nil {
			errs = append(errs, fmt.Errorf("property %q: %w", m.key, err))
			continue
		}
		p, err := parseProperty(m.key, v)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		p.Index = len(out)
		out = append(out, p)
	}
	return out, joinErrs(errs)
}

func joinErrs(errs []error) error { return errors.Join(errs...) }
