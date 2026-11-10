package javasession

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// Java data for the goal states server/mob/goal/misc shows through extra
// Bedrock actor flags (mobFlag in mobdata_boss.go): fox crouching/interested/sleeping, panda
// sneezing/rolling/sitting/on its back, vex charging, polar bear standing.
// This init runs after the one in mobdata.go (files init in name order), so
// it replaces the fox and panda extras with versions that also send the
// flags.

func init() {
	// Fox.DATA_FLAGS_ID 19: 0x01 sitting, 0x04 crouching, 0x08 interested, 0x20 sleeping.
	mobKinds["minecraft:fox"].extra = func(_ *Session, d *mobData, e world.Entity, _ string) {
		if v, ok := mobAs[mobVariant](e); ok {
			d.varInt(18, v.Variant()) // 0 red, 1 snow
		}
		crouch := mobBool(e, mobSneaking.Sneaking) || mobFlag(e, protocol.EntityDataFlagStalking)
		d.byteVal(19, bit(mobBool(e, mobSitting.Sitting), 0x01)|bit(crouch, 0x04)|
			bit(mobFlag(e, protocol.EntityDataFlagInterested), 0x08)|bit(mobFlag(e, protocol.EntityDataFlagSleeping), 0x20))
	}
	// Panda.DATA_ID_FLAGS 23: 0x02 sneezing, 0x04 rolling, 0x08 sitting, 0x10 on its back.
	mobKinds["minecraft:panda"].extra = func(_ *Session, d *mobData, e world.Entity, _ string) {
		if v, ok := mobAs[mobVariant](e); ok {
			// Bedrock sends only the gene shown: both Java genes get it, so recessive ones show too.
			g := byte(v.Variant())
			d.byteVal(21, g)
			d.byteVal(22, g)
		}
		if _, ok := mobAs[mobExtraFlags](e); !ok {
			return // not a definition mob (no goal flags)
		}
		d.byteVal(23, bit(mobFlag(e, protocol.EntityDataFlagSneezing), 0x02)|bit(mobFlag(e, protocol.EntityDataFlagRolling), 0x04)|
			bit(mobBool(e, mobSitting.Sitting), 0x08)|bit(mobFlag(e, protocol.EntityDataFlagLayingDown), 0x10))
	}
	// Vex.DATA_FLAGS_ID 16: 0x01 charging.
	mobKinds["minecraft:vex"].extra = func(_ *Session, d *mobData, e world.Entity, _ string) {
		if _, ok := mobAs[mobExtraFlags](e); !ok {
			return
		}
		d.byteVal(16, bit(mobFlag(e, protocol.EntityDataFlagCharging), 0x01))
	}
	// PolarBear.DATA_STANDING_ID 18 (AgeableMob has 16 baby and 17 AGE_LOCKED).
	mobKinds["minecraft:polar_bear"].extra = func(_ *Session, d *mobData, e world.Entity, _ string) {
		if _, ok := mobAs[mobExtraFlags](e); !ok {
			return
		}
		d.boolean(18, mobFlag(e, protocol.EntityDataFlagStanding))
	}
}
