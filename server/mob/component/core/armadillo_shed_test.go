//go:build crossfly_full

package core_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	. "github.com/df-mc/dragonfly/server/mob/component/core/coretest"
	"github.com/df-mc/dragonfly/server/world"
)

// armadillo has the vanilla adult armadillo's spawn_entity (copied verbatim but for waits of 1 second instead
// of 300 to 600).
var armadillo = &Def{ID: "test:armadillo", Category: "creature",
	Base: []mob.RawComponent{
		C("minecraft:health", `{"value":12,"max":12}`),
		C("minecraft:spawn_entity", `{"entities":{"min_wait_time":1,"max_wait_time":1,"spawn_sound":"mob.armadillo.scute_drop","spawn_item":"armadillo_scute"}}`),
	},
}

func init() { Register(armadillo) }

// TestArmadilloShedsScute: an adult armadillo drops an armadillo scute every so often (spawn_entity).
func TestArmadilloShedsScute(t *testing.T) {
	run := World(t)
	run(func(tx *world.Tx) {
		floor(tx)
		m, _ := Spawn(t, tx, "test:armadillo", at, "")
		TickAll(tx, 25, m)
		n := 0
		for _, s := range Items(tx) {
			if _, ok := s.Item().(item.ArmadilloScute); ok {
				n += s.Count()
			}
		}
		if n != 1 {
			Fatalf(t, "%v armadillo scutes after 25 ticks, want 1", n)
		}
	})
}
