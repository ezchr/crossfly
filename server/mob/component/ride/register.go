//go:build crossfly_full

// Package ride holds the riding components of Bedrock mobs: seats and
// getting on (rideable), steering by the controlling player
// (input_ground_controlled, item_controllable, free_camera_controlled),
// jumping and boosting (can_power_jump, dash_action, vertical_movement_action,
// boostable), riders spawned with the mob (addrider), equipment slots and
// the mob container (equippable, inventory). The riding links themselves
// (who rides what, seats, the sessions) are package mob's riding.go; the
// goals are in package mob/goal/ride.
package ride

import (
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

func init() {
	mob.RegisterComponent("minecraft:rideable", newRideable)
	mob.RegisterComponent("minecraft:input_ground_controlled", newGroundControlled)
	mob.RegisterComponent("minecraft:free_camera_controlled", newFreeCamera)
	mob.RegisterComponent("minecraft:item_controllable", newItemControllable)
	mob.RegisterComponent("minecraft:boostable", newBoostable)
	mob.RegisterComponent("minecraft:can_power_jump", newPowerJump)
	mob.RegisterComponent("minecraft:dash_action", newDash)
	mob.RegisterComponent("minecraft:vertical_movement_action", newVerticalMovement)
	mob.RegisterComponent("minecraft:underwater_mount_breathing", newMountBreathing)
	mob.RegisterComponent("minecraft:rotation_locked_to_vehicle", newMarker(protocol.EntityDataFlagRotationLockedToVehicle))
	mob.RegisterComponent("minecraft:addrider", newAddRider)
	mob.RegisterComponent("minecraft:equippable", newEquippable)
	mob.RegisterComponent("minecraft:inventory", newInventory)
	mob.SetPlayerVehicles(playerVehicle)
}
