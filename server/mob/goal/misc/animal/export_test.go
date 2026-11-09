//go:build crossfly_full

package animal

import "github.com/df-mc/dragonfly/server/mob"

// Factories are the goal factories by key, for the external tests.
var Factories = map[string]mob.GoalFactory{
	"minecraft:behavior.stalk_and_pounce_on_target":  newStalkAndPounce,
	"minecraft:behavior.nap":                         newNap,
	"minecraft:behavior.find_cover":                  newFindCover,
	"minecraft:behavior.eat_carried_item":            newEatCarriedItem,
	"minecraft:behavior.random_look_around_and_sit":  newRandomLookAroundAndSit,
	"minecraft:behavior.raid_garden":                 newRaidGarden,
	"minecraft:behavior.defend_trusted_target":       newDefendTrustedTarget,
	"minecraft:behavior.roll":                        newRoll,
	"minecraft:behavior.sneeze":                      newSneeze,
	"minecraft:behavior.snacking":                    newSnack,
	"minecraft:behavior.lay_down":                    newLayDown,
	"minecraft:behavior.scared":                      newScared,
	"minecraft:behavior.random_sitting":              newRandomSitting,
	"minecraft:behavior.croak":                       newCroak,
	"minecraft:behavior.eat_mob":                     newEatMob,
	"minecraft:behavior.jump_to_block":               newJumpToBlock,
	"minecraft:behavior.lay_egg":                     newLayEgg,
	"minecraft:behavior.random_search_and_dig":       newRandomSearchAndDig,
	"minecraft:behavior.silverfish_merge_with_stone": newMergeWithStone,
	"minecraft:behavior.silverfish_wake_up_friends":  newWakeUpFriends,
}
