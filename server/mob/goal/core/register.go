//go:build crossfly_full

package core

import "github.com/df-mc/dragonfly/server/mob"

func init() {
	mob.RegisterGoal("minecraft:behavior.random_stroll", newRandomStroll)
	mob.RegisterGoal("minecraft:behavior.look_at_player", newLookAt(true))
	mob.RegisterGoal("minecraft:behavior.look_at_entity", newLookAt(false))
	mob.RegisterGoal("minecraft:behavior.random_look_around", newRandomLookAround)
	mob.RegisterGoal("minecraft:behavior.float", newFloat)
	mob.RegisterGoal("minecraft:behavior.panic", newPanic)
	mob.RegisterGoal("minecraft:behavior.tempt", newTempt)
	mob.RegisterGoal("minecraft:behavior.breed", newBreed)
	mob.RegisterGoal("minecraft:behavior.follow_parent", newFollowParent)
	mob.RegisterGoal("minecraft:behavior.eat_block", newEatBlock)
	mob.RegisterGoal("minecraft:behavior.mount_pathing", newMountPathing)
}
