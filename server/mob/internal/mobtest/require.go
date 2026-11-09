//go:build crossfly_full

package mobtest

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/data"
	"github.com/df-mc/dragonfly/server/mob/debugcmd"
	"github.com/df-mc/dragonfly/server/world"
)

// owner names the package expected to implement a key, for skip messages.
func owner(key string) string {
	switch {
	case strings.HasPrefix(key, "minecraft:navigation."):
		return "mob/nav (B)"
	case strings.HasPrefix(key, "minecraft:movement."), strings.HasPrefix(key, "minecraft:jump."):
		return "mob/control (C)"
	case strings.HasPrefix(key, "minecraft:behavior."):
		return "mob/goal/* (D/E)"
	default:
		return "mob/component/* (D/E/F)"
	}
}

// Require skips the test unless every key has a registered factory in this
// test binary. Scenario tests use it so they stay green until the packages
// they exercise are linked.
func Require(t testing.TB, keys ...string) {
	t.Helper()
	var missing []string
	for _, k := range keys {
		if !mob.Supported(k) {
			missing = append(missing, fmt.Sprintf("%v [%v]", k, owner(k)))
		}
	}
	if len(missing) > 0 {
		t.Skipf("not implemented yet: %v", strings.Join(missing, ", "))
	}
}

// RequireDefinitions skips the test unless the definition package (A1,
// component groups and events) is linked: without it ParseDefinition falls
// back to a reader that knows base components only and never fires events.
func RequireDefinitions(t testing.TB) {
	t.Helper()
	if !DefinitionsLinked() {
		t.Skip("not implemented yet: component groups/events [mob/def (A1)]")
	}
}

// DefinitionsLinked reports whether a definition parser other than the
// Phase 0 fallback is installed.
func DefinitionsLinked() bool { return mob.DefinitionParserInstalled() }

// RequireFilters skips the test unless filters (A2) are compiled: without
// them every non-empty filter is false.
func RequireFilters(t testing.TB) {
	t.Helper()
	if !FiltersLinked() {
		t.Skip("not implemented yet: filters [mob/filter (A2)]")
	}
}

// FiltersLinked reports whether a filter compiler is installed: the Phase 0
// fallback evaluates every non-empty filter, even an empty all_of, false.
func FiltersLinked() bool {
	f, err := mob.CompileFilter(json.RawMessage(`{"all_of":[]}`))
	return err == nil && f != nil && f.Test(&mob.FilterContext{})
}

// Dump describes the mob behind r (goals, path, groups, components) for
// failure messages; "gone" when it left the world.
func (w *World) Dump(r *Ref) string {
	s := "gone"
	w.Do(func(tx *world.Tx) {
		if m, ok := r.Mob(tx); ok {
			s = debugcmd.Describe(m).String()
		} else if e, ok := r.Entity(tx); ok {
			s = fmt.Sprintf("%v at %v", e.H().Type().EncodeEntity(), e.Position())
		}
	})
	return s
}

// Component looks up a component's JSON in an embedded definition file
// ("creeper.json"): in "components" when group is empty, else in that
// component group.
func Component(t testing.TB, file, group, key string) json.RawMessage {
	t.Helper()
	raw, ok := data.Entity(file)
	if !ok {
		t.Fatalf("mobtest: no definition %v", file)
	}
	var f struct {
		Entity struct {
			Components      map[string]json.RawMessage            `json:"components"`
			ComponentGroups map[string]map[string]json.RawMessage `json:"component_groups"`
		} `json:"minecraft:entity"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("mobtest: %v: %v", file, err)
	}
	src := f.Entity.Components
	if group != "" {
		src = f.Entity.ComponentGroups[group]
	}
	v, ok := src[key]
	if !ok {
		t.Fatalf("mobtest: %v has no %v in %q", file, key, group)
	}
	return v
}

// Decode unmarshals raw into v, failing the test on error.
func Decode(t testing.TB, raw json.RawMessage, v any) {
	t.Helper()
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("mobtest: decode %s: %v", raw, err)
	}
}
