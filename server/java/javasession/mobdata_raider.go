package javasession

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// celebrating reports whether the mob has the Bedrock celebrating (or
// celebrating_special) actor flag: a raider after a won raid
// (behavior.celebrate), a piglin after a hoglin hunt (celebrate_hunt).
func celebrating(e world.Entity) bool {
	return mobFlag(e, protocol.EntityDataFlagCelebrating) || mobFlag(e, protocol.EntityDataFlagCelebratingSpecial)
}

// raiderData is Raider.IS_CELEBRATING (16): vindicators, pillagers, evokers,
// illusioners, witches and ravagers. A celebrating vindicator, evoker or
// illusioner raises its arms (IllagerArmPose.CELEBRATING) instead of
// crossing them.
func raiderData(_ *Session, d *mobData, e world.Entity, _ string) {
	d.boolean(16, celebrating(e))
}

// piglinData is Piglin.DATA_BABY_ID (17), DATA_IS_CHARGING_CROSSBOW (18) and
// DATA_IS_DANCING (19): the crossbow draw pose while the piglin charges
// (ChargeHeldItemGoal uses the item meanwhile, the Bedrock using_item flag)
// and the dance after a hoglin hunt (PiglinArmPose.CROSSBOW_CHARGE and
// DANCING). Its melee pose comes from the aggressive mob flag and the weapon
// in its hand (PiglinArmPose.ATTACKING_WITH_MELEE_WEAPON).
func piglinData(_ *Session, d *mobData, e world.Entity, _ string) {
	d.boolean(17, mobBool(e, mobBaby.Baby))
	d.boolean(18, mobBool(e, mobUsing.UsingItem))
	d.boolean(19, celebrating(e))
}
