//go:build crossfly_full

// Package loot holds the item helpers that mob loot, spawn equipment and
// mob combat share: the Looting enchantment (Bedrock id 14, which Dragonfly
// lacks), the trident item (drowned gear), the random
// enchanting loot functions use (BDS EnchantUtils::randomlyEnchant), the
// BDS "special multiplier" (clamped regional difficulty), the
// was-picked-up mark on stacks a mob picked up, the held weapon's damage and
// enchantments, and the crossbow charge state ranged mobs wait for.
//
// It registers no mob components or goals; package mob/component/core and
// mob/goal/combat import it, mob/goal/equip has the charge_held_item goal.
package loot
