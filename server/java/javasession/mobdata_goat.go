package javasession

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// This init runs after the one in mobdata.go (files initialise in name
// order) and adds the goat's own data to its ageable entry.
func init() {
	if k, ok := mobKinds["minecraft:goat"]; ok {
		k.extra = goatData
	}
}

// goatData is Goat.DATA_IS_SCREAMING_GOAT (18, the Bedrock variant 1),
// DATA_HAS_LEFT_HORN (19) and DATA_HAS_RIGHT_HORN (20), from the Bedrock
// horn count (actor data 122, server/mob/goal/misc goat_horns.go): the
// Bedrock client draws the left horn above 0 horns and the right one above
// 1. A goat without the data has both, as by default.
func goatData(_ *Session, d *mobData, e world.Entity, _ string) {
	horns := int32(2)
	if v, ok := extraData(e, protocol.EntityDataKeyGoatHornCount); ok {
		if n, ok := v.(int32); ok {
			horns = n
		}
	}
	screaming := false
	if v, ok := mobAs[mobVariant](e); ok {
		screaming = v.Variant() == 1
	}
	d.boolean(18, screaming)
	d.boolean(19, horns > 0)
	d.boolean(20, horns > 1)
}
