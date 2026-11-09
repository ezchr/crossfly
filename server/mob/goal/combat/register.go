//go:build crossfly_full

// Package combat holds the hostile minecraft:behavior.* goals: target
// selection (nearest_attackable_target, hurt_by_target), melee_attack,
// melee_box_attack, ranged_attack, swell, leap_at_target, flee_sun,
// avoid_mob_type, pickup_items, and the equip_item / use_kinetic_weapon
// stubs. Ported from the BDS 1.21.3 goals; see README.md.
package combat

import "github.com/df-mc/dragonfly/server/mob"

func init() {
	mob.RegisterGoal("minecraft:behavior.nearest_attackable_target", newNearestAttackableTarget)
	mob.RegisterGoal("minecraft:behavior.hurt_by_target", newHurtByTarget)
	mob.RegisterGoal("minecraft:behavior.melee_attack", newMeleeAttack)
	mob.RegisterGoal("minecraft:behavior.melee_box_attack", newMeleeBoxAttack)
	mob.RegisterGoal("minecraft:behavior.ranged_attack", newRangedAttack)
	mob.RegisterGoal("minecraft:behavior.swell", newSwell)
	mob.RegisterGoal("minecraft:behavior.leap_at_target", newLeapAtTarget)
	mob.RegisterGoal("minecraft:behavior.flee_sun", newFleeSun)
	mob.RegisterGoal("minecraft:behavior.avoid_mob_type", newAvoidMobType)
	mob.RegisterGoal("minecraft:behavior.pickup_items", newPickupItems)
	mob.RegisterGoal("minecraft:behavior.equip_item", newEquipItem)
	mob.RegisterGoal("minecraft:behavior.use_kinetic_weapon", newUseKineticWeapon)
}
