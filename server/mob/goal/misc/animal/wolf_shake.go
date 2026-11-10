//go:build crossfly_full

package animal

import (
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/kit"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// A wolf shaking itself dry is hardcoded in BDS (Wolf::newServerAiStep and
// Wolf::postNormalTick); wolf.json has no component for it, so it is gated
// by identifier.
const wolfID = "minecraft:wolf"

func init() {
	mob.RegisterSystem(mob.System{Name: "animal/wolf_shake_start", Stage: mob.StageGoals, Order: 300, Tick: wolfShakeStart})
	mob.RegisterSystem(mob.System{Name: "animal/wolf_shake", Stage: mob.StageTravel, Order: 31, Tick: wolfShakeTick})
}

// wolfShake is the wolf's mIsWet, mIsShaking and mShakeAnim.
type wolfShake struct {
	wet, shaking bool
	anim         float64
}

type wolfShakeKey struct{}

func wolfShakeOf(m *mob.Mob) *wolfShake {
	if v, ok := m.Data(wolfShakeKey{}); ok {
		return v.(*wolfShake)
	}
	s := &wolfShake{}
	m.SetData(wolfShakeKey{}, s)
	return s
}

// wolfShakeStart is Wolf::newServerAiStep: a wet wolf that is not shaking,
// has nowhere to walk and stands on the ground starts shaking and tells the
// clients (ActorEvent 8, ShakeWetness; Java entity event 8), which animate
// it and splash the water off.
func wolfShakeStart(m *mob.Mob) {
	if m.Identifier() != wolfID {
		return
	}
	s := wolfShakeOf(m)
	if !s.wet || s.shaking || s.anim != 0 || !m.OnGround() {
		return
	}
	if nav := m.Navigation(); nav != nil && !nav.Done() {
		return
	}
	s.shaking, s.anim = true, 0
	kit.Show(m, kit.ActorEvent(packet.ActorEventShakeWetness))
}

// wolfShakeTick is Wolf::postNormalTick: water or rain makes the wolf wet
// and stops a shake; a shake starts with the shake sound (Java only: the
// Bedrock client plays it itself on ActorEvent 8) and the entity_act game
// event and lasts 40 ticks (the animation runs 0..2 in steps of 0.05), after
// which the wolf is dry. A shake cut short by water still runs its
// animation out.
func wolfShakeTick(m *mob.Mob) {
	if m.Identifier() != wolfID {
		return
	}
	s := wolfShakeOf(m)
	if sense.InWaterOrRain(m.Tx(), m) {
		s.wet, s.shaking = true, false
	}
	if s.shaking {
		if s.anim == 0 {
			m.Tx().PlaySound(m.Position(), mob.Sound{Event: "shake", Identifier: wolfID, Baby: m.Baby(), ClientSide: true})
			m.EmitVibration("entity_act")
		}
	} else if s.anim <= 0 {
		return
	}
	prev := s.anim
	s.anim += 0.05
	if prev >= 2 {
		s.wet, s.shaking, s.anim = false, false, 0
	}
}
