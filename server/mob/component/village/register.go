//go:build crossfly_full

package village

import (
	"sync"

	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
)

func init() {
	mob.RegisterComponent("minecraft:dweller", newDweller)
	mob.RegisterComponent("minecraft:scheduler", newScheduler)
	mob.RegisterComponent("minecraft:economy_trade_table", newEconomy)
	mob.RegisterComponent("minecraft:trade_table", newEconomy)
	mob.RegisterComponent("minecraft:trade_resupply", newTradeResupply)
	mob.RegisterComponent("minecraft:home", newHome)
	mob.RegisterComponent("minecraft:hide", newHide)
	mob.RegisterComponent("minecraft:annotation.open_door", newOpenDoor)
	mob.RegisterComponent("minecraft:is_illager_captain", newMarker)
	mob.RegisterComponent("minecraft:can_join_raid", newMarker)
	mob.RegisterComponent("minecraft:raid_trigger", newMarker)
	mob.RegisterComponent("minecraft:insomnia", newInsomnia)
	mob.RegisterComponent("minecraft:managed_wandering_trader", newManagedTrader)
	mob.RegisterComponent("minecraft:grows_crop", newGrowsCrop)
	mob.RegisterComponent("minecraft:shareables", newShareables)
	mob.RegisterComponent("minecraft:admire_item", newAdmire)
	mob.RegisterComponent("minecraft:barter", newBarter)
	mob.RegisterComponent("minecraft:celebrate_hunt", newCelebrateHunt)

}

var enable sync.Once

// Enable registers the effects of villages and raids (Bad Omen, Hero of the Village, Raid Omen) and the world tick
// hook villages are managed from. vanilla.Register calls it: until then, nothing of this package runs in a world tick.
func Enable() {
	enable.Do(func() {
		if _, ok := effect.ByID(28); !ok {
			effect.Register(28, BadOmen)
		}
		if _, ok := effect.ByID(29); !ok {
			effect.Register(29, HeroOfTheVillage)
		}
		if _, ok := effect.ByID(36); !ok {
			effect.Register(36, RaidOmen)
		}
		world.RegisterTickHook(func(tx *world.Tx) { ManagerOf(tx).tick(tx) })
	})
}
