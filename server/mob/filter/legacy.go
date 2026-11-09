//go:build crossfly_full

package filter

import (
	"fmt"
)

// Legacy filter members (pre-1.2 entity files) such as
// {"other_with_families": "lightning"}. The table is
// ActorFilterGroup::initialize: member -> (test, subject, operator); an
// array value becomes an any_of (with_*) or all_of (without_*) of one test
// per element, as _processLegacyArray does.

type legacyDef struct {
	test    string
	subject Subject
	op      Operator
	anyOf   bool // array values are OR-ed (true) or AND-ed (false)
	value   any  // fixed value; nil = the member's value
}

var legacyDefs = map[string]legacyDef{}

var legacyKeywords = map[string]struct {
	test  string
	value bool
}{
	"underground": {"is_underground", true}, "surface": {"is_underground", false},
	"day": {"is_daytime", true}, "night": {"is_daytime", false},
	"biome:snow_covered": {"is_snow_covered", true}, "biome:humid": {"is_humid", true},
}

func addLegacy(prefix string, subject Subject) {
	add := func(name, test string, op Operator, anyOf bool) {
		legacyDefs[prefix+name] = legacyDef{test: test, subject: subject, op: op, anyOf: anyOf}
	}
	add("with_item", "has_equipment", OpEqual, true)
	add("without_item", "has_equipment", OpNotEqual, false)
	add("with_armor", "has_equipment", OpEqual, true)
	add("without_armor", "has_equipment", OpNotEqual, false)
	add("with_equipment", "has_equipment", OpEqual, true)
	add("without_equipment", "has_equipment", OpNotEqual, false)
	add("with_components", "has_component", OpEqual, false)
	add("without_components", "has_component", OpNotEqual, false)
	add("with_families", "is_family", OpEqual, true)
	add("without_families", "is_family", OpNotEqual, false)
	add("with_variant", "is_variant", OpEqual, true)
	add("without_variant", "is_variant", OpNotEqual, false)
	add("with_color", "is_color", OpEqual, true)
	add("without_color", "is_color", OpNotEqual, false)
}

func init() {
	addLegacy("", SubjectSelf)
	addLegacy("target_", SubjectTarget)
	addLegacy("other_", SubjectOther)
	legacyDefs["is_in_caravan"] = legacyDef{test: "in_caravan", op: OpEqual}
	legacyDefs["is_not_in_caravan"] = legacyDef{test: "in_caravan", op: OpNotEqual}
	legacyDefs["other_is_owner"] = legacyDef{test: "is_owner", subject: SubjectOther}
	legacyDefs["other_is_target"] = legacyDef{test: "is_target", subject: SubjectOther}
	legacyDefs["other_is_in_caravan"] = legacyDef{test: "in_caravan", subject: SubjectOther}
	legacyDefs["other_is_not_in_caravan"] = legacyDef{test: "in_caravan", subject: SubjectOther, op: OpNotEqual}
	legacyDefs["is_world_difficulty"] = legacyDef{test: "is_difficulty", anyOf: true}
	legacyDefs["is_not_world_difficulty"] = legacyDef{test: "is_difficulty", op: OpNotEqual}
	legacyDefs["is_game_rule_true"] = legacyDef{test: "is_game_rule"}
	legacyDefs["is_game_rule_false"] = legacyDef{test: "is_game_rule", op: OpNotEqual}
	legacyDefs["with_damage_cause"] = legacyDef{test: "has_damage"}
	legacyDefs["with_damage_fatal"] = legacyDef{test: "has_damage", value: "fatal"}
	legacyDefs["with_environment_any"] = legacyDef{test: "is_biome", anyOf: true}
	legacyDefs["with_environment_every"] = legacyDef{test: "is_biome"}
	legacyDefs["without_environment_any"] = legacyDef{test: "is_biome", op: OpNotEqual, anyOf: true}
	legacyDefs["without_environment_every"] = legacyDef{test: "is_biome", op: OpNotEqual}
	legacyDefs["player_with_instabuild"] = legacyDef{test: "has_ability", subject: SubjectPlayer, value: "instabuild"}
	legacyDefs["biome"] = legacyDef{test: "is_biome"}
}

// parseLegacy is ActorFilterGroup::_handleUnknownMember.
func (f *Filter) parseLegacy(name string, v any) error {
	switch name {
	// Environment keywords used as member names ({"day": true}).
	case "underground", "surface", "day", "night", "biome:snow_covered", "biome:humid":
		kw := legacyKeywords[name]
		val := kw.value
		if b, ok := v.(bool); ok && !b {
			val = !val
		}
		return f.addLegacyTest(kw.test, SubjectSelf, OpEqual, val)
	case "brightness_less", "brightness_greater", "altitude_less", "altitude_greater":
		test := "is_brightness"
		if name[0] == 'a' {
			test = "is_altitude"
		}
		op := OpLess
		if name[len(name)-7:] == "greater" {
			op = OpGreater
		}
		return f.addLegacyTest(test, SubjectSelf, op, v)
	}
	d, ok := legacyDefs[name]
	if !ok {
		noteUnknown("legacy:" + name)
		return fmt.Errorf("filter: invalid member %q", name)
	}
	if d.value != nil {
		return f.addLegacyTest(d.test, d.subject, d.op, d.value)
	}
	if arr, ok := v.([]any); ok {
		g := f
		kind := AllOf
		if d.anyOf {
			kind = AnyOf
		}
		if kind != f.Kind {
			g = &Filter{Kind: kind}
			f.Groups = append(f.Groups, g)
		}
		for _, e := range arr {
			if err := g.addLegacyTest(d.test, d.subject, d.op, e); err != nil {
				return err
			}
		}
		return nil
	}
	return f.addLegacyTest(d.test, d.subject, d.op, v)
}

func (f *Filter) addLegacyTest(test string, subject Subject, op Operator, value any) error {
	l, err := NewLeaf(test, float64(subject), float64(op), nil, value)
	if err != nil {
		return err
	}
	f.Tests = append(f.Tests, l)
	return nil
}
