//go:build crossfly_full

// Package animal holds the fox, panda, frog, sniffer and silverfish goals
// of server/mob/goal/misc (see its README.md).
package animal

import "github.com/df-mc/dragonfly/server/mob"

func init() {
	// Fox (and camel: random_look_around_and_sit; rabbit: raid_garden).
	mob.RegisterGoal("minecraft:behavior.stalk_and_pounce_on_target", newStalkAndPounce)
	mob.RegisterGoal("minecraft:behavior.nap", newNap)
	mob.RegisterGoal("minecraft:behavior.find_cover", newFindCover)
	mob.RegisterGoal("minecraft:behavior.eat_carried_item", newEatCarriedItem)
	mob.RegisterGoal("minecraft:behavior.random_look_around_and_sit", newRandomLookAroundAndSit)
	mob.RegisterGoal("minecraft:behavior.raid_garden", newRaidGarden)
	mob.RegisterGoal("minecraft:behavior.defend_trusted_target", newDefendTrustedTarget)
	// Panda.
	mob.RegisterGoal("minecraft:behavior.roll", newRoll)
	mob.RegisterGoal("minecraft:behavior.sneeze", newSneeze)
	mob.RegisterGoal("minecraft:behavior.snacking", newSnack)
	mob.RegisterGoal("minecraft:behavior.lay_down", newLayDown)
	mob.RegisterGoal("minecraft:behavior.scared", newScared)
	mob.RegisterGoal("minecraft:behavior.random_sitting", newRandomSitting)
	// Frog (jump_to_block also goat, lay_egg also turtle).
	mob.RegisterGoal("minecraft:behavior.croak", newCroak)
	mob.RegisterGoal("minecraft:behavior.eat_mob", newEatMob)
	mob.RegisterGoal("minecraft:behavior.jump_to_block", newJumpToBlock)
	mob.RegisterGoal("minecraft:behavior.lay_egg", newLayEgg)
	// Sniffer, silverfish.
	mob.RegisterGoal("minecraft:behavior.random_search_and_dig", newRandomSearchAndDig)
	mob.RegisterGoal("minecraft:behavior.silverfish_merge_with_stone", newMergeWithStone)
	mob.RegisterGoal("minecraft:behavior.silverfish_wake_up_friends", newWakeUpFriends)
}
