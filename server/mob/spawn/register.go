//go:build crossfly_full

package spawn

import (
	"sync"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
)

func init() {
	mob.RegisterComponent("minecraft:despawn", newDespawn)
	mob.RegisterComponent("minecraft:persistent", newPersistent)
	mob.RegisterSystem(mob.System{Name: "spawn/inactivity", Stage: mob.StageDespawn, Tick: mobSystem})
}

var registerHook sync.Once

// RegisterHook registers the world tick hook natural spawning runs from (in the worlds that Enable it).
// vanilla.Register calls it: until then, nothing of this package runs in a world tick.
func RegisterHook() {
	registerHook.Do(func() {
		world.RegisterTickHook(worldTick)
		world.RegisterTickHook(insomniaTick)
	})
}
