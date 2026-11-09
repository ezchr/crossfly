//go:build crossfly_full

// Package locomotion holds the movement components of water and flying mobs
// and the hard-coded actor behaviour BDS keeps in their classes: flocking
// (with the follow_flock goal), buoyancy, can_fly, friction and air drag
// modifiers, bounciness, block_climber, drying_out_timer, bribeable, the
// dolphin move control, float navigation travel (ghast, bat), the fly,
// hover and glide controls of fliers, slime squish and splitting, guardian spikes,
// flopping and curse, blaze hovering.
// The goals are in server/mob/goal/locomotion; its README.md lists sources.
package locomotion

import (
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/filter"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

func init() {
	mob.RegisterComponent("minecraft:flocking", newFlocking)
	mob.RegisterComponent("minecraft:buoyant", newBuoyant)
	mob.RegisterComponent("minecraft:friction_modifier", newFrictionModifier)
	mob.RegisterComponent("minecraft:air_drag_modifier", newAirDragModifier)
	mob.RegisterComponent("minecraft:uses_uniform_air_drag", newUniformAirDrag)
	mob.RegisterComponent("minecraft:bounciness", newBounciness)
	mob.RegisterComponent("minecraft:block_climber", newBlockClimber)
	mob.RegisterComponent("minecraft:drying_out_timer", newDryingOutTimer)
	mob.RegisterComponent("minecraft:bribeable", newBribeable)

	// can_fly: Actor::setCanFly (actor flag CANFLY, 21); RandomPos and the
	// pathfinder read the component.
	mob.RegisterComponent("minecraft:can_fly", newFlag(protocol.EntityDataFlagFly))
	// uses_legacy_friction (1.26) opts a mob out of the newer friction
	// rules; this port's physics is the 1.21.3 (legacy) behaviour, so it
	// changes nothing.
	mob.RegisterComponent("minecraft:uses_legacy_friction", newFlag(0))
	// can_stand_on_powder_snow (1.26; BDS 1.21.3 CanStandOnSnowFlag) is read
	// by block_climber and the navigation. Dragonfly has no powder snow.
	mob.RegisterComponent("minecraft:can_stand_on_powder_snow", newFlag(0))
	// pushable_by_block (1.26; BDS pushable is_pushable_by_piston): pistons
	// may move the mob. Dragonfly has no pistons.
	mob.RegisterComponent("minecraft:pushable_by_block", newFlag(0))

	mob.RegisterInitHook(mob.InitHook{Name: "locomotion/actors", Order: 10, Init: initActor})
	mob.RegisterSystem(mob.System{Name: "locomotion/float_navigation", Stage: mob.StageSensors, Order: 50, Tick: floatNavigationBefore})
	mob.RegisterSystem(mob.System{Name: "locomotion/flight_control", Stage: mob.StageSensors, Order: 51, Tick: flightSwap})
	mob.RegisterSystem(mob.System{Name: "locomotion/guardian", Stage: mob.StageSensors, Order: 52, Tick: guardianTick})
	mob.RegisterSystem(mob.System{Name: "locomotion/blaze", Stage: mob.StageControls, Order: 990, Tick: blazePreTravel})
	mob.RegisterSystem(mob.System{Name: "locomotion/before_travel", Stage: mob.StageControls, Order: 1000, Tick: beforeTravel})
	mob.RegisterSystem(mob.System{Name: "locomotion/after_travel", Stage: mob.StageTravel, Order: 20, Tick: afterTravel})
	mob.RegisterSystem(mob.System{Name: "locomotion/float_facing", Stage: mob.StageTravel, Order: 21, Tick: floatNavigationAfter})
	mob.RegisterHurtHook(mob.HurtHook{Name: "locomotion/guardian_thorns", Hurt: guardianThorns})
	mob.RegisterSystem(mob.System{Name: "locomotion/slime", Stage: mob.StageTravel, Order: 22, Tick: slimeTick})
	mob.RegisterDeathHook(mob.DeathHook{Name: "locomotion/slime_split", Die: slimeDied})
	filter.RegisterFlagSource(waterBottleFlag)
}
