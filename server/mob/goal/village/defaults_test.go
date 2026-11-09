//go:build crossfly_full

package village

import (
	"encoding/json"
	"testing"

	"github.com/df-mc/dragonfly/server/mob"
)

func build(t *testing.T, key string, f mob.GoalFactory, raw string) mob.Goal {
	t.Helper()
	spec, err := mob.ParseGoalSpec(key, json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	g, err := f(spec)
	if err != nil {
		t.Fatalf("%v: %v", key, err)
	}
	return g
}

func eq[T comparable](t *testing.T, name string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%v = %v, want %v", name, got, want)
	}
}

// TestGoalDefaults checks the BDS 1.21.3 defaults (Definition constructors
// / LegacyGoalDefinition::parse) of every goal with empty JSON.
func TestGoalDefaults(t *testing.T) {
	w := build(t, "minecraft:behavior.work", newWork, `{}`).(*work)
	eq(t, "work speed", w.speed, 0.5)
	eq(t, "work rain tolerance", w.rainTolerance, -1)
	eq(t, "work active time", w.activeTime, 0)
	eq(t, "work flags", w.Flags(), mob.FlagMove|mob.FlagLook)
	if w.composter != nil {
		t.Error("work has composter settings")
	}
	wc := build(t, "minecraft:behavior.work_composter", newWork, `{}`).(*work)
	c := wc.composter
	if c == nil {
		t.Fatal("work_composter without composter settings")
	}
	eq(t, "use_block_min", c.useMin, 100)
	eq(t, "use_block_max", c.useMax, 200)
	eq(t, "items_per_use_max", c.itemsMax, 20)
	eq(t, "block_interaction_max", c.interactions, 1)
	eq(t, "min_item_count", c.minItems, 10)
	eq(t, "can_fill", c.fill, true)

	s := build(t, "minecraft:behavior.sleep", newSleep, `{}`).(*sleep)
	eq(t, "sleep speed", s.speed, 1.0)
	eq(t, "sleep flags", s.Flags(), mob.FlagMove|mob.FlagLook|mob.FlagJump)

	mi := build(t, "minecraft:behavior.mingle", newMingle, `{}`).(*mingle)
	eq(t, "mingle duration", mi.duration, 20)
	eq(t, "mingle distance", mi.distance, 2.0)
	eq(t, "mingle speed", mi.speed, 1.0)

	mtr := build(t, "minecraft:behavior.move_towards_dwelling_restriction", newMoveTowardsRestriction(true), `{}`).(*moveTowardsRestriction)
	eq(t, "restriction speed", mtr.speed, 1.0)
	eq(t, "restriction flags", mtr.Flags(), mob.FlagMove)

	od := build(t, "minecraft:behavior.open_door", newOpenDoor, `{}`).(*openDoorGoal)
	eq(t, "close_door_after", od.closeAfter, true)

	in := build(t, "minecraft:behavior.move_indoors", newMoveIndoors, `{}`).(*moveIndoors)
	eq(t, "move_indoors speed", in.speed, 0.8)
	eq(t, "move_indoors timeout", in.timeout, 160)

	out := build(t, "minecraft:behavior.move_outdoors", newMoveOutdoors, `{}`).(*moveOutdoors)
	eq(t, "move_outdoors speed", out.speed, 0.5)
	eq(t, "search_range", out.rng, 15)
	eq(t, "search_height", out.height, 5)
	eq(t, "search_count", out.cnt, 10)
	eq(t, "goal_radius", out.radius, 2.0)

	eo := build(t, "minecraft:behavior.explore_outskirts", newExploreOutskirts, `{}`).(*exploreOutskirts)
	eq(t, "next_xz", eo.nextXZ, 5)
	eq(t, "next_y", eo.nextY, 3)
	eq(t, "min_wait", eo.minWait, 60)
	eq(t, "max_travel", eo.maxTravel, 1200)
	eq(t, "explore_dist", eo.dist, 5.0)
	eq(t, "min_dist_from_target", eo.minDistSq, 2.25)
	eq(t, "timer_ratio", eo.ratio, 2.0)

	gh := build(t, "minecraft:behavior.go_home", newGoHome, `{}`).(*goHome)
	eq(t, "go_home interval", gh.interval, 120)
	eq(t, "go_home radius", gh.radiusSq, 0.25)
	eq(t, "calculate_new_path_radius", gh.repathRadius, 2.0)

	h := build(t, "minecraft:behavior.harvest_farm_block", newHarvestFarmBlock, `{}`).(*harvestFarmBlock)
	eq(t, "harvest before search", h.beforeSearch, 20)
	eq(t, "harvest search cooldown", h.searchCooldown, 160)
	eq(t, "harvest new task", h.newTask, 10)
	eq(t, "harvest range", h.rng, 16)
	eq(t, "harvest height", h.height, 1)
	eq(t, "harvest radius", h.radiusSq, 2.25)
	eq(t, "harvest flags", h.Flags(), mob.FlagMove|mob.FlagLook|mob.FlagJump)

	fz := build(t, "minecraft:behavior.fertilize_farm_block", newFertilizeFarmBlock, `{}`).(*fertilizeFarmBlock)
	eq(t, "max_fertilizer_usage", fz.maxUse, 1)
	eq(t, "fertilize search_count", fz.cnt, 9)
	eq(t, "fertilize range", fz.rng, 1)

	ib := build(t, "minecraft:behavior.inspect_bookshelf", newInspectBookshelf, `{}`).(*inspectBookshelf)
	eq(t, "bookshelf count", ib.cnt, 10)
	eq(t, "bookshelf radius", ib.radiusSq, 0.25)

	tw := build(t, "minecraft:behavior.trade_with_player", newTradeWithPlayer, `{}`).(*tradeWithPlayer)
	eq(t, "max_distance_from_player²", tw.maxDistSq, 64.0)
	eq(t, "trade_with_player flags", tw.Flags(), mob.FlagMove|mob.FlagJump)

	ti := build(t, "minecraft:behavior.trade_interest", newTradeInterest, `{}`).(*tradeInterest)
	eq(t, "interest_time", ti.interest, 900)
	eq(t, "remove_item_time", ti.remove, 20)
	eq(t, "carried_item_switch_time", ti.switch_, 40)
	eq(t, "cooldown", ti.cooldown, 40)

	si := build(t, "minecraft:behavior.share_items", newShareItems, `{}`).(*shareItems)
	eq(t, "share max_dist", si.maxDist, 1.0)
	eq(t, "share goal_radius²", si.radiusSq, 0.25)

	of := build(t, "minecraft:behavior.offer_flower", newOfferFlower, `{}`).(*offerFlower)
	eq(t, "max_offer_flower_duration", of.duration, 400)
	eq(t, "chance_to_start", of.chance, 0.000125)

	tf := build(t, "minecraft:behavior.take_flower", newTakeFlower, `{}`).(*takeFlower)
	eq(t, "take_flower speed", tf.speed, 0.5)
	eq(t, "min_wait_time", tf.minWait, 80)
	eq(t, "max_wait_time", tf.maxWait, 400)
	eq(t, "min_distance_to_target²", tf.minDistSq, 4.0)

	cs := build(t, "minecraft:behavior.celebrate_survive", newCelebrateSurvive, `{}`).(*celebrateSurvive)
	eq(t, "celebrate_survive duration", cs.duration, 600)

	ce := build(t, "minecraft:behavior.celebrate", newCelebrate, `{}`).(*celebrate)
	eq(t, "celebrate duration", ce.duration, 600)
	eq(t, "celebration_sound", ce.sound, "celebrate")
	eq(t, "sound_interval min", ce.soundI.Min, 2.0)
	eq(t, "jump_interval max", ce.jumpI.Max, 3.5)
	eq(t, "celebrate flags", ce.Flags(), mob.FlagMove|mob.FlagJump)

	mv := build(t, "minecraft:behavior.move_to_village", newMoveToVillage(false), `{}`).(*moveToVillage)
	eq(t, "move_to_village speed", mv.speed, 1.0)
	eq(t, "move_to_village radius²", mv.radiusSq, 0.25)
	sv := build(t, "minecraft:behavior.stroll_towards_village", newMoveToVillage(true), `{}`).(*moveToVillage)
	eq(t, "start_chance", sv.chance, 0.1)

	dv := build(t, "minecraft:behavior.defend_village_target", newDefendVillageTarget, `{}`).(*defendVillageTarget)
	eq(t, "attack_chance", dv.chance, 0.05)
	if !dv.IsTargetGoal() {
		t.Error("defend_village_target is not a target goal")
	}

	dp := build(t, "minecraft:behavior.drink_potion", newDrinkPotion, `{"potions":[{"id":7,"chance":1}]}`).(*drinkPotion)
	eq(t, "potions", len(dp.potions), 1)
	dm := build(t, "minecraft:behavior.drink_milk", newDrinkMilk, `{}`).(*drinkMilk)
	eq(t, "cooldown_seconds", dm.cooldown, int64(100))

	fc := build(t, "minecraft:behavior.follow_caravan", newFollowCaravan, `{}`).(*followCaravan)
	eq(t, "entity_count", fc.count, 1)
	eq(t, "caravan speed", fc.base, 1.0)

	tb := build(t, "minecraft:behavior.take_block", newTakeBlock, `{}`).(*takeBlock)
	eq(t, "take chance", tb.chance, 0.05)
	eq(t, "take xz", tb.xz, [2]int{-2, 2})
	eq(t, "take y", tb.y, [2]int{0, 3})
	pb := build(t, "minecraft:behavior.place_block", newPlaceBlock, `{}`).(*placeBlock)
	eq(t, "place chance", pb.chance, 0.0005)
	eq(t, "place xz", pb.xz, [2]int{-1, 1})
	eq(t, "place y", pb.y, [2]int{0, 2})

	ti2 := build(t, "minecraft:behavior.transport_items", newTransportItems, `{}`).(*transportItems)
	eq(t, "max_stack_size", ti2.maxStack, 16)
	eq(t, "max_visited_containers", ti2.maxVisited, 10)

	if g := build(t, "minecraft:behavior.move_through_village", newMoveThroughVillage, `{}`); g.CanUse(nil) {
		t.Error("move_through_village can start (BDS 1.21.3: never)")
	}
}
