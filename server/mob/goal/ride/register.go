//go:build crossfly_full

// Package ride holds the riding, taming and owner goals of Bedrock mobs:
// player_ride_tamed, controlled_by_player, run_around_like_crazy,
// find_mount, skeleton_horse_trap (riding) and follow_owner,
// stay_while_sitting, teleport_to_owner, owner_hurt_by_target,
// owner_hurt_target, beg, pet_sleep_with_owner,
// ocelot_sit_on_block (owners and pets). It links the
// component packages mob/component/ride and mob/component/tame, so one
// import registers the whole area.
package ride

import (
	"github.com/df-mc/dragonfly/server/mob"
	_ "github.com/df-mc/dragonfly/server/mob/component/ride"
	_ "github.com/df-mc/dragonfly/server/mob/component/tame"
)

func init() {
	mob.RegisterGoal("minecraft:behavior.player_ride_tamed", newPlayerRideTamed)
	mob.RegisterGoal("minecraft:behavior.controlled_by_player", newControlledByPlayer)
	mob.RegisterGoal("minecraft:behavior.run_around_like_crazy", newRunAroundLikeCrazy)
	mob.RegisterGoal("minecraft:behavior.find_mount", newFindMount)
	mob.RegisterGoal("minecraft:behavior.skeleton_horse_trap", newSkeletonHorseTrap)
	mob.RegisterGoal("minecraft:behavior.follow_owner", newFollowOwner)
	mob.RegisterGoal("minecraft:behavior.stay_while_sitting", newStayWhileSitting)
	mob.RegisterGoal("minecraft:behavior.teleport_to_owner", newTeleportToOwner)
	mob.RegisterGoal("minecraft:behavior.owner_hurt_by_target", newOwnerHurtBy(false))
	mob.RegisterGoal("minecraft:behavior.owner_hurt_target", newOwnerHurtBy(true))
	mob.RegisterGoal("minecraft:behavior.beg", newBeg)
	mob.RegisterGoal("minecraft:behavior.pet_sleep_with_owner", newPetSleepWithOwner)
	mob.RegisterGoal("minecraft:behavior.ocelot_sit_on_block", newOcelotSitOnBlock)
	mob.RegisterHurtHook(mob.HurtHook{Name: "goal/ride owner_hurt_target", Hurt: recordOwnerHit})
}
