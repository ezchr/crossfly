//go:build crossfly_full

// Package social holds the allay, cat gift, villager love/play/hide, warden
// sniff and breeze goals of server/mob/goal/misc (see its README.md).
package social

import "github.com/df-mc/dragonfly/server/mob"

func init() {
	// Allay.
	mob.RegisterGoal("minecraft:behavior.go_and_give_items_to_owner", newGiveItems(false))
	mob.RegisterGoal("minecraft:behavior.go_and_give_items_to_noteblock", newGiveItems(true))
	mob.RegisterGoal("minecraft:behavior.stay_near_noteblock", newStayNearNoteblock)
	// Cat.
	mob.RegisterGoal("minecraft:behavior.drop_item_for", newDropItemFor)
	// Villagers.
	mob.RegisterGoal("minecraft:behavior.make_love", newMakeLove)
	mob.RegisterGoal("minecraft:behavior.receive_love", newReceiveLove)
	mob.RegisterGoal("minecraft:behavior.play", newPlay)
	mob.RegisterGoal("minecraft:behavior.hide", newHide)
	// Warden.
	mob.RegisterGoal("minecraft:behavior.sniff", newSniff)
	// Breeze.
	mob.RegisterGoal("minecraft:behavior.jump_around_target", newJumpAroundTarget)
	mob.RegisterGoal("minecraft:behavior.move_around_target", newMoveAroundTarget)
	mob.RegisterGoal("minecraft:behavior.fire_at_target", newFireAtTarget)
}
