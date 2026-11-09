package javasession

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// Entity data of water mobs set by server/mob/goal/locomotion and
// server/mob/component/locomotion. This init runs after the one in
// mobdata.go (files initialise in name order) and replaces or extends those
// entries.
func init() {
	for _, n := range []string{"guardian", "elder_guardian"} {
		mobKinds["minecraft:"+n] = &mobKind{extra: guardianData}
	}
	// Pufferfish.PUFF_STATE (17): the Bedrock variant 0..2 is the same puff
	// state.
	mobKinds["minecraft:pufferfish"] = &mobKind{extra: intVariant(17)}
	if k, ok := mobKinds["minecraft:axolotl"]; ok {
		variant := k.extra
		k.extra = func(s *Session, d *mobData, e world.Entity, bedrock string) {
			if variant != nil {
				variant(s, d, e, bedrock)
			}
			// Axolotl.DATA_PLAYING_DEAD, after the variant (18); only for
			// entities that carry actor flags (vanilla mobs).
			if _, ok := mobAs[interface{ ExtraActorFlags() [3]uint64 }](e); ok {
				d.boolean(19, mobActorFlag(e, protocol.EntityDataFlagPlayingDead))
			}
		}
	}
}

// mobActorFlag reports whether an extra Bedrock actor flag (mob.Mob
// ExtraActorFlags) is set on e.
func mobActorFlag(e world.Entity, flag uint8) bool {
	f, ok := mobAs[interface{ ExtraActorFlags() [3]uint64 }](e)
	if !ok {
		return false
	}
	w := f.ExtraActorFlags()
	if int(flag)/64 >= len(w) {
		return false
	}
	return w[flag/64]&(1<<(flag%64)) != 0
}

// guardianData is Guardian.DATA_ID_MOVING (16, spikes in while swimming) and
// DATA_ID_ATTACK_TARGET (17, the beam target, 0 for none).
func guardianData(s *Session, d *mobData, e world.Entity, _ string) {
	d.boolean(16, mobActorFlag(e, protocol.EntityDataFlagMoving))
	var target int32
	if t, ok := mobAs[interface{ TargetEntity() *world.EntityHandle }](e); ok {
		if h := t.TargetEntity(); h != nil {
			if h == s.ent {
				target = selfEntityID
			} else {
				s.entMu.Lock()
				target = s.entityIDs[h]
				s.entMu.Unlock()
			}
		}
	}
	d.varInt(17, target)
}
