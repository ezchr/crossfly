//go:build crossfly_full

package core_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	. "github.com/df-mc/dragonfly/server/mob/component/core/coretest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// climateChicken has the vanilla adult chicken's spawn_entity (copied verbatim but for waits of 1 second instead
// of 300 to 600): each climate variant lays its own egg.
var climateChicken = &Def{ID: "test:climate_chicken", Category: "creature",
	Props: []mob.PropertyDecl{{Name: "minecraft:climate_variant", Type: "enum", Values: []string{"temperate", "warm", "cold"}, Default: "temperate"}},
	Base: []mob.RawComponent{
		C("minecraft:health", `{"value":4,"max":4}`),
		C("minecraft:spawn_entity", `{"entities":[
			{"min_wait_time":1,"max_wait_time":1,"spawn_sound":"plop","spawn_item":"egg","filters":[{"test":"rider_count","subject":"self","operator":"==","value":0},{"test":"enum_property","subject":"self","domain":"minecraft:climate_variant","value":"temperate"}]},
			{"min_wait_time":1,"max_wait_time":1,"spawn_sound":"plop","spawn_item":"brown_egg","filters":[{"test":"rider_count","subject":"self","operator":"==","value":0},{"test":"enum_property","subject":"self","domain":"minecraft:climate_variant","value":"warm"}]},
			{"min_wait_time":1,"max_wait_time":1,"spawn_sound":"plop","spawn_item":"blue_egg","filters":[{"test":"rider_count","subject":"self","operator":"==","value":0},{"test":"enum_property","subject":"self","domain":"minecraft:climate_variant","value":"cold"}]}]}`),
	},
}

func init() { Register(climateChicken) }

// TestClimateChickensLayTheirEggs: warm chickens lay brown eggs, cold chickens blue ones (chicken.json
// spawn_entity), and nothing else.
func TestClimateChickensLayTheirEggs(t *testing.T) {
	for i, v := range []item.EggVariant{item.EggTemperate, item.EggWarm, item.EggCold} {
		run := World(t)
		run(func(tx *world.Tx) {
			floor(tx)
			m, _ := Spawn(t, tx, "test:climate_chicken", at.Add(mgl64.Vec3{float64(i), 0, 0}), "")
			if err := m.SetProperty("minecraft:climate_variant", v.String()); err != nil {
				Fatalf(t, "set climate variant: %v", err)
			}
			TickAll(tx, 25, m)
			eggs := map[item.EggVariant]int{}
			for _, s := range Items(tx) {
				if e, ok := s.Item().(item.Egg); ok {
					eggs[e.Variant] += s.Count()
				}
			}
			if eggs[v] != 1 || len(eggs) != 1 {
				Fatalf(t, "%v chicken laid %v, want one %v egg", v, eggs, v)
			}
		})
	}
}
