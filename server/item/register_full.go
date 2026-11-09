//go:build crossfly_full

package item

import (
	"sync"

	"github.com/df-mc/dragonfly/server/world"
)

var registerJavaRedstone, registerMobItems sync.Once

// RegisterJavaRedstone registers the items of Crossfly Full's Java redstone: the minecarts. Call it before the
// server is configured, with block.RegisterJavaRedstone.
func RegisterJavaRedstone() {
	registerJavaRedstone.Do(func() {
		for _, k := range MinecartKinds() {
			world.RegisterItem(Minecart{Kind: k})
		}
	})
}

// RegisterMobItems registers the items of Crossfly Full's mobs: the saddle and horse armour. Call it before the
// server is configured.
func RegisterMobItems() {
	registerMobItems.Do(func() {
		world.RegisterItem(Saddle{})
		for _, t := range HorseArmourTiers() {
			world.RegisterItem(HorseArmour{Tier: t})
		}
	})
}
