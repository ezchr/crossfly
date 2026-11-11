package item

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// FishingRod is a tool that casts a fishing hook. The hook pulls an entity it caught towards the rod's holder when
// reeled in, and knocks back what it hits.
type FishingRod struct{}

// MaxCount always returns 1.
func (FishingRod) MaxCount() int {
	return 1
}

// DurabilityInfo: 384 uses (the DurabilityItemComponent(384) of the BDS FishingRodItem). Reeling in costs what
// entity.ReelFishingHook returns, nothing else wears the rod.
func (FishingRod) DurabilityInfo() DurabilityInfo {
	return DurabilityInfo{
		MaxDurability: 384,
		BrokenItem:    simpleItem(Stack{}),
	}
}

// EnchantmentValue ...
func (FishingRod) EnchantmentValue() int {
	return 1
}

// Use casts a fishing hook, or reels in the one the user has out (BDS FishingRodItem::use).
func (FishingRod) Use(tx *world.Tx, user User, ctx *UseContext) bool {
	conf := tx.World().EntityRegistry().Config()
	if conf.ReelFishingHook != nil {
		if dmg, ok := conf.ReelFishingHook(tx, user); ok {
			ctx.DamageItem(dmg)
			tx.PlaySound(user.Position(), sound.FishingRodRetrieve{})
			return true
		}
	}
	if conf.FishingHook == nil {
		return false
	}
	tx.AddEntity(conf.FishingHook(world.EntitySpawnOpts{Position: eyePosition(user)}, user))
	tx.PlaySound(eyePosition(user), sound.FishingRodCast{})
	return true
}

// EncodeItem ...
func (FishingRod) EncodeItem() (name string, meta int16) {
	return "minecraft:fishing_rod", 0
}
