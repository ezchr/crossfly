//go:build crossfly_full

package item

import "github.com/df-mc/dragonfly/server/world"

// Saddle is an item put on a horse, donkey, mule, pig, strider, camel or
// nautilus so a player can ride and steer it. Mobs take it through their
// minecraft:equippable or minecraft:interact components.
type Saddle struct{}

// MaxCount ...
func (Saddle) MaxCount() int {
	return 1
}

// EncodeItem ...
func (Saddle) EncodeItem() (name string, meta int16) {
	return "minecraft:saddle", 0
}

// HorseArmourTier is the material of horse armour.
type HorseArmourTier int

const (
	HorseArmourLeather HorseArmourTier = iota
	HorseArmourIron
	HorseArmourGold
	HorseArmourDiamond
	HorseArmourCopper
	HorseArmourNetherite
)

// HorseArmourTiers returns every horse armour tier.
func HorseArmourTiers() []HorseArmourTier {
	return []HorseArmourTier{HorseArmourLeather, HorseArmourIron, HorseArmourGold, HorseArmourDiamond, HorseArmourCopper, HorseArmourNetherite}
}

// HorseArmour is armour a horse wears in its armour slot (minecraft:equippable
// slot 1).
type HorseArmour struct {
	Tier HorseArmourTier
}

// MaxCount ...
func (HorseArmour) MaxCount() int {
	return 1
}

// DefencePoints is the armour the horse gets from it (Java/Bedrock values).
func (h HorseArmour) DefencePoints() float64 {
	switch h.Tier {
	case HorseArmourLeather:
		return 3
	case HorseArmourIron:
		return 5
	case HorseArmourGold:
		return 7
	case HorseArmourDiamond:
		return 11
	case HorseArmourCopper:
		return 4
	case HorseArmourNetherite:
		return 19
	}
	return 0
}

// EncodeItem ...
func (h HorseArmour) EncodeItem() (name string, meta int16) {
	switch h.Tier {
	case HorseArmourLeather:
		return "minecraft:leather_horse_armor", 0
	case HorseArmourIron:
		return "minecraft:iron_horse_armor", 0
	case HorseArmourGold:
		return "minecraft:golden_horse_armor", 0
	case HorseArmourDiamond:
		return "minecraft:diamond_horse_armor", 0
	case HorseArmourCopper:
		return "minecraft:copper_horse_armor", 0
	}
	return "minecraft:netherite_horse_armor", 0
}

// boostRidden boosts the entity user rides with held (a carrot on a stick on
// a pig, a warped fungus on a stick on a strider) and damages held by what
// the entity asks.
func boostRidden(tx *world.Tx, user User, held Stack, ctx *UseContext) bool {
	r, ok := user.(interface{ RidingEntityHandle() *world.EntityHandle })
	if !ok || r.RidingEntityHandle() == nil {
		return false
	}
	e, ok := r.RidingEntityHandle().Entity(tx)
	if !ok {
		return false
	}
	b, ok := e.(interface {
		BoostWith(rider world.Entity, held Stack) (int, bool)
	})
	if !ok {
		return false
	}
	dmg, ok := b.BoostWith(user, held)
	if ok {
		ctx.Damage += dmg
	}
	return ok
}

// brokenStick is what a carrot or warped fungus on a stick turns into when it
// breaks: a fishing rod, if the server has the item.
func brokenStick() Stack {
	if it, ok := world.ItemByName("minecraft:fishing_rod", 0); ok {
		return NewStack(it, 1)
	}
	return Stack{}
}

// Use boosts the saddled mob the user rides and steers.
func (c CarrotOnAStick) Use(tx *world.Tx, user User, ctx *UseContext) bool {
	return boostRidden(tx, user, NewStack(c, 1), ctx)
}

// DurabilityInfo ...
func (CarrotOnAStick) DurabilityInfo() DurabilityInfo {
	return DurabilityInfo{MaxDurability: 26, BrokenItem: brokenStick}
}

// Use boosts the saddled mob the user rides and steers.
func (c WarpedFungusOnAStick) Use(tx *world.Tx, user User, ctx *UseContext) bool {
	return boostRidden(tx, user, NewStack(c, 1), ctx)
}

// DurabilityInfo ...
func (WarpedFungusOnAStick) DurabilityInfo() DurabilityInfo {
	return DurabilityInfo{MaxDurability: 100, BrokenItem: brokenStick}
}
