package javasession

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// Charging mobs: the ranged attack goal's charge (server/mob goal/combat,
// RangedAttackGoal charge mode) sets the Bedrock charged actor flag.

// ghastData is Ghast.DATA_IS_CHARGING (16): the mouth opens to shoot.
func ghastData(_ *Session, d *mobData, e world.Entity, _ string) {
	d.boolean(16, mobBool(e, mobCharged.Charged) || mobActorFlag(e, protocol.EntityDataFlagCharged))
}

// blazeData is Blaze.DATA_FLAGS_ID (16): 0x01 on fire while it charges its
// fireballs.
func blazeData(_ *Session, d *mobData, e world.Entity, _ string) {
	d.byteVal(16, bit(mobActorFlag(e, protocol.EntityDataFlagCharged), 0x01))
}
