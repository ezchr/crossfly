//go:build crossfly_full

package item

import (
	"sync"

	"github.com/df-mc/dragonfly/server/world"
)

var registerJavaRedstone, registerMobItems, registerSpawnEggs sync.Once

// RegisterJavaRedstone registers the items of Crossfly Full's Java redstone: the minecarts. Call it before the
// server is configured, with block.RegisterJavaRedstone.
func RegisterJavaRedstone() {
	registerJavaRedstone.Do(func() {
		for _, k := range MinecartKinds() {
			world.RegisterItem(Minecart{Kind: k})
		}
	})
}

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
