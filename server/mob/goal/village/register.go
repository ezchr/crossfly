//go:build crossfly_full

// Package village holds the villager, wandering trader, illager/raid, iron
// golem, piglin and enderman goals. The components they use (dweller,
// trading, shareables, raids, ...) are in server/mob/component/village.
package village

import "github.com/df-mc/dragonfly/server/mob"

func init() {
	for key, f := range map[string]mob.GoalFactory{
		"minecraft:behavior.work":                              newWork,
		"minecraft:behavior.work_composter":                    newWork,
		"minecraft:behavior.sleep":                             newSleep,
		"minecraft:behavior.mingle":                            newMingle,
		"minecraft:behavior.move_towards_home_restriction":     newMoveTowardsRestriction(false),
		"minecraft:behavior.move_towards_dwelling_restriction": newMoveTowardsRestriction(true),
		"minecraft:behavior.open_door":                         newOpenDoor,
		"minecraft:behavior.restrict_open_door":                newRestrictOpenDoor,
		"minecraft:behavior.move_indoors":                      newMoveIndoors,
		"minecraft:behavior.move_outdoors":                     newMoveOutdoors,
		"minecraft:behavior.explore_outskirts":                 newExploreOutskirts,
		"minecraft:behavior.go_home":                           newGoHome,
		"minecraft:behavior.harvest_farm_block":                newHarvestFarmBlock,
		"minecraft:behavior.fertilize_farm_block":              newFertilizeFarmBlock,
		"minecraft:behavior.inspect_bookshelf":                 newInspectBookshelf,
		"minecraft:behavior.trade_with_player":                 newTradeWithPlayer,
		"minecraft:behavior.look_at_trading_player":            newLookAtTradingPlayer,
		"minecraft:behavior.trade_interest":                    newTradeInterest,
		"minecraft:behavior.share_items":                       newShareItems,
		"minecraft:behavior.offer_flower":                      newOfferFlower,
		"minecraft:behavior.take_flower":                       newTakeFlower,
		"minecraft:behavior.celebrate_survive":                 newCelebrateSurvive,
		"minecraft:behavior.celebrate":                         newCelebrate,
		"minecraft:behavior.move_to_village":                   newMoveToVillage(false),
		"minecraft:behavior.stroll_towards_village":            newMoveToVillage(true),
		"minecraft:behavior.follow_target_captain":             newFollowTargetCaptain,
		"minecraft:behavior.defend_village_target":             newDefendVillageTarget,
		"minecraft:behavior.move_through_village":              newMoveThroughVillage,
		"minecraft:behavior.drink_potion":                      newDrinkPotion,
		"minecraft:behavior.drink_milk":                        newDrinkMilk,
		"minecraft:behavior.follow_caravan":                    newFollowCaravan,
		"minecraft:behavior.admire_item":                       newAdmireItem,
		"minecraft:behavior.barter":                            newBarter,
		"minecraft:behavior.take_block":                        newTakeBlock,
		"minecraft:behavior.place_block":                       newPlaceBlock,
		"minecraft:behavior.transport_items":                   newTransportItems,
	} {
		mob.RegisterGoal(key, f)
	}
}
