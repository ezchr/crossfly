package javasession

import (
	"math"

	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/java/protocol/version"
)

// A Java client draws as many rows of hearts as the player's minecraft:max_health attribute holds
// and clamps the health set_health gives it to that attribute (LivingEntity.setHealth). Without
// it a player with more than 20 health (a raised maximum, health boost) was drawn with 10 hearts,
// full at 20. SendHealth sends the attribute before the health whenever the maximum changes, and
// newPlayerEntity makes it go again for each new player entity of the client.

// attributeMaxHealth is minecraft:max_health in the minecraft:attribute registry.
var attributeMaxHealth = v777.BuiltinID("minecraft:attribute", "minecraft:max_health")

// javaMaxHealth is maxHealth within the range of the Java attribute (1-1024).
func javaMaxHealth(maxHealth float64) float64 {
	if math.IsNaN(maxHealth) {
		return 20
	}
	return min(max(maxHealth, 1), 1024)
}

// writeMaxHealth sends the player's maximum health as the base value of minecraft:max_health.
func (s *Session) writeMaxHealth(maxHealth float64) {
	attr := attributeMaxHealth
	if l := s.legacy(); l != nil {
		if attr = version.Map(l.attribute, attr); attr < 0 {
			return
		}
	}
	w := s.packet()
	w.VarInt(selfEntityID)
	w.VarInt(1)
	w.VarInt(attr)
	w.Float64(javaMaxHealth(maxHealth))
	w.VarInt(0) // modifiers
	s.queue(v777.ClientboundPlayUpdateAttributes, w)
}

// newPlayerEntity is called when the client makes a new player entity (a respawn, a world change):
// the attributes and the absorption are sent again with the next health and the next tick.
func (s *Session) newPlayerEntity() {
	s.enchAttrs.respawned()
	s.vitalsMu.Lock()
	s.vitals.maxHealthSent, s.vitals.absorptionSent = false, false
	s.vitalsMu.Unlock()
}

// SendArrowHitPlayer plays the sound a Java client makes when an arrow the player shot hits another
// player: game event 6 (ARROW_HIT_PLAYER), which vanilla sends the shooter from
// AbstractArrow.onHitEntity.
func (s *Session) SendArrowHitPlayer() {
	w := s.packet()
	w.Byte(gameEventArrowHitPlayer)
	w.Float32(0)
	s.queue(v777.ClientboundPlayGameEvent, w)
}

// gameEventArrowHitPlayer is ClientboundGameEventPacket.ARROW_HIT_PLAYER.
const gameEventArrowHitPlayer = 6
