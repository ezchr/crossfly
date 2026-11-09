package session

import (
	"strings"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// Mob boss bars (minecraft:boss, BDS BossComponent). Package
// server/mob/component/boss drives them through these methods; each bar is
// keyed by the boss entity, so a player sees one bar per boss in range,
// apart from the session's own SendBossBar.

// MobBossBarViewer returns the entity whose position decides whether the
// player is within a boss's hud_range.
func (s *Session) MobBossBarViewer() *world.EntityHandle { return s.ent }

// ShowMobBossBar shows the boss bar of e (BossEventShow). An empty name is
// the entity's localisation key ("entity.wither.name"), as BDS sends it.
func (s *Session) ShowMobBossBar(e world.Entity, name string, health float64, colour uint8, darken bool) {
	id, ok := s.mobBossID(e)
	if !ok {
		return
	}
	if name == "" {
		name = "entity." + strings.TrimPrefix(e.H().Type().EncodeEntity(), "minecraft:") + ".name"
	}
	// darken (should_darken_sky) has no field in this protocol's BossEvent.
	_ = darken
	s.writePacket(&packet.BossEvent{
		BossEntityUniqueID: id,
		EventType:          packet.BossEventShow,
		BossBarTitle:       name,
		HealthPercentage:   float32(min(max(health, 0), 1)),
		Colour:             colour,
	})
}

// UpdateMobBossBar changes the health shown on e's boss bar.
func (s *Session) UpdateMobBossBar(e world.Entity, health float64) {
	id, ok := s.mobBossID(e)
	if !ok {
		return
	}
	s.writePacket(&packet.BossEvent{
		BossEntityUniqueID: id,
		EventType:          packet.BossEventHealthPercentage,
		HealthPercentage:   float32(min(max(health, 0), 1)),
	})
}

// HideMobBossBar removes e's boss bar.
func (s *Session) HideMobBossBar(e world.Entity) {
	id, ok := s.mobBossID(e)
	if !ok {
		return
	}
	s.writePacket(&packet.BossEvent{BossEntityUniqueID: id, EventType: packet.BossEventHide})
}

// mobBossID is the unique id of a boss the client knows.
func (s *Session) mobBossID(e world.Entity) (int64, bool) {
	s.entityMutex.RLock()
	id, ok := s.entityRuntimeIDs[e.H()]
	s.entityMutex.RUnlock()
	return int64(id), ok
}
