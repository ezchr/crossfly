//go:build crossfly_full

package def

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Typed component decoding is not done here: component packages register
// factories with mob.RegisterComponent / mob.RegisterGoal and get the raw
// JSON of each active component from the mob. This package only keeps the
// raw JSON, in file order.

// compEntry is one component as written in the definition (base or group).
// It is shared by every Instance of the definition.
type compEntry struct {
	name  string
	group string // "" for the base components
	raw   json.RawMessage
}

// compGroup is the base components or one component group, in file order.
type compGroup struct {
	order  []*compEntry
	byName map[string]*compEntry
}

func (g *compGroup) has(name string) bool {
	if g == nil {
		return false
	}
	_, ok := g.byName[name]
	return ok
}

func newGroup(group string, members []member) *compGroup {
	g := &compGroup{byName: make(map[string]*compEntry, len(members))}
	for _, m := range members {
		e := &compEntry{name: m.key, group: group, raw: m.raw}
		if old, ok := g.byName[m.key]; ok {
			// Duplicate key: the last one wins, at the first one's place.
			*old = *e
			continue
		}
		g.byName[m.key] = e
		g.order = append(g.order, e)
	}
	return g
}

// Component is an active component of an instance.
type Component struct {
	entry *compEntry
}

// Name is the component name, e.g. "minecraft:behavior.float".
func (c Component) Name() string { return c.entry.name }

// Group is the component group that supplied it ("" for the base components).
func (c Component) Group() string { return c.entry.group }

// Raw is the component JSON.
func (c Component) Raw() json.RawMessage { return c.entry.raw }

// Valid reports whether c refers to a component.
func (c Component) Valid() bool { return c.entry != nil }

// member is one key/value of a JSON object.
type member struct {
	key string
	raw json.RawMessage
}

// orderedObject decodes a JSON object keeping key order.
func orderedObject(raw json.RawMessage) ([]member, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	t, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, nil
	}
	if d, ok := t.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("expected a JSON object")
	}
	var out []member
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, err
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		out = append(out, member{key: kt.(string), raw: v})
	}
	return out, nil
}
