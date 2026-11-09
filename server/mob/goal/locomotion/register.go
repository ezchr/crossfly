//go:build crossfly_full

// Package locomotion holds the Bedrock goals of water mobs, flying mobs,
// slimes and squids: swimming, flocking-aware idling, moving to water, land
// or lava, breaching and breathing (dolphin), playing dead (axolotl), float
// and hover wandering, random flight, phantom circling and swooping, slime
// hopping, squid movement and the guardian beam. Their components (flocking,
// buoyancy, flight flags, friction, drying out) are in
// server/mob/component/locomotion. README.md lists sources and defaults.
package locomotion

import (
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// Actor flags these goals show (gophertunnel EntityDataFlag*).
const (
	flagResting     = protocol.EntityDataFlagResting
	flagPlayingDead = protocol.EntityDataFlagPlayingDead
)

func init() {
	mob.RegisterGoal("minecraft:behavior.random_swim", newRandomSwim)
	mob.RegisterGoal("minecraft:behavior.swim_idle", newSwimIdle)
	mob.RegisterGoal("minecraft:behavior.swim_wander", newSwimWander)
	mob.RegisterGoal("minecraft:behavior.move_to_water", newMoveTo(moveToLiquid, materialWater))
	mob.RegisterGoal("minecraft:behavior.move_to_liquid", newMoveTo(moveToLiquid, materialAny))
	mob.RegisterGoal("minecraft:behavior.move_to_land", newMoveTo(moveToLand, materialAny))
	mob.RegisterGoal("minecraft:behavior.rise_to_liquid_level", newRiseToLiquidLevel)
	mob.RegisterGoal("minecraft:behavior.swim_up_for_breath", newSwimUpForBreath)
	mob.RegisterGoal("minecraft:behavior.swim_with_entity", newSwimWithEntity)
	mob.RegisterGoal("minecraft:behavior.random_breach", newRandomBreach)
	mob.RegisterGoal("minecraft:behavior.find_underwater_treasure", newFindUnderwaterTreasure)
	mob.RegisterGoal("minecraft:behavior.play_dead", newPlayDead)

	mob.RegisterGoal("minecraft:behavior.float_wander", newFloatWander)
	mob.RegisterGoal("minecraft:behavior.random_hover", newRandomHover)
	mob.RegisterGoal("minecraft:behavior.random_fly", newRandomFly)
	mob.RegisterGoal("minecraft:behavior.float_tempt", newFloatTempt)
	mob.RegisterGoal("minecraft:behavior.circle_around_anchor", newCircleAroundAnchor)
	mob.RegisterGoal("minecraft:behavior.swoop_attack", newSwoopAttack)

	mob.RegisterGoal("minecraft:behavior.slime_float", newSlimeFloat)
	mob.RegisterGoal("minecraft:behavior.slime_keep_on_jumping", newSlimeKeepOnJumping)
	mob.RegisterGoal("minecraft:behavior.slime_random_direction", newSlimeRandomDirection)
	mob.RegisterGoal("minecraft:behavior.slime_attack", newSlimeAttack)

	mob.RegisterGoal("minecraft:behavior.squid_dive", newSquidGoal(func(b squidGoal) *squidDive { return &squidDive{b} }))
	mob.RegisterGoal("minecraft:behavior.squid_flee", newSquidGoal(func(b squidGoal) *squidFlee { return &squidFlee{squidGoal: b} }))
	mob.RegisterGoal("minecraft:behavior.squid_idle", newSquidGoal(func(b squidGoal) *squidIdle { return &squidIdle{squidGoal: b} }))
	mob.RegisterGoal("minecraft:behavior.squid_move_away_from_ground", newSquidGoal(func(b squidGoal) *squidMoveAwayFromGround { return &squidMoveAwayFromGround{b} }))
	mob.RegisterGoal("minecraft:behavior.squid_out_of_water", newSquidGoal(func(b squidGoal) *squidOutOfWater { return &squidOutOfWater{b} }))

	mob.RegisterGoal("minecraft:behavior.guardian_attack", newGuardianAttack)
	mob.RegisterGoal("minecraft:behavior.aquatic_charge_attack", newAquaticChargeAttack)

	mob.RegisterSystem(mob.System{Name: "locomotion/squid", Stage: mob.StageTravel, Order: 10, Tick: squidTravel})
	mob.RegisterHurtHook(mob.HurtHook{Name: "locomotion/last_hurt", Hurt: recordHurt})
	mob.RegisterHurtHook(mob.HurtHook{Name: "locomotion/squid_ink", Hurt: squidHurt})
}
