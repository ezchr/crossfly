//go:build crossfly_full

package item

import "github.com/df-mc/dragonfly/server/world"

// WolfArmour is armour the owner of a tamed adult wolf puts on it (the wolf's minecraft:interact equips it into
// slot.armor.body). While worn it takes the damage of most causes in place of the wolf, losing that much
// durability, until it breaks; shears take it off again and armadillo scutes repair it. It is only of use with
// the mobs of Crossfly Full.
type WolfArmour struct{}

// MaxCount always returns 1.
func (WolfArmour) MaxCount() int {
	return 1
}

// DurabilityInfo ...
func (WolfArmour) DurabilityInfo() DurabilityInfo {
	// WolfArmorItem: DurabilityItemComponent(64).
	return DurabilityInfo{
		MaxDurability: 64,
		BrokenItem:    simpleItem(Stack{}),
	}
}

// DefencePoints is the armour the wolf gets from it (WolfArmorItem::getArmorValue). It counts for the damage
// the armour does not take itself.
func (WolfArmour) DefencePoints() float64 {
	return 11
}

// Toughness is the armour toughness of wolf armour, which has none.
func (WolfArmour) Toughness() float64 {
	return 0
}

// RepairableBy reports whether i repairs wolf armour in an anvil: armadillo scutes do
// (WolfArmorItem::isValidRepairItem).
func (WolfArmour) RepairableBy(i Stack) bool {
	_, ok := i.Item().(ArmadilloScute)
	return ok
}

// EncodeItem ...
func (WolfArmour) EncodeItem() (name string, meta int16) {
	return "minecraft:wolf_armor", 0
}

func init() {
	mobItems = append(mobItems, func() {
		world.RegisterItem(WolfArmour{})
	})
}
