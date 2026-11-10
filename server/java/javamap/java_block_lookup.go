//go:build crossfly_full

package javamap

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/world"
)

// JavaBlock is a Java 26.3 block as the Mojang reports describe it: the block Java's block state syntax
// (minecraft:repeater[facing=north,delay=3], as /setblock takes it) names.
type JavaBlock struct {
	// Name is the namespaced name, such as minecraft:repeater.
	Name string
	// Properties holds the values of each property, in the order of the report.
	Properties map[string][]string
	// Default is the id of the default state, which has the properties a state syntax leaves out.
	Default int32

	first, end int32
}

var javaBlockIndex = sync.OnceValue(func() map[string]*JavaBlock {
	states := blockDescriptions()
	m := make(map[string]*JavaBlock, len(javaDefaultStates))
	for id, s := range states {
		b := m[s.Name]
		if b == nil {
			b = &JavaBlock{Name: s.Name, Properties: map[string][]string{}, Default: -1, first: int32(id)}
			m[s.Name] = b
		}
		b.end = int32(id) + 1
		for k, v := range s.Properties {
			if !slices.Contains(b.Properties[k], v) {
				b.Properties[k] = append(b.Properties[k], v)
			}
		}
	}
	for _, d := range javaDefaultStates {
		if int(d) < len(states) {
			m[states[d].Name].Default = d
		}
	}
	return m
})

// JavaBlockByName returns the Java block of a name, with or without its minecraft: namespace.
func JavaBlockByName(name string) (*JavaBlock, bool) {
	if !strings.Contains(name, ":") {
		name = "minecraft:" + name
	}
	b, ok := javaBlockIndex()[name]
	return b, ok
}

// JavaStateProperties returns the block name and properties of a Java state id. The map is shared and must not be
// modified.
func JavaStateProperties(id int32) (name string, props map[string]string, ok bool) {
	states := blockDescriptions()
	if id < 0 || int(id) >= len(states) {
		return "", nil, false
	}
	return states[id].Name, states[id].Properties, true
}

// State returns the state of b with the properties passed and the others at their defaults. The errors name the
// unknown property or value, as Java Edition's do.
func (b *JavaBlock) State(props map[string]string) (int32, error) {
	states := blockDescriptions()
	want := maps.Clone(states[b.Default].Properties)
	for k, v := range props {
		values, ok := b.Properties[k]
		if !ok {
			return 0, fmt.Errorf("Block %s does not have property '%s' (properties: %s)", b.Name, k, strings.Join(sortedNames(b.Properties), ", "))
		}
		if !slices.Contains(values, v) {
			return 0, fmt.Errorf("Block %s does not accept '%s' for %s property (values: %s)", b.Name, v, k, strings.Join(values, ", "))
		}
		want[k] = v
	}
	for id := b.first; id < b.end; id++ {
		if equalProps(states[id].Properties, want) {
			return id, nil
		}
	}
	return 0, fmt.Errorf("Block %s has no state %v", b.Name, want)
}

func sortedNames(m map[string][]string) []string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	slices.Sort(names)
	return names
}

func equalProps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// javaToBedrock lists the runtime ids of the Bedrock states of world.DefaultBlockRegistry by the Java block they
// are shown to Java players as. States missing from the generated table (custom blocks) are left out.
var javaToBedrock = sync.OnceValue(func() map[string][]uint32 {
	world.DefaultBlockRegistry.Finalize()
	table, misses := BlockStatesWithMisses(world.DefaultBlockRegistry)
	missed := make(map[uint32]bool, len(misses))
	for _, rid := range misses {
		missed[rid] = true
	}
	states := blockDescriptions()
	m := map[string][]uint32{}
	for rid, java := range table {
		if missed[uint32(rid)] || int(java) >= len(states) {
			continue
		}
		name := states[java].Name
		m[name] = append(m[name], uint32(rid))
	}
	return m
})

// BedrockRuntimeID returns the runtime id, in world.DefaultBlockRegistry, of the Bedrock state that shows as the Java
// state java, or as the state closest to it: a Java property the Bedrock state does not keep (a stair's shape, a
// fence's connections) is worked out from the neighbours instead. The properties in named, which a player typed,
// count before the others. Among equal candidates the Bedrock block of the same name, an implemented block and the
// state closest to its block's default win. False if no Bedrock state shows as the Java block.
func BedrockRuntimeID(java int32, named []string) (uint32, bool) {
	states := blockDescriptions()
	if java < 0 || int(java) >= len(states) {
		return 0, false
	}
	target := states[java]
	cands := javaToBedrock()[target.Name]
	if len(cands) == 0 {
		return 0, false
	}
	table := DefaultBlockStates()
	reg := world.DefaultBlockRegistry
	bare := strings.TrimPrefix(target.Name, "minecraft:")
	type score struct{ props, sameName, implemented, nearDefault int }
	better := func(a, b score) bool {
		if a.props != b.props {
			return a.props > b.props
		}
		if a.sameName != b.sameName {
			return a.sameName > b.sameName
		}
		if a.implemented != b.implemented {
			return a.implemented > b.implemented
		}
		return a.nearDefault > b.nearDefault
	}
	best, bestScore := uint32(math.MaxUint32), score{props: -1}
	for _, rid := range cands {
		var s score
		got := states[table[rid]].Properties
		for k, v := range target.Properties {
			if got[k] != v {
				continue
			}
			if slices.Contains(named, k) {
				s.props += 1000
			} else {
				s.props++
			}
		}
		name, props, _ := reg.RuntimeIDToState(rid)
		if strings.TrimPrefix(name, "minecraft:") == bare {
			s.sameName = 1
		}
		if b, ok := reg.BlockByRuntimeID(rid); ok {
			if _, h := b.Hash(); h != math.MaxUint64 {
				s.implemented = 1
			}
		}
		if def, ok := reg.DefaultProperties(name); ok {
			for k, v := range def {
				if props[k] == v {
					s.nearDefault++
				}
			}
		}
		if better(s, bestScore) {
			best, bestScore = rid, s
		}
	}
	return best, true
}
