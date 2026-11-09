//go:build crossfly_full

package tame

import (
	"encoding/json"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/data"
)

func build[T any](t *testing.T, f mob.ComponentFactory, raw string) T {
	t.Helper()
	c, err := f("test", json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	v, ok := c.(T)
	if !ok {
		t.Fatalf("factory built %T", c)
	}
	return v
}

// BDS defaults: tameable probability 1; tamemount temper 0..100 starting at
// min_temper; a pufferfish bucket gives its water bucket back.
func TestTameDefaults(t *testing.T) {
	tm := build[*Tameable](t, newTameable, `{}`)
	if tm.prob != 1 || len(tm.items) != 0 {
		t.Fatalf("tameable defaults %+v", tm)
	}
	tm = build[*Tameable](t, newTameable, `{"probability":0.33,"tame_items":[{"item":"pufferfish_bucket","result_item":"water_bucket:0"},"pufferfish"]}`)
	if tm.prob != 0.33 || len(tm.items) != 2 || tm.items[0].result != "water_bucket:0" {
		t.Fatalf("nautilus tameable %+v", tm)
	}
	tm = build[*Tameable](t, newTameable, `{"tame_items":"bone"}`)
	if len(tm.items) != 1 || !tm.items[0].item.Matches(item.NewStack(item.Bone{}, 1)) {
		t.Fatalf("wolf tame item %+v", tm.items)
	}
	mt := build[*TameMount](t, newTameMount, `{}`)
	if mt.minTemper != 0 || mt.maxTemper != 100 || mt.temper != 0 {
		t.Fatalf("tamemount defaults %+v", mt)
	}
	mt = build[*TameMount](t, newTameMount, `{"min_temper":0,"max_temper":30,"feed_items":[{"item":"wheat","temper_mod":3},{"item":"hay_block","temper_mod":6}]}`)
	if mt.maxTemper != 30 || len(mt.feed) != 2 || mt.feed[1].temper != 6 {
		t.Fatalf("llama tamemount %+v", mt)
	}
	h := build[*Healable](t, newHealable, `{"filters":{"test":"is_riding","operator":"!=","value":true},"force_use":true,"items":[{"item":"cookie","heal_amount":0,"effects":[{"name":"fatal_poison","duration":1000,"amplifier":0,"chance":1.0}]}]}`)
	if !h.forceUse || len(h.items) != 1 || h.items[0].effects[0].duration != 20000 {
		t.Fatalf("parrot healable %+v", h)
	}
	g := build[*Giveable](t, newGiveable, `{"triggers":{"cooldown":3.0,"items":["bamboo","cake"],"on_give":{"event":"minecraft:on_calm","target":"self"}}}`)
	if g.cooldown != 60 || len(g.items) != 2 || g.onGive == nil {
		t.Fatalf("panda giveable %+v", g)
	}
}

// Every vanilla use of the taming components builds.
func TestVanillaComponentsBuild(t *testing.T) {
	factories := map[string]mob.ComponentFactory{
		"minecraft:tameable": newTameable, "minecraft:tamemount": newTameMount, "minecraft:sittable": newSittable,
		"minecraft:healable": newHealable, "minecraft:is_dyeable": newDyeable, "minecraft:giveable": newGiveable,
		"minecraft:trusting": newTrusting, "minecraft:trust": newTrust, "minecraft:on_wake_with_owner": newOnWake,
	}
	n := 0
	for _, f := range data.Entities() {
		var v struct {
			Entity struct {
				Components json.RawMessage            `json:"components"`
				Groups     map[string]json.RawMessage `json:"component_groups"`
			} `json:"minecraft:entity"`
		}
		if err := json.Unmarshal(f.Data, &v); err != nil {
			t.Fatal(err)
		}
		sets := []json.RawMessage{v.Entity.Components}
		for _, g := range v.Entity.Groups {
			sets = append(sets, g)
		}
		for _, raw := range sets {
			cs, err := mob.ParseOrderedObject(raw)
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range cs {
				if fac, ok := factories[c.Key]; ok {
					if _, err := fac(c.Key, c.Raw); err != nil {
						t.Errorf("%v %v: %v", f.Name, c.Key, err)
					}
					n++
				}
			}
		}
	}
	if n < 30 {
		t.Fatalf("only %d uses built", n)
	}
}
