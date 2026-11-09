package javasession

import (
	"sync"
	"weak"

	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// Java animation states of the sniffer, the armadillo and the creaking, from the Bedrock actor
// flags of their goals (sniffer) and their enum actor properties (armadillo, creaking), and the
// happy ghast standing still. This init
// runs after the one in mobdata.go (files init in name order) and sets the extra data of the
// entries it made, which keeps their ageable data.

// EntityDataSerializers.SNIFFER_STATE and ARMADILLO_STATE (registration order, 26.3).
const (
	dataTypeSnifferState   = 35
	dataTypeArmadilloState = 36
)

// Sniffer.State ids.
const (
	snifferIdling    = 0
	snifferHappy     = 1
	snifferScenting  = 2 // the random air sniff of the Java brain: Bedrock has no goal for it
	snifferSniffing  = 3
	snifferSearching = 4
	snifferDigging   = 5
	snifferRising    = 6
)

// Armadillo.ArmadilloState ids.
const (
	armadilloIdle      = 0
	armadilloRolling   = 1
	armadilloScared    = 2
	armadilloUnrolling = 3
)

// Entity data indexes: Animal (AgeableMob 16 baby, 17 age locked) types start at 18, Monster
// types after Mob's 15 at 16.
const (
	snifferStateIndex   = 18 // Sniffer.DATA_STATE; DATA_DROP_SEED_AT_TICK (19) is server only
	armadilloStateIndex = 18 // Armadillo.ARMADILLO_STATE
	creakingCanMove     = 16 // Creaking.CAN_MOVE
	creakingActive      = 17 // Creaking.IS_ACTIVE
	creakingTearingDown = 18 // Creaking.IS_TEARING_DOWN; HOME_POS (19) is not sent
)

// HappyGhast.STAYS_STILL (Animal types start at 18; 18 is IS_LEASH_HOLDER).
const happyGhastStaysStill = 19

// armadilloPeekEvent is EntityEvent 64: the client plays the peek of a SCARED armadillo.
const armadilloPeekEvent = 64

func init() {
	mobKinds["minecraft:sniffer"].extra = func(_ *Session, d *mobData, e world.Entity, _ string) {
		if _, ok := mobAs[mobExtraFlags](e); !ok {
			return // not a definition mob (no goal flags)
		}
		d.entry(snifferStateIndex, dataTypeSnifferState)
		d.w.VarInt(snifferState(e))
	}
	mobKinds["minecraft:armadillo"].extra = func(s *Session, d *mobData, e world.Entity, _ string) {
		st := mobProperty(e, "minecraft:armadillo_state")
		if st == "" {
			return
		}
		// The state is the last entry written: armadilloSent reads it back from the end.
		d.entry(armadilloStateIndex, dataTypeArmadilloState)
		d.w.VarInt(s.armadilloState(e, st))
		s.armadilloPeek(e, st == "rolled_up_peeking")
	}
	mobKinds["minecraft:happy_ghast"].extra = func(_ *Session, d *mobData, e world.Entity, _ string) {
		if _, ok := mobAs[mobExtraFlags](e); !ok {
			return
		}
		// Bedrock minecraft:is_collidable comes with adult_immobile (a player on top): Java
		// staysStill, which makes the client collide with it and line its body up with an axis.
		d.boolean(happyGhastStaysStill, mobFlag(e, protocol.EntityDataFlagCollidable))
	}
	mobKinds["minecraft:creaking"].extra = func(_ *Session, d *mobData, e world.Entity, _ string) {
		st := mobProperty(e, "minecraft:creaking_state")
		if st == "" {
			return
		}
		// neutral: still, eyes dark, free to move. hostile_observed: eyes lit, frozen (a player
		// looks at it); hostile_unobserved: eyes lit, moving. twitching (its heart was broken) and
		// crumbling: the Java tear-down twitch, frozen; the eyes are off (Bedrock flickers them).
		move, active, tearing := true, false, false
		switch st {
		case "hostile_observed":
			move, active = false, true
		case "hostile_unobserved":
			active = true
		case "twitching", "crumbling":
			move, tearing = false, true
		}
		d.boolean(creakingCanMove, move)
		d.boolean(creakingActive, active)
		d.boolean(creakingTearingDown, tearing)
	}
}

// snifferState is the Java state of the sniffer goal flags (server/mob/goal/misc/animal,
// sniffer.json). Bedrock timer_flag_1 plays the long sniff before a search, the Java SNIFFING
// state (SCENTING is the shorter random sniff). The item is found while digging (timer_flag_3
// joins the digging flag) and the sniffer stands up (timer_flag_2) with it still on; Java digs,
// rises and only then feels happy, hence the order.
func snifferState(e world.Entity) int32 {
	switch {
	case mobFlag(e, protocol.EntityDataFlagDigging):
		return snifferDigging
	case mobFlag(e, protocol.EntityDataFlagTimerFlag2):
		return snifferRising
	case mobFlag(e, protocol.EntityDataFlagTimerFlag3):
		return snifferHappy
	case mobFlag(e, protocol.EntityDataFlagSearching):
		return snifferSearching
	case mobFlag(e, protocol.EntityDataFlagTimerFlag1):
		return snifferSniffing
	}
	return snifferIdling
}

// armadilloState is the Java state of the Bedrock minecraft:armadillo_state st.
//
// Java rolls up through ROLLING (the roll-up animation, 10 ticks) into SCARED, which starts the
// peek animation fast-forwarded to its end (a closed ball). rolled_up therefore is ROLLING when the
// armadillo was unrolled (or still rolling) for this client, else SCARED: back from a peek, from
// relaxing or unrolling (Java UNROLLING -> SCARED on a threat), or seen for the first time.
// rolled_up_with_threats turns timer_flag_1 on 2.5 s after the roll: from then on it is SCARED,
// so the client is SCARED when the first peek comes. The peek itself is SCARED plus the peek
// entity event (armadilloPeek); relaxing (no threat, about to unroll) stays SCARED.
func (s *Session) armadilloState(e world.Entity, st string) int32 {
	switch st {
	case "rolled_up":
		if mobFlag(e, protocol.EntityDataFlagTimerFlag1) {
			return armadilloScared
		}
		if prev, ok := s.armadilloSent(e); ok && (prev == armadilloIdle || prev == armadilloRolling) {
			return armadilloRolling
		}
		return armadilloScared
	case "rolled_up_peeking", "rolled_up_relaxing":
		return armadilloScared
	case "rolled_up_unrolling":
		return armadilloUnrolling
	}
	return armadilloIdle
}

// armadilloSent is the ArmadilloState last sent to s for e: the end of the mob data viewMobData
// last sent for it (index, serializer and the state, each one byte).
func (s *Session) armadilloSent(e world.Entity) (int32, bool) {
	id := s.handleJavaID(e.H())
	if id == 0 {
		return 0, false
	}
	s.entMu.Lock()
	b := s.mobData[id]
	s.entMu.Unlock()
	n := len(b)
	if n < 3 || b[n-3] != armadilloStateIndex || b[n-2] != dataTypeArmadilloState {
		return 0, false
	}
	return int32(b[n-1]), true
}

// armadilloPeeks are the armadillos each session saw peeking, so that a peek sends its entity
// event once although the mob data is written again during it (timer_flag_1 goes off a moment
// after the state turns rolled_up_peeking). The keys are weak: an entry left behind by a session
// that closed or an armadillo that went away while peeking holds neither, and is dropped the next
// time a peek starts.
var armadilloPeeks sync.Map // armadilloPeekKey -> struct{}

type armadilloPeekKey struct {
	s weak.Pointer[Session]
	h weak.Pointer[world.EntityHandle]
}

// armadilloPeek sends the peek entity event when e starts peeking for s.
func (s *Session) armadilloPeek(e world.Entity, peeking bool) {
	key := armadilloPeekKey{weak.Make(s), weak.Make(e.H())}
	if !peeking {
		armadilloPeeks.Delete(key)
		return
	}
	if _, seen := armadilloPeeks.LoadOrStore(key, struct{}{}); seen {
		return
	}
	armadilloPeeks.Range(func(k, _ any) bool {
		if k := k.(armadilloPeekKey); k.s.Value() == nil || k.h.Value() == nil {
			armadilloPeeks.Delete(k)
		}
		return true
	})
	id := s.handleJavaID(e.H())
	if id == 0 {
		return
	}
	w := s.packet()
	w.Int32(id)
	w.Byte(armadilloPeekEvent)
	s.queue(v777.ClientboundPlayEntityEvent, w)
}
