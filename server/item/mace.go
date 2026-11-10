package item

import "github.com/df-mc/dragonfly/server/world"

// Mace is a heavy weapon that deals more damage the further its wielder fell before hitting (a smash
// attack). Values follow BDS MaceItem.
type Mace struct{}

// AttackDamage returns 6: the base 5 plus the 1 every hit deals.
func (Mace) AttackDamage() float64 { return 6 }

// MaxCount always returns 1.
func (Mace) MaxCount() int { return 1 }

// EnchantmentValue ...
func (Mace) EnchantmentValue() int { return 15 }

// DurabilityInfo ...
func (Mace) DurabilityInfo() DurabilityInfo {
	return DurabilityInfo{MaxDurability: 500, BrokenItem: simpleItem(Stack{}), AttackDurability: 1, BreakDurability: 2}
}

// RepairableBy returns true for breeze rods.
func (Mace) RepairableBy(i Stack) bool {
	_, ok := i.Item().(BreezeRod)
	return ok
}

// SmashBonus returns the damage a smash attack adds for a fall of fd blocks: none up to 1.5 blocks, 4 per
// block up to 3, 2 per block up to 8 and 1 per block beyond that.
func (Mace) SmashBonus(fd float64) float64 {
	switch {
	case fd <= 1.5:
		return 0
	case fd <= 3:
		return 4 * fd
	case fd <= 8:
		return 2*fd + 6
	}
	return fd + 14
}

// SmashAttack reports whether a fall of fd blocks makes a hit a smash attack.
func (Mace) SmashAttack(fd float64) bool { return fd > 1.5 }

// EncodeItem ...
func (Mace) EncodeItem() (name string, meta int16) { return "minecraft:mace", 0 }

var _ world.Item = Mace{}
