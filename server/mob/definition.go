//go:build crossfly_full

package mob

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Definition is one parsed entity definition (a behaviour pack minecraft:entity
// file). Package mob/def (A1) implements it and installs its parser with
// SetDefinitionParser; until then ParseDefinition falls back to a minimal
// reader that knows the description and the base components only (no groups,
// no events).
type Definition interface {
	// Identifier is the entity identifier, e.g. "minecraft:zombie".
	Identifier() string
	// Description is the definition's description block.
	Description() Description
	// ComponentKeys lists every component key used anywhere in the
	// definition: base components and all component groups, each key once,
	// in first-seen order. It is used for support reports only.
	ComponentKeys() []string
	// NewInstance returns the live definition state of one new mob: base
	// components active, no groups added, every property at its default.
	NewInstance() Instance
}

// Instance is one mob's live definition: the base components plus the
// component groups added so far, and the mob's property values.
type Instance interface {
	// Components returns the active components: base components overridden
	// by added groups in the order they were added (a later group replaces an
	// earlier one's component of the same key). The order must be stable
	// between calls with an unchanged set (definition order), because goals
	// are added in this order and BDS breaks priority ties by it.
	Components() []RawComponent
	// Fire runs the event named on m: filters, sequence, randomize,
	// add/remove groups, set_property, trigger, and the other responses.
	// It reports whether the active component set changed; the Mob then
	// re-applies Components(). Group changes made by one Fire must only become
	// visible to Components() after Fire returns, as BDS applies definition
	// changes after the event.
	Fire(m *Mob, event string, ctx *FilterContext) (changed bool)
	// Property returns the current value of a property declared in
	// description.properties: bool, int, float64 or string (enum).
	Property(name string) (any, bool)
	// SetProperty sets a property, checking type and range.
	SetProperty(name string, v any) error
	// Properties returns every declared property with its current value, in
	// declaration order.
	Properties() []PropertyValue
	// EncodeNBT and DecodeNBT save and load the added groups and the
	// property values.
	EncodeNBT() map[string]any
	DecodeNBT(map[string]any)
}

// RawComponent is one active component: its JSON key (with namespace, e.g.
// "minecraft:behavior.float") and its JSON value.
type RawComponent struct {
	Key string
	Raw json.RawMessage
}

// Description is the description block of an entity definition.
type Description struct {
	Identifier        string
	RuntimeIdentifier string
	SpawnCategory     string
	Spawnable         bool
	Summonable        bool
	Experimental      bool
	// Properties are the declared description.properties, in file order.
	Properties []PropertyDecl
}

// PropertyDecl is one entry of description.properties.
type PropertyDecl struct {
	Name string
	// Type is "bool", "int", "float" or "enum".
	Type string
	// Values are the enum values (Type "enum").
	Values []string
	// Range is [min, max] for int and float properties.
	Range [2]float64
	// Default is the default value as written in the JSON: a bool, a float64
	// (int/float), a string (enum), or a Molang string.
	Default    any
	ClientSync bool
}

// PropertyValue is a property and its current value.
type PropertyValue struct {
	Decl  PropertyDecl
	Value any
}

// DefinitionParser parses one entity file.
type DefinitionParser func(raw []byte) (Definition, error)

var definitionParser DefinitionParser

// SetDefinitionParser installs the definition parser. Only package mob/def
// calls it, from its register.go. It panics when called twice.
func SetDefinitionParser(p DefinitionParser) {
	if definitionParser != nil {
		panic("mob: definition parser already set")
	}
	definitionParser = p
}

// DefinitionParserInstalled reports whether a definition parser (package
// mob/def) is installed; without it ParseDefinition uses the minimal reader.
func DefinitionParserInstalled() bool { return definitionParser != nil }

// Optional Instance methods, probed by type assertion:
type (
	// InstanceGroups lists the component groups currently added, bottom
	// to top (debug output: /mobdebug).
	InstanceGroups interface{ ActiveGroups() []string }
	// InstanceReinit reports components that stay active but must be
	// rebuilt although their JSON is unchanged: their group was added
	// again (or overridden by another group) since the last call. BDS
	// re-initialises such components (a re-added timer restarts). The list
	// is cleared by the call.
	InstanceReinit interface{ TakeReinit() []string }
)

// ParseDefinition parses one entity file with the installed parser, or with
// the minimal fallback reader when none is installed.
func ParseDefinition(raw []byte) (Definition, error) {
	if definitionParser != nil {
		return definitionParser(raw)
	}
	return parseRawDefinition(raw)
}

// rawDefinition is the fallback Definition: description and base components,
// no groups or events.
type rawDefinition struct {
	desc  Description
	base  []RawComponent
	keys  []string
	props []PropertyDecl
}

func (d *rawDefinition) Identifier() string       { return d.desc.Identifier }
func (d *rawDefinition) Description() Description { return d.desc }
func (d *rawDefinition) ComponentKeys() []string  { return d.keys }
func (d *rawDefinition) NewInstance() Instance {
	inst := &rawInstance{d: d, values: map[string]any{}}
	for _, p := range d.props {
		switch v := p.Default.(type) {
		case bool, float64:
			inst.values[p.Name] = v
		case string:
			if p.Type == "enum" {
				inst.values[p.Name] = v
			}
		}
	}
	return inst
}

type rawInstance struct {
	d      *rawDefinition
	values map[string]any
}

func (i *rawInstance) Components() []RawComponent             { return i.d.base }
func (i *rawInstance) Fire(*Mob, string, *FilterContext) bool { return false }
func (i *rawInstance) Property(name string) (any, bool)       { v, ok := i.values[name]; return v, ok }
func (i *rawInstance) EncodeNBT() map[string]any              { return nil }
func (i *rawInstance) DecodeNBT(map[string]any)               {}
func (i *rawInstance) SetProperty(name string, v any) error {
	for _, p := range i.d.props {
		if p.Name == name {
			i.values[name] = v
			return nil
		}
	}
	return fmt.Errorf("mob: %v has no property %q", i.d.desc.Identifier, name)
}
func (i *rawInstance) Properties() []PropertyValue {
	out := make([]PropertyValue, 0, len(i.d.props))
	for _, p := range i.d.props {
		out = append(out, PropertyValue{Decl: p, Value: i.values[p.Name]})
	}
	return out
}

// orderedObject decodes a JSON object keeping its key order.
type orderedObject []RawComponent

func (o *orderedObject) UnmarshalJSON(b []byte) error {
	dec := json.NewDecoder(strings.NewReader(string(b)))
	t, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := t.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("expected object")
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return err
		}
		*o = append(*o, RawComponent{Key: kt.(string), Raw: v})
	}
	return nil
}

// ParseOrderedObject decodes a JSON object into its members in file order. It
// is exported for packages that need JSON key order (definition order).
func ParseOrderedObject(raw []byte) ([]RawComponent, error) {
	var o orderedObject
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, err
	}
	return o, nil
}

func parseRawDefinition(raw []byte) (Definition, error) {
	var file struct {
		Entity *struct {
			Description struct {
				Identifier        string        `json:"identifier"`
				RuntimeIdentifier string        `json:"runtime_identifier"`
				SpawnCategory     string        `json:"spawn_category"`
				Spawnable         bool          `json:"is_spawnable"`
				Summonable        bool          `json:"is_summonable"`
				Experimental      bool          `json:"is_experimental"`
				Properties        orderedObject `json:"properties"`
			} `json:"description"`
			Components      orderedObject            `json:"components"`
			ComponentGroups map[string]orderedObject `json:"component_groups"`
		} `json:"minecraft:entity"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, err
	}
	if file.Entity == nil || file.Entity.Description.Identifier == "" {
		return nil, fmt.Errorf("mob: not an entity definition (no minecraft:entity.description.identifier)")
	}
	e := file.Entity
	d := &rawDefinition{
		desc: Description{
			Identifier: e.Description.Identifier, RuntimeIdentifier: e.Description.RuntimeIdentifier,
			SpawnCategory: e.Description.SpawnCategory, Spawnable: e.Description.Spawnable,
			Summonable: e.Description.Summonable, Experimental: e.Description.Experimental,
		},
		base: e.Components,
	}
	for _, p := range e.Description.Properties {
		var pj struct {
			Type       string    `json:"type"`
			Values     any       `json:"values"`
			Range      []float64 `json:"range"`
			Default    any       `json:"default"`
			ClientSync bool      `json:"client_sync"`
		}
		if err := json.Unmarshal(p.Raw, &pj); err != nil {
			return nil, fmt.Errorf("mob: property %v: %w", p.Key, err)
		}
		decl := PropertyDecl{Name: p.Key, Type: pj.Type, Default: pj.Default, ClientSync: pj.ClientSync}
		if vs, ok := pj.Values.([]any); ok {
			for _, v := range vs {
				if s, ok := v.(string); ok {
					decl.Values = append(decl.Values, s)
				}
			}
		}
		if len(pj.Range) == 2 {
			decl.Range = [2]float64{pj.Range[0], pj.Range[1]}
		}
		d.props = append(d.props, decl)
	}
	d.desc.Properties = d.props
	seen := map[string]bool{}
	add := func(cs []RawComponent) {
		for _, c := range cs {
			if !seen[c.Key] {
				seen[c.Key] = true
				d.keys = append(d.keys, c.Key)
			}
		}
	}
	add(e.Components)
	groups := make([]string, 0, len(e.ComponentGroups))
	for g := range e.ComponentGroups {
		groups = append(groups, g)
	}
	slices.Sort(groups)
	for _, g := range groups {
		add(e.ComponentGroups[g])
	}
	return d, nil
}
