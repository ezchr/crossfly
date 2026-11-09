package javasession

import "github.com/df-mc/dragonfly/server/world"

// pillagerData is Pillager.IS_CHARGING_CROSSBOW (17): the crossbow draw pose
// while the pillager charges its crossbow (server/mob ChargeHeldItemGoal
// uses the item meanwhile, the Bedrock using_item flag). Once loaded, the
// Java client draws the aiming pose from the charged crossbow item itself.
func pillagerData(_ *Session, d *mobData, e world.Entity, _ string) {
	d.boolean(17, mobBool(e, mobUsing.UsingItem))
}
