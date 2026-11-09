//go:build crossfly_full

package locomotion

import (
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// slimeState is what Slime keeps between ticks: whether it was on the
// ground (SlimeWasOnGroundPreNormalTick) and the tick it died at.
type slimeState struct {
	ground, known bool
	dead          bool
	deathTick     int64
}

type slimeKey struct{}

func slimeStateOf(m *mob.Mob) *slimeState {
	if v, ok := m.Data(slimeKey{}); ok {
		return v.(*slimeState)
	}
	s := &slimeState{}
	m.SetData(slimeKey{}, s)
	return s
}

// Slime client events (actor data 0x18, CLIENT_EVENT), which make the
// client run Slime::justLanded / justJumped: the landing particles and the
// squash and stretch of the model.
const (
	slimeEventLanded int8 = 1
	slimeEventJumped int8 = 2
)

// slimeTick is Slime::postNormalTick, run after travel: a slime that just
// landed (Slime::justLanded) sends client event 1 and, at size 3 or more,
// plays squish.big; one that just left the ground (Slime::justJumped,
// playJumpSound) sends client event 2 and plays squish.big, or squish.small
// at size 1. A dead slime splits when its corpse is removed (slimeSplit).
func slimeTick(m *mob.Mob) {
	if !isSlime(m) {
		return
	}
	s := slimeStateOf(m)
	if m.Dead() {
		// The mob package removes a corpse at the start of the 20th tick
		// after the death: the tick before is the last one it runs.
		if s.dead && m.Ticks()-s.deathTick >= 19 {
			s.dead = false
			slimeSplit(m)
		}
		return
	}
	g := m.OnGround()
	if s.known && g != s.ground {
		size := m.Meta().Variant
		if g {
			m.SetActorData(protocol.EntityDataKeyClientEvent, slimeEventLanded)
			if size > 2 {
				m.PlaySound("squish.big")
			}
		} else {
			m.SetActorData(protocol.EntityDataKeyClientEvent, slimeEventJumped)
			if size > 0 {
				m.PlaySound(squishSound(size))
			}
		}
	}
	s.ground, s.known = g, true
}

// squishSound is the squish.small sound of size 1 slimes, squish.big of
// larger ones (LevelSoundEvent 23 / 22).
func squishSound(size int32) string {
	if size <= 1 {
		return "squish.small"
	}
	return "squish.big"
}

// slimeDied records the death tick for slimeTick.
func slimeDied(m *mob.Mob, _ world.DamageSource) {
	if !isSlime(m) {
		return
	}
	s := slimeStateOf(m)
	s.dead, s.deathTick = true, m.Ticks()
}
