//go:build crossfly_full

package loot

import (
	"sync"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// Trident and FishingRod are the two items vanilla spawn equipment hands
// out that Dragonfly has no item for (drowned_equipment.json,
// drowned_ranged_equipment.json). They are registered here, unless
// something registered the names first, so drowned can hold, show and drop
// them. They hold and hit like the vanilla items (trident melee 9, both
// durable and enchantable) but have no use action: throwing tridents and
// fishing are not implemented.
type (
	Trident    struct{}
	FishingRod struct{}
)

var enableItems sync.Once

// Enable registers the items and the enchantment mobs drop and use that Dragonfly does not have (trident, fishing
// rod, Looting). vanilla.Register calls it, before the server is configured.
func Enable() {
	enableItems.Do(registerItems)
	enableLooting.Do(registerLooting)
}

func registerItems() {
	for _, it := range []world.Item{Trident{}, FishingRod{}} {
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

// EncodeItem ...
func (FishingRod) EncodeItem() (string, int16) { return "minecraft:fishing_rod", 0 }

// MaxCount ...
func (FishingRod) MaxCount() int { return 1 }

// DurabilityInfo ...
func (FishingRod) DurabilityInfo() item.DurabilityInfo {
	return item.DurabilityInfo{MaxDurability: 384, BrokenItem: func() item.Stack { return item.Stack{} }}
}

// EnchantmentValue ...
func (FishingRod) EnchantmentValue() int { return 1 }
