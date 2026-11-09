//go:build crossfly_full

package combat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/data"
)

// Every goal registered here builds from every vanilla definition that
// uses it (base components and all component groups).
func TestVanillaGoalsBuild(t *testing.T) {
	keys := map[string]bool{}
	for _, k := range mob.RegisteredGoals() {
		keys[k] = true
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
				if !keys[c.Key] || strings.HasPrefix(c.Key, "test:") {
					continue
				}
				spec, err := mob.ParseGoalSpec(c.Key, c.Raw)
				if err == nil {
					_, err = goalFactory(c.Key)(spec)
				}
				if err != nil {
					t.Errorf("%v %v: %v", f.Name, c.Key, err)
				}
				n++
			}
		}
	}
	if n < 100 {
		t.Fatalf("only %d goal uses built", n)
	}
}

func goalFactory(key string) mob.GoalFactory {
	return map[string]mob.GoalFactory{
		"minecraft:behavior.nearest_attackable_target": newNearestAttackableTarget,
		"minecraft:behavior.hurt_by_target":            newHurtByTarget,
		"minecraft:behavior.melee_attack":              newMeleeAttack,
		"minecraft:behavior.melee_box_attack":          newMeleeBoxAttack,
		"minecraft:behavior.ranged_attack":             newRangedAttack,
		"minecraft:behavior.swell":                     newSwell,
		"minecraft:behavior.leap_at_target":            newLeapAtTarget,
		"minecraft:behavior.flee_sun":                  newFleeSun,
		"minecraft:behavior.avoid_mob_type":            newAvoidMobType,
		"minecraft:behavior.pickup_items":              newPickupItems,
		"minecraft:behavior.equip_item":                newEquipItem,
		"minecraft:behavior.use_kinetic_weapon":        newUseKineticWeapon,
		// variants_misc.go
		"minecraft:behavior.nearest_prioritized_attackable_target": newNearestPrioritized,
		"minecraft:behavior.delayed_attack":                        newDelayedAttack,
		"minecraft:behavior.stomp_attack":                          newStompAttack,
	}[key]
}
