//go:build crossfly_full

package def

import (
	"fmt"
	"strings"
)

// NBT keys written by Save.
const (
	// NBTDefinitions is the group stack as BDS writes it: a list of strings
	// "+minecraft:cow", "+", "+minecraft:cow_adult", "-minecraft:cow_baby".
	NBTDefinitions = "definitions"
	// NBTProperties is a compound of property name -> value (TAG_Int,
	// TAG_Float, TAG_Byte for bools, TAG_String for enums).
	NBTProperties = "properties"
)

// Save returns the instance state for the entity's NBT compound. Queued
// property changes that were not committed are not saved.
func (i *Instance) Save() map[string]any {
	defs := make([]string, len(i.stack))
	for j, e := range i.stack {
		defs[j] = StackEntry{Add: e.add, Group: e.name}.String()
	}
	m := map[string]any{NBTDefinitions: defs}
	if len(i.props) > 0 {
		props := make(map[string]any, len(i.props))
		for _, p := range i.def.Properties {
			switch v := i.props[p.Index].(type) {
			case bool:
				// Dragonfly's NBT decoder reads TAG_Byte back as uint8.
				var n uint8
				if v {
					n = 1
				}
				props[p.Name] = n
			case int:
				props[p.Name] = int32(v)
			case float64:
				props[p.Name] = float32(v)
			default:
				props[p.Name] = v
			}
		}
		m[NBTProperties] = props
	}
	return m
}

// Load restores state written by Save into a fresh instance (NewInstance).
// Unknown groups and properties, and values of the wrong type, are skipped
// and reported in the returned error; the rest is applied. The next Commit
// reports the loaded components as added.
func (i *Instance) Load(m map[string]any) error {
	var errs []string
	if raw, ok := m[NBTDefinitions]; ok {
		var list []string
		switch l := raw.(type) {
		case []string:
			list = l
		case []any:
			for _, e := range l {
				if s, ok := e.(string); ok {
					list = append(list, s)
				}
			}
		}
		stack := []stackEntry{i.stack[0], i.stack[1]}
		for _, s := range list {
			if len(s) == 0 || (s[0] != '+' && s[0] != '-') {
				errs = append(errs, fmt.Sprintf("bad definition entry %q", s))
				continue
			}
			add, name := s[0] == '+', s[1:]
			if name == "" || name == i.def.ID {
				continue // description and base entries are always there
			}
			comps, ok := i.def.groups[name]
			if !ok {
				errs = append(errs, fmt.Sprintf("unknown component group %q", name))
				continue
			}
			stack = append(stack, stackEntry{add: add, name: name, serial: i.serial(), comps: comps})
		}
		i.stack = stack
		i.compact()
		i.dirty = true
	}
	loaded := map[string]bool{}
	if raw, ok := m[NBTProperties].(map[string]any); ok {
		for name, v := range raw {
			p := i.def.Property(name)
			if p == nil {
				errs = append(errs, fmt.Sprintf("unknown property %q", name))
				continue
			}
			v = normaliseValue(p, v)
			if !p.Valid(v) {
				errs = append(errs, fmt.Sprintf("property %q: invalid value %v", name, v))
				continue
			}
			i.setNow(p, v)
			loaded[name] = true
		}
	}
	// Expression defaults of properties that were not saved see the loaded
	// groups now.
	for _, p := range i.def.Properties {
		if p.DefaultExpr != nil && !loaded[p.Name] {
			if v, err := p.Convert(p.DefaultExpr.Eval(i)); err == nil {
				i.setNow(p, v)
			}
		}
	}
	i.committed = map[string]activeComp{}
	if len(errs) > 0 {
		return fmt.Errorf("def %s: load: %s", i.def.ID, strings.Join(errs, "; "))
	}
	return nil
}

// normaliseValue converts NBT and Go number types to the property's Go type
// (int, float64, bool, string).
func normaliseValue(p *PropertyDef, v any) any {
	var f float64
	switch n := v.(type) {
	case uint8:
		f = float64(n)
	case int8:
		f = float64(n)
	case int16:
		f = float64(n)
	case int32:
		f = float64(n)
	case int64:
		f = float64(n)
	case int:
		f = float64(n)
	case float32:
		f = float64(n)
	case float64:
		f = n
	default:
		return v
	}
	switch p.Type {
	case PropBool:
		return f != 0
	case PropInt:
		return int(f)
	case PropFloat:
		return float64(float32(f))
	}
	return v
}
