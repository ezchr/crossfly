//go:build crossfly_full

package loot

import (
	"sync"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// Trident is the item vanilla spawn equipment hands out that Dragonfly has
// no item for (drowned_equipment.json, drowned_ranged_equipment.json; the
// fishing rod is item.FishingRod). It is registered here, unless something
// registered the name first, so drowned can hold, show and drop it. It holds
// and hits like the vanilla item (melee 9, durable and enchantable) but has
// no use action: throwing tridents is not implemented.
type Trident struct{}

var enableItems sync.Once

// Enable registers the item and the enchantment mobs drop and use that Dragonfly does not have (trident,
// Looting). vanilla.Register calls it, before the server is configured.
func Enable() {
	enableItems.Do(registerItems)
	enableLooting.Do(registerLooting)
}

func registerItems() {
	for _, it := range []world.Item{Trident{}} {
		if n, m := it.EncodeItem(); !registered(n, m) {
			world.RegisterItem(it)
		}
	}
}

func registered(name string, meta int16) bool {
	_, ok := world.ItemByName(name, meta)
	return ok
}

// EncodeItem ...
func (Trident) EncodeItem() (string, int16) { return "minecraft:trident", 0 }

// MaxCount ...
func (Trident) MaxCount() int { return 1 }

// AttackDamage ...
func (Trident) AttackDamage() float64 { return 8 }

// DurabilityInfo ...
func (Trident) DurabilityInfo() item.DurabilityInfo {
	return item.DurabilityInfo{MaxDurability: 250, BrokenItem: func() item.Stack { return item.Stack{} }, AttackDurability: 1, BreakDurability: 2}
}

// EnchantmentValue ...
func (Trident) EnchantmentValue() int { return 1 }
