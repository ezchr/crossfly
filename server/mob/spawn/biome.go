//go:build crossfly_full

package spawn

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync"

	"github.com/df-mc/dragonfly/server/world"
)

// Biome filters of spawn rules are evaluated per biome, once (BDS
// BiomeFilterGroup: the spawn rule is added to the spawn list of every biome
// its filter accepts at load). The tests the vanilla rules use are
// has_biome_tag (185 uses) and is_snow_covered (1, rabbit).
//
// Biome tags come from world.Biome.Tags(). Dragonfly's biome tags were
// checked against the 1.26.50 vanilla behaviour pack biome definitions
// (minecraft:tags of all 89 biomes): they are identical, so no extra table
// is needed. Custom biomes only need to return their Bedrock tags.

type biomeFilter interface {
	test(b biomeInfo) bool
}

type biomeInfo struct {
	tags  []string
	name  string
	snowy bool
}

func (b biomeInfo) has(tag string) bool { return slices.Contains(b.tags, tag) }

type biomeAll []biomeFilter
type biomeAny []biomeFilter
type biomeNone []biomeFilter
type biomeLeaf struct {
	test_ func(b biomeInfo) bool
}

type biomeConst bool

var biomeAlways biomeFilter = biomeConst(true)

func (c biomeConst) test(biomeInfo) bool  { return bool(c) }
func (l biomeLeaf) test(b biomeInfo) bool { return l.test_(b) }
func (a biomeAll) test(b biomeInfo) bool {
	for _, f := range a {
		if !f.test(b) {
			return false
		}
	}
	return true
}
func (a biomeAny) test(b biomeInfo) bool {
	for _, f := range a {
		if f.test(b) {
			return true
		}
	}
	return false
}
func (a biomeNone) test(b biomeInfo) bool {
	for _, f := range a {
		if f.test(b) {
			return false
		}
	}
	return true
}

// compileBiomeFilter parses a filter: an object with "test", an object with
// all_of/any_of/none_of, or an array (all_of).
func compileBiomeFilter(raw json.RawMessage) (biomeFilter, error) {
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) == nil {
		return compileGroup(list, func(fs []biomeFilter) biomeFilter { return biomeAll(fs) })
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if _, ok := m["test"]; ok {
		return compileTest(m)
	}
	var group biomeAll
	for key, mk := range map[string]func([]biomeFilter) biomeFilter{
		"all_of":  func(fs []biomeFilter) biomeFilter { return biomeAll(fs) },
		"any_of":  func(fs []biomeFilter) biomeFilter { return biomeAny(fs) },
		"none_of": func(fs []biomeFilter) biomeFilter { return biomeNone(fs) },
	} {
		v, ok := m[key]
		if !ok {
			continue
		}
		var items []json.RawMessage
		if json.Unmarshal(v, &items) != nil {
			items = []json.RawMessage{v}
		}
		f, err := compileGroup(items, mk)
		if err != nil {
			return nil, err
		}
		group = append(group, f)
	}
	if len(group) == 1 {
		return group[0], nil
	}
	return group, nil
}

func compileGroup(items []json.RawMessage, mk func([]biomeFilter) biomeFilter) (biomeFilter, error) {
	fs := make([]biomeFilter, 0, len(items))
	for _, it := range items {
		f, err := compileBiomeFilter(it)
		if err != nil {
			return nil, err
		}
		fs = append(fs, f)
	}
	return mk(fs), nil
}

func compileTest(m map[string]json.RawMessage) (biomeFilter, error) {
	test := str(m["test"])
	op := str(m["operator"])
	if op == "" {
		op = "=="
	}
	var negate bool
	switch op {
	case "==", "=", "equals":
	case "!=", "<>", "not":
		negate = true
	default:
		return nil, fmt.Errorf("operator %q not valid for %v", op, test)
	}
	apply := func(f func(b biomeInfo) bool) biomeFilter {
		if negate {
			return biomeLeaf{func(b biomeInfo) bool { return !f(b) }}
		}
		return biomeLeaf{f}
	}
	switch test {
	case "has_biome_tag":
		tag := str(m["value"])
		return apply(func(b biomeInfo) bool { return b.has(tag) }), nil
	case "is_biome":
		name := str(m["value"])
		return apply(func(b biomeInfo) bool { return b.name == name || "minecraft:"+b.name == name }), nil
	case "is_snow_covered":
		want := boolOr(m["value"], true)
		return apply(func(b biomeInfo) bool { return b.snowy == want }), nil
	}
	// Unknown tests evaluate false, as in BDS.
	return apply(func(biomeInfo) bool { return false }), nil
}

// biomeCandidates caches, per biome, the rules whose biome filter accepts it
// (Biome::getMobs in BDS), in rule order.
var biomeCandidates sync.Map // biomeKey -> []*Rule

type biomeKey struct {
	id   int
	name string
}

func rulesFor(b world.Biome) []*Rule {
	k := biomeKey{b.EncodeBiome(), b.String()}
	if v, ok := biomeCandidates.Load(k); ok {
		return v.([]*Rule)
	}
	info := biomeInfo{tags: b.Tags(), name: b.String(), snowy: b.Temperature() < 0.15}
	var out []*Rule
	for _, r := range rules().all {
		if !r.Experimental && r.biome.test(info) {
			out = append(out, r)
		}
	}
	v, _ := biomeCandidates.LoadOrStore(k, out)
	return v.([]*Rule)
}
