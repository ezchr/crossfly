//go:build crossfly_full

// Package misc holds the remaining Bedrock goals: timer flags, ram and
// charge attacks, block avoidance and searches, golem and illager goals,
// following, spells; its sub-packages animal (fox, panda, frog, sniffer,
// silverfish) and social (allay, cat gifts, villager love and play, warden
// sniff, breeze) register the rest. Helpers are in package kit. See
// README.md.
package misc

import (
	"github.com/df-mc/dragonfly/server/mob"
	_ "github.com/df-mc/dragonfly/server/mob/goal/misc/animal"  // fox, panda, frog, sniffer, silverfish
	_ "github.com/df-mc/dragonfly/server/mob/goal/misc/hostile" // enderman, ravager, hoglin, breeze extras
	_ "github.com/df-mc/dragonfly/server/mob/goal/misc/social"  // allay, cat, villager, warden, breeze
)

func init() {
	// minecraft:trail is a component (the snow golem's snow), here because it
	// needs component/combat's mobGriefing hook, which component/misc cannot
	// import.
	mob.RegisterComponent("minecraft:trail", newTrail)
	for key, flag := range timerFlags {
		mob.RegisterGoal(key, newTimerFlag(flag))
	}
	mob.RegisterGoal("minecraft:behavior.ram_attack", newRamAttack)
	mob.RegisterGoal("minecraft:behavior.charge_attack", newChargeAttack)
	mob.RegisterGoal("minecraft:behavior.knockback_roar", newKnockbackRoar)
	mob.RegisterGoal("minecraft:behavior.move_to_block", newMoveToBlock)
	mob.RegisterGoal("minecraft:behavior.stomp_turtle_egg", newStompTurtleEgg)
	mob.RegisterGoal("minecraft:behavior.avoid_block", newAvoidBlock)
	mob.RegisterGoal("minecraft:behavior.move_to_random_block", newMoveToRandomBlock)
	mob.RegisterGoal("minecraft:behavior.move_towards_target", newMoveTowardsTarget)
	mob.RegisterGoal("minecraft:behavior.target_when_pushed", newTargetWhenPushed)
	mob.RegisterGoal("minecraft:behavior.hold_ground", newHoldGround)
	mob.RegisterGoal("minecraft:behavior.follow_mob", newFollowMob)
	mob.RegisterGoal("minecraft:behavior.look_at_target", newLookAtTarget)
	mob.RegisterGoal("minecraft:behavior.send_event", newSendEvent)
	mob.RegisterGoal("minecraft:behavior.summon_entity", newSummonEntity)
}
