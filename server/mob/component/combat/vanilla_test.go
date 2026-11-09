//go:build crossfly_full

package combat

import (
	"encoding/json"
	"testing"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/data"
)

// Every component here builds from every vanilla definition that uses it.
func TestVanillaComponentsBuild(t *testing.T) {
	n := 0
	for _, f := range data.Entities() {
		var v struct {
			Entity struct {
				Components json.RawMessage            `json:"components"`
				Groups     map[string]json.RawMessage `json:"component_groups"`
			} `json:"minecraft:entity"`
		}
		if err := json.Unmarshal(f.Data, &v); err != nil {
			t.Fatalf("%v: %v", f.Name, err)
		}
		sets := []json.RawMessage{v.Entity.Components}
		for _, g := range v.Entity.Groups {
			sets = append(sets, g)
		}
		for _, raw := range sets {
			if len(raw) == 0 {
				continue
			}
			cs, err := mob.ParseOrderedObject(raw)
			if err != nil {
				t.Fatalf("%v: %v", f.Name, err)
			}
			for _, c := range cs {
				fac := componentFactory(c.Key)
				if fac == nil {
					continue
				}
				if _, err := fac(c.Key, c.Raw); err != nil {
					t.Errorf("%v %v: %v", f.Name, c.Key, err)
				}
				n++
			}
		}
	}
	if n < 100 {
		t.Fatalf("only %d component uses built", n)
	}
}
