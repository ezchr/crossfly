//go:build crossfly_full

package jvillage

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/world"
)

// CopperGolemID is the copper golem's identifier.
const CopperGolemID = "minecraft:copper_golem"

// giveCopperGolemPoppy is the end of OfferFlowerGoal.stop: a copper golem
// next to the iron golem with nothing on its antenna gets the poppy.
func giveCopperGolemPoppy(m *javamob.Mob, e world.Entity, box cube.BBox) {
	if CopperGolemPoppyHook != nil {
		CopperGolemPoppyHook(m, e, box)
	}
}

// CopperGolemPoppyHook is set by the copper golem.
var CopperGolemPoppyHook func(golem *javamob.Mob, target world.Entity, box cube.BBox)
