//go:build crossfly_full

package mob

import (
	"math"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
)

// defencePointer is armour worn in the body slot (horse armour, wolf
// armour): BDS HorseArmorItem/WolfArmorItem::getArmorValue.
type defencePointer interface{ DefencePoints() float64 }

// ArmourValue is Mob::getArmorValue: the defence points of the worn armour
// (the four armour slots and the body slot), plus the natural armour of
// some classes: Zombie::getArmorValue +2 (zombies, husks, drowned, zombie
// villagers and zombie pigmen), WitherBoss::getArmorValue +4, both capped
// at 20, and LavaSlime::getArmorValue 3 per size (the variant). The
// shulker's closed shell is its own damage modifier (component/shulker).
func (m *Mob) ArmourValue() float64 {
	v := 0.0
	if m.s.armour != nil {
		for _, it := range m.s.armour.Items() {
			if a, ok := it.Item().(item.Armour); ok {
				v += a.DefencePoints()
			}
		}
	}
	if b, ok := m.s.bodyArmour.Item().(defencePointer); ok && !m.s.bodyArmour.Empty() {
		v += b.DefencePoints()
	}
	switch m.Identifier() {
	case "minecraft:zombie", "minecraft:husk", "minecraft:drowned", "minecraft:zombie_villager",
		"minecraft:zombie_villager_v2", "minecraft:zombie_pigman":
		v = math.Min(v+2, 20)
	case "minecraft:wither":
		v = math.Min(v+4, 20)
	case "minecraft:magma_cube":
		v = float64(3 * m.Variant())
	}
	return v
}

// armourToughness is the toughness of the worn armour.
func (m *Mob) armourToughness() float64 {
	t := 0.0
	if m.s.armour != nil {
		for _, it := range m.s.armour.Items() {
			if a, ok := it.Item().(item.Armour); ok {
				t += a.Toughness()
			}
		}
	}
	return t
}

// armourReduced is Mob::getDamageAfterArmorReduction: each effective armour
// point takes 4% off; the effective points are the armour value less
// damage/(2+toughness/4), at least a fifth of the armour value and at most
// 20. (BDS passes the damage as a negative health change, which is why its
// code adds the term.)
func (m *Mob) armourReduced(dmg float64) float64 {
	armour := m.ArmourValue()
	if armour <= 0 {
		return dmg
	}
	f := math.Min(20, math.Max(armour-dmg/(2+m.armourToughness()/4), armour*0.2))
	return dmg * (1 - f*0.04)
}

// protectionReduced is Mob::getDamageAfterEnchantReduction: the protection
// enchantments of the worn armour.
func (m *Mob) protectionReduced(dmg float64, src world.DamageSource) float64 {
	if m.s.armour == nil {
		return dmg
	}
	var ench []item.Enchantment
	for _, it := range m.s.armour.Items() {
		ench = append(ench, it.Enchantments()...)
	}
	if len(ench) == 0 {
		return dmg
	}
	return dmg - dmg*enchantment.ProtectionFactor(src, ench)
}

// hurtArmour is Mob::hurtArmor and hurtArmorSlots: every worn armour piece
// loses max(damage/4, 1) durability (less with Unbreaking).
func (m *Mob) hurtArmour(dmg float64) {
	if m.s.armour == nil {
		return
	}
	durable := false
	for _, it := range m.s.armour.Items() {
		durable = durable || it.MaxDurability() > 0
	}
	if !durable {
		// Armour.Damage sets every slot, which shows the armour again.
		return
	}
	m.s.armour.Damage(math.Floor(dmg), func(s item.Stack, d int) item.Stack {
		if s.Empty() || s.MaxDurability() <= 0 || d <= 0 {
			return s
		}
		if e, ok := s.Enchantment(enchantment.Unbreaking); ok {
			d = enchantment.Unbreaking.Reduce(s.Item(), e.Level(), d)
		}
		return s.Damage(d)
	})
}
