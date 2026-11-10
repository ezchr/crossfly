package javasession

import (
	"math"

	"github.com/df-mc/dragonfly/server/world"
)

// Entity data of the Java mob mode's "other hostiles & water" mobs
// (server/mob/javamob/hostile) that the tables in mobdata.go do not send.
// This init runs after mobdata.go's (files initialise in name order).
func init() {
	// Phantom.ID_SIZE (16): the phantom's size shows as its scale, 1 +
	// 0.15 per size step (Phantom.getDefaultDimensions). Only a size above 0
	// is sent: Bedrock-engine phantoms (scale 1) keep exactly the data they
	// had (HARD RULE 2: no change outside the Java mob mode).
	mobKinds["minecraft:phantom"] = &mobKind{extra: phantomData}
}

// phantomData is Phantom.ID_SIZE from the scale, when above 0.
func phantomData(_ *Session, d *mobData, e world.Entity, _ string) {
	s, ok := mobAs[interface{ Scale() float64 }](e)
	if !ok {
		return
	}
	if size := max(0, min(64, int32(math.Round((s.Scale()-1)/0.15)))); size > 0 {
		d.varInt(16, size)
	}
}
