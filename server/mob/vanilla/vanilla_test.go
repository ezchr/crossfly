//go:build crossfly_full

package vanilla

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/data"
)

// TestLoadAllDefinitions parses all 129 vanilla definitions and reports the
// component keys no linked package supports yet, most used first. Run with
// -v to see the report; MOB_SUPPORT_REPORT=<file> also writes it to a file.
func TestLoadAllDefinitions(t *testing.T) {
	defs, err := Definitions()
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 129 {
		t.Fatalf("got %d definitions, want 129", len(defs))
	}
	ids := map[string]bool{}
	type usage struct {
		key      string
		entities []string
	}
	use := map[string]*usage{}
	mobs := 0
	for _, d := range defs {
		id := d.Identifier()
		if id == "" || ids[id] {
			t.Fatalf("empty or duplicate identifier %q", id)
		}
		ids[id] = true
		if IsMob(d) {
			mobs++
		}
		if inst := d.NewInstance(); inst == nil {
			t.Fatalf("%v: nil instance", id)
		} else {
			_ = inst.Components()
			_ = inst.Properties()
		}
		for _, k := range d.ComponentKeys() {
			u := use[k]
			if u == nil {
				u = &usage{key: k}
				use[k] = u
			}
			u.entities = append(u.entities, strings.TrimPrefix(id, "minecraft:"))
		}
	}
	var unsupported, supported []*usage
	for _, u := range use {
		if mob.Supported(u.key) {
			supported = append(supported, u)
		} else {
			unsupported = append(unsupported, u)
		}
	}
	byUse := func(a, b *usage) int {
		return cmp.Or(cmp.Compare(len(b.entities), len(a.entities)), cmp.Compare(a.key, b.key))
	}
	slices.SortFunc(unsupported, byUse)
	slices.SortFunc(supported, byUse)

	fullyCovered := 0
	for _, d := range defs {
		if !IsMob(d) {
			continue
		}
		ok := true
		for _, k := range d.ComponentKeys() {
			if !mob.Supported(k) {
				ok = false
				break
			}
		}
		if ok {
			fullyCovered++
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "definitions: %d (mobs with goals: %d, fully supported: %d)\n", len(defs), mobs, fullyCovered)
	fmt.Fprintf(&b, "component keys: %d (supported: %d, unsupported: %d)\n", len(use), len(supported), len(unsupported))
	b.WriteString("unsupported (entities using it):\n")
	for _, u := range unsupported {
		ex := u.entities
		more := ""
		if len(ex) > 6 {
			ex, more = ex[:6], ", ..."
		}
		fmt.Fprintf(&b, "  %-58s %3d  %s%s\n", u.key, len(u.entities), strings.Join(ex, ", "), more)
	}
	t.Log("\n" + b.String())
	if p := os.Getenv("MOB_SUPPORT_REPORT"); p != "" {
		if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestSpawnRulesEmbedded checks the spawn rules are embedded and parse.
func TestSpawnRulesEmbedded(t *testing.T) {
	n := 0
	for _, f := range data.SpawnRules() {
		var v map[string]json.RawMessage
		if err := json.Unmarshal(f.Data, &v); err != nil {
			t.Fatalf("%v: %v", f.Name, err)
		}
		if _, ok := v["minecraft:spawn_rules"]; !ok {
			t.Fatalf("%v: not a spawn rules file", f.Name)
		}
		n++
	}
	if n != 60 {
		t.Fatalf("got %d spawn rule files, want 60", n)
	}
}
