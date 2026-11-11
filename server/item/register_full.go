//go:build crossfly_full

package item

import (
	"sync"

	"github.com/df-mc/dragonfly/server/world"
)

var registerJavaRedstone, registerMobItems, registerSpawnEggs sync.Once

// mobItems holds the functions that register more items of Crossfly Full's mobs, appended to from the init
// functions of the files that define them.
var mobItems []func()

// RegisterRedstone registers the items of Crossfly Full's redstone and vehicles: the minecarts and boats. Call it before the server is
// configured, with block.RegisterRedstone.
func RegisterRedstone() {
	registerJavaRedstone.Do(func() {
		for _, k := range MinecartKinds() {
			world.RegisterItem(Minecart{Kind: k})
		}
		for _, w := range BoatWoods() {
			world.RegisterItem(Boat{Wood: w})
			world.RegisterItem(Boat{Wood: w, Chest: true})
		}
	})
}

// RegisterJavaRedstone is RegisterRedstone, by its older name.
func RegisterJavaRedstone() { RegisterRedstone() }

// RegisterMobItems registers the items of Crossfly Full's mobs: the saddle, horse armour and the seeds sniffers
// dig up. Call it before the server is configured.
func RegisterMobItems() {
	registerMobItems.Do(func() {
		world.RegisterItem(Saddle{})
		world.RegisterItem(TorchflowerSeeds{})
		world.RegisterItem(PitcherPod{})
		for _, t := range HorseArmourTiers() {
			world.RegisterItem(HorseArmour{Tier: t})
		}
		for _, f := range mobItems {
			f()
		}
	})
}

// RegisterSpawnEggs registers the spawn eggs of the mobs passed (entity identifiers) that have one, which
// spawn their mobs through spawner. Call it after the mobs are registered and before the server is
// configured, so that the creative inventory gets the eggs. It returns the eggs registered.
func RegisterSpawnEggs(mobs []string, spawner MobSpawner) []SpawnEgg {
	var eggs []SpawnEgg
	registerSpawnEggs.Do(func() {
		mobSpawner = spawner
		for _, id := range mobs {
			if egg, ok := SpawnEggMob(id); ok {
				world.RegisterItem(egg)
				eggs = append(eggs, egg)
			}
		}
	})
	return eggs
}
