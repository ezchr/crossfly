//go:build crossfly_full

// Package def loads Bedrock entity definitions (behaviour pack
// entities/*.json) and keeps the per-entity definition state BDS keeps in
// ActorDefinitionDiffList: the component group stack, the active component
// set derived from it, and actor property values. See README.md.
package def

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/df-mc/dragonfly/server/mob/event"
)

// Definition is a parsed entity definition. It is immutable after Parse and
// shared by all instances.
type Definition struct {
	FormatVersion string
	// ID is description.identifier, e.g. "minecraft:cow".
	ID string
	// RuntimeID is description.runtime_identifier (vanilla behaviour to copy).
	RuntimeID     string
	Spawnable     bool
	Summonable    bool
	Experimental  bool
	SpawnCategory string
	// Properties are description.properties in file order.
	Properties []*PropertyDef
	// Description keeps the raw description members not parsed above
	// (scripts, animations, aliases, ...).
	Description map[string]json.RawMessage

	base       *compGroup
	groups     map[string]*compGroup
	groupOrder []string
	events     map[string]*event.Response

	propIndex map[string]int
}

// Parse parses one entity file. Errors in single components, events or
// properties do not stop the parse: the returned definition is usable and the
// error lists what was skipped. A file without minecraft:entity.description
// returns ErrNotEntity.
func Parse(data []byte) (*Definition, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(stripComments(data), &top); err != nil {
		return nil, fmt.Errorf("def: %w", err)
	}
	var fv string
	_ = json.Unmarshal(top["format_version"], &fv)
	raw, ok := top["minecraft:entity"]
	if !ok {
		return nil, ErrNotEntity
	}
	var ent struct {
		Description     map[string]json.RawMessage `json:"description"`
		Components      json.RawMessage            `json:"components"`
		ComponentGroups json.RawMessage            `json:"component_groups"`
		Events          map[string]json.RawMessage `json:"events"`
	}
	if err := json.Unmarshal(raw, &ent); err != nil {
		return nil, fmt.Errorf("def: minecraft:entity: %w", err)
	}
	if ent.Description == nil {
		return nil, ErrNotEntity
	}
	d := &Definition{
		FormatVersion: fv,
		Description:   map[string]json.RawMessage{},
		groups:        map[string]*compGroup{},
		events:        map[string]*event.Response{},
		propIndex:     map[string]int{},
	}
	var errs []error
	for k, v := range ent.Description {
		var err error
		switch k {
		case "identifier":
			err = json.Unmarshal(v, &d.ID)
		case "runtime_identifier":
			err = json.Unmarshal(v, &d.RuntimeID)
		case "is_spawnable":
			err = json.Unmarshal(v, &d.Spawnable)
		case "is_summonable":
			err = json.Unmarshal(v, &d.Summonable)
		case "is_experimental":
			err = json.Unmarshal(v, &d.Experimental)
		case "spawn_category":
			err = json.Unmarshal(v, &d.SpawnCategory)
		case "properties":
			d.Properties, err = parseProperties(v)
		default:
			d.Description[k] = v
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("description.%s: %w", k, err))
		}
	}
	if d.ID == "" {
		return nil, fmt.Errorf("def: description.identifier is missing")
	}
	for _, p := range d.Properties {
		d.propIndex[p.Name] = p.Index
	}
	base, err := orderedObject(ent.Components)
	if err != nil {
		errs = append(errs, fmt.Errorf("components: %w", err))
	}
	d.base = newGroup("", base)
	groups, err := orderedObject(ent.ComponentGroups)
	if err != nil {
		errs = append(errs, fmt.Errorf("component_groups: %w", err))
	}
	for _, g := range groups {
		comps, err := orderedObject(g.raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("component group %s: %w", g.key, err))
			continue
		}
		if _, dup := d.groups[g.key]; !dup {
			d.groupOrder = append(d.groupOrder, g.key)
		}
		d.groups[g.key] = newGroup(g.key, comps)
	}
	for name, ev := range ent.Events {
		r, err := event.Parse(ev)
		if err != nil {
			errs = append(errs, fmt.Errorf("event %s: %w", name, err))
		}
		if r != nil {
			d.events[name] = r
		}
	}
	if len(errs) > 0 {
		return d, fmt.Errorf("def %s: %w", d.ID, errors.Join(errs...))
	}
	return d, nil
}

// ErrNotEntity is returned for JSON files that are not entity definitions.
var ErrNotEntity = errors.New("def: not an entity definition (no minecraft:entity.description)")

// Event returns the named event, or nil.
func (d *Definition) Event(name string) *event.Response { return d.events[name] }

// Events returns the event names, sorted.
func (d *Definition) Events() []string { return sortedKeys(d.events) }

// HasGroup reports whether the definition declares the component group.
func (d *Definition) HasGroup(name string) bool {
	_, ok := d.groups[name]
	return ok
}

// Groups returns the component group names in file order.
func (d *Definition) Groups() []string { return append([]string(nil), d.groupOrder...) }

// GroupComponents returns the components of a group in file order (nil if
// the group does not exist).
func (d *Definition) GroupComponents(group string) []Component {
	g, ok := d.groups[group]
	if !ok {
		return nil
	}
	return g.components()
}

// BaseComponents returns the "components" block in file order.
func (d *Definition) BaseComponents() []Component { return d.base.components() }

func (g *compGroup) components() []Component {
	out := make([]Component, len(g.order))
	for i, e := range g.order {
		out[i] = Component{entry: e}
	}
	return out
}

// ComponentNames returns every component name used in the base components
// and the groups, each once, in first-seen file order.
func (d *Definition) ComponentNames() []string {
	seen := map[string]bool{}
	var out []string
	add := func(g *compGroup) {
		for _, e := range g.order {
			if !seen[e.name] {
				seen[e.name] = true
				out = append(out, e.name)
			}
		}
	}
	add(d.base)
	for _, name := range d.groupOrder {
		add(d.groups[name])
	}
	return out
}

// Property returns the property definition, or nil.
func (d *Definition) Property(name string) *PropertyDef {
	i, ok := d.propIndex[name]
	if !ok {
		return nil
	}
	return d.Properties[i]
}

// SplitEvent splits "minecraft:drowned<minecraft:as_adult>" (the form used
// by transformation "into", spawn eggs and /summon) into the identifier and
// the spawn event. Without <...> the event is minecraft:entity_spawned.
func SplitEvent(s string) (id, ev string) {
	i := strings.IndexByte(s, '<')
	if i < 0 || !strings.HasSuffix(s, ">") {
		return s, event.EntitySpawned
	}
	return s[:i], s[i+1 : len(s)-1]
}

// Set is a collection of definitions keyed by identifier.
type Set struct {
	defs map[string]*Definition
}

// NewSet returns an empty set.
func NewSet() *Set { return &Set{defs: map[string]*Definition{}} }

// Add adds or replaces a definition. Later additions win, which gives pack
// stack order when packs are added bottom to top.
func (s *Set) Add(d *Definition) { s.defs[d.ID] = d }

// Get returns the definition for an identifier, or nil.
func (s *Set) Get(id string) *Definition { return s.defs[id] }

// IDs returns all identifiers, sorted.
func (s *Set) IDs() []string { return sortedKeys(s.defs) }

// Len returns the number of definitions.
func (s *Set) Len() int { return len(s.defs) }

// LoadFS parses every *.json file under dir (recursively) into a Set. Files
// that are not entity definitions are skipped silently; other parse errors
// are returned together, but definitions with partial errors are still added.
func LoadFS(fsys fs.FS, dir string) (*Set, error) {
	s := NewSet()
	var errs []error
	err := fs.WalkDir(fsys, dir, func(p string, de fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if de.IsDir() || !strings.HasSuffix(p, ".json") {
			return nil
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		d, err := Parse(data)
		if errors.Is(err, ErrNotEntity) {
			return nil
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path.Base(p), err))
		}
		if d != nil {
			s.Add(d)
		}
		return nil
	})
	if err != nil {
		errs = append(errs, err)
	}
	return s, errors.Join(errs...)
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
