//go:build crossfly_full

package mob

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
)

// Wolf armour: the damage absorption and cracks of the item (BDS WolfArmorItem's
// DamageAbsorptionItemComponent and DamageSensorItemComponent).

// wolfArmourCracks are WolfArmorItem::CRACKS_THRESHOLDS: the damage values at which wolf armour cracks a bit
// more (DamageSensorItemComponentUtils::onHurtAndBreak plays armor.crack_wolf when one is reached).
var wolfArmourCracks = [...]int{4, 20, 44, 64}

// Wolf armour sounds (LevelSoundEvent names).
const (
	soundWolfArmourCrack = "armor.crack_wolf"
	soundWolfArmourBreak = "armor.break_wolf"
)

// wolfArmourAbsorbs reports whether worn wolf armour takes damage of src: the causes of
// WolfArmorItem::ABSORBABLE_DAMAGE_CAUSES (anvil, block and entity explosions, campfires, charging, contact,
// entity attacks, falls, falling blocks, fire, fire ticks, fireworks, flying into a wall, lava, lightning,
// magma, projectiles, ram attacks, sonic booms, stalactites and stalagmites) as Dragonfly's sources have them.
// Drowning, suffocation, the void, magic, wither, starving, freezing and the like still hurt the wolf.
func wolfArmourAbsorbs(src world.DamageSource) bool {
	switch src.(type) {
	case entity.AttackDamageSource, entity.ProjectileDamageSource, entity.ExplosionDamageSource,
		entity.FallDamageSource, entity.GlideDamageSource, entity.LightningDamageSource,
		block.FireDamageSource, block.LavaDamageSource, block.MagmaDamageSource, block.DamageSource:
		return true
	}
	if c, ok := src.(interface{ DamageCause() string }); ok {
		switch c.DamageCause() {
		case "anvil", "block_explosion", "campfire", "charging", "contact", "entity_attack", "entity_explosion",
			"fall", "falling_block", "fire", "fire_tick", "fireworks", "fly_into_wall", "lava", "lightning",
			"magma", "projectile", "ram_attack", "sonic_boom", "soul_campfire", "stalactite", "stalagmite":
			return true
		}
	}
	return false
}

// absorbWithArmour is DamageAbsorptionItemComponentUtils::tryAbsorbDamageWithEquipment for the body slot: worn
// wolf armour takes dmg (rounded up) as durability damage in place of the mob for the causes it absorbs, and
// absorbWithArmour reports true; the mob then loses no health. Mob::_hurt calls it with the damage before
// armour reduction, once the hit got past the hurt cooldown. Unbreaking may spare the armour
// (ItemStackBase::hurtAndBreak), and then the mob takes the hit. When the damage reaches a crack threshold
// the armour cracks (armor.crack_wolf); when it runs out of durability it breaks (armor.break_wolf) and is gone.
func (m *Mob) absorbWithArmour(dmg float64, src world.DamageSource) bool {
	s := m.s.bodyArmour
	if _, ok := s.Item().(item.WolfArmour); !ok || s.Empty() || !wolfArmourAbsorbs(src) {
		return false
	}
	n := int(math.Ceil(dmg))
	if n <= 0 {
		return false
	}
	if e, ok := s.Enchantment(enchantment.Unbreaking); ok && e.Level() > 0 {
		// Item::getDamageChance: 100/(level+1) percent.
		if m.Rand().IntN(100) >= 100/(e.Level()+1) {
			return false
		}
	}
	before := s.MaxDurability() - s.Durability()
	after := before + n
	for _, t := range wolfArmourCracks {
		if before < t && after >= t {
			m.PlaySound(soundWolfArmourCrack)
			break
		}
	}
	left := s.Damage(n)
	if left.Empty() {
		m.PlaySound(soundWolfArmourBreak)
	}
	m.SetBodyArmour(left)
	return true
}

// RepairSlot repairs the item in slot by amount durability, as minecraft:interact's repair_entity_item
// ({"slot": "slot.armor.body", "amount": 8}: an armadillo scute used on a wolf). It reports whether the item
// was damaged and got repaired. (The interaction plays armor.repair_wolf itself.)
func (m *Mob) RepairSlot(slot string, amount int) bool {
	if slot != "slot.armor.body" || amount <= 0 {
		return false
	}
	s := m.s.bodyArmour
	if s.Empty() || s.MaxDurability() <= 0 || s.Durability() >= s.MaxDurability() {
		return false
	}
	m.SetBodyArmour(s.WithDurability(min(s.Durability()+amount, s.MaxDurability())))
	return true
}
