package javasession

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// Java data of pets and mounts whose state server/mob shows through extra
// Bedrock actor flags (the Java mob mode in server/mob/jpets sets the same
// flags as the Bedrock engine, so both clients animate the same state):
// a begging wolf's head tilt, a cat lying on a bed or getting ready to,
// a camel sitting or dashing, a nautilus dashing, a horse eating grass or
// rearing. Like the panda and polar bear (mobdata_misc.go), the entries are only sent for entities that carry
// actor flags (mobs of package mob). This init runs after mobdata.go and
// mobdata_misc.go (files initialise in name order) and wraps their extras.

const (
	poseSitting = 10 // Pose.SITTING
)

func init() {
	// Wolf.DATA_INTERESTED_ID (20): BegGoal tilts the head.
	wrapExtra("minecraft:wolf", func(d *mobData, e world.Entity) {
		d.boolean(20, mobFlag(e, protocol.EntityDataFlagInterested))
	})
	// Cat.IS_LYING (21) and RELAX_STATE_ONE (22): CatRelaxOnOwnerGoal and
	// CatLieOnBlockGoal; relax state one is the cat curled up next to its
	// sleeping owner before it lies down (the resting flag).
	wrapExtra("minecraft:cat", func(d *mobData, e world.Entity) {
		d.boolean(21, mobFlag(e, protocol.EntityDataFlagLayingDown))
		d.boolean(22, mobFlag(e, protocol.EntityDataFlagResting))
	})
	// Camel.DASH (19) and LAST_POSE_CHANGE_TICK (20): negative while the camel
	// sits (isCamelSitting). -1 and 0 are ticks long past, so the client shows
	// the pose without replaying the sit down or stand up animation.
	for _, n := range []string{"minecraft:camel", "minecraft:camel_husk"} {
		wrapExtra(n, func(d *mobData, e world.Entity) {
			d.boolean(19, mobFlag(e, protocol.EntityDataFlagDash))
			tick := int64(0)
			if mobBool(e, mobSitting.Sitting) {
				tick = -1
			}
			d.long(20, tick)
		})
	}
	// AbstractNautilus.DASH (20), after TamableAnimal's 18 and 19.
	for _, n := range []string{"minecraft:nautilus", "minecraft:zombie_nautilus"} {
		wrapExtra(n, func(d *mobData, e world.Entity) {
			d.boolean(20, mobFlag(e, protocol.EntityDataFlagDash))
		})
	}
}

// wrapExtra adds f after the existing extra data of a mob kind, for
// entities with extra actor flags only.
func wrapExtra(name string, f func(d *mobData, e world.Entity)) {
	k, ok := mobKinds[name]
	if !ok {
		return
	}
	prev := k.extra
	k.extra = func(s *Session, d *mobData, e world.Entity, bedrock string) {
		if prev != nil {
			prev(s, d, e, bedrock)
		}
		if _, ok := mobAs[mobExtraFlags](e); ok {
			f(d, e)
		}
	}
}

// horseFlags is AbstractHorse.DATA_ID_FLAGS (18): 0x02 tame, 0x10 eating
// grass, 0x20 rearing (standing). (0x08 bred and 0x40 mouth open have no
// Bedrock flag and change nothing the Java client draws for long.)
func horseFlags(e world.Entity) byte {
	return bit(mobBool(e, mobTamed.Tamed), 0x02) | bit(mobFlag(e, protocol.EntityDataFlagEating), 0x10) |
		bit(mobFlag(e, protocol.EntityDataFlagStanding), 0x20)
}

// camelSittingPose is Pose.SITTING for a sitting camel.
func camelSittingPose(e world.Entity) (int32, bool) {
	switch bedrockName(e) {
	case "minecraft:camel", "minecraft:camel_husk":
		if mobBool(e, mobSitting.Sitting) {
			if _, ok := mobAs[mobExtraFlags](e); ok {
				return poseSitting, true
			}
		}
	}
	return 0, false
}
