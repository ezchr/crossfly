//go:build crossfly_full

package physics

import "github.com/df-mc/dragonfly/server/mob"

func init() {
	mob.SetTravel(Travel)
	mob.SetKnockBack(KnockBack)

	mob.RegisterComponent("minecraft:physics", newPhysics)
	mob.RegisterComponent("minecraft:can_climb", newCanClimb)
	mob.RegisterComponent("minecraft:pushable", newPushable)
	mob.RegisterComponent("minecraft:pushable_by_entity", newPushableByEntity)
	mob.RegisterComponent("minecraft:push_through", newPushThrough)
	mob.RegisterComponent("minecraft:water_movement", newWaterMovement)
	mob.RegisterComponent("minecraft:variable_max_auto_step", newVariableMaxAutoStep)
	mob.RegisterComponent("minecraft:block_movement_slowdown_immunity", newSlowdownImmunity)

}
