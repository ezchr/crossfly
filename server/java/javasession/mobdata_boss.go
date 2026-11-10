package javasession

import (
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/java/protocol/text"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// Boss mobs (server/mob/component/boss): mob boss bars, the wither's head
// targets and invulnerable ticks, the ender dragon's phase, the warden's
// anger and the poses the Bedrock actor flags stand for.

// MobBossBarViewer returns the entity whose position decides whether the
// player is within a boss's hud_range.
func (s *Session) MobBossBarViewer() *world.EntityHandle { return s.ent }

// ShowMobBossBar shows the boss bar of e under e's UUID (one per boss, apart
// from the session's own SendBossBar). An empty name is the vanilla
// translation of the entity name. The ender dragon's bar is pink and plays
// the boss music with fog, as vanilla's EnderDragonFight; darken makes the
// sky dark (the wither).
func (s *Session) ShowMobBossBar(e world.Entity, name string, health float64, colour uint8, darken bool) {
	mobBossBars.Store(mobBossKey{s, e.H()}, struct{}{})
	id := e.H().Type().EncodeEntity()
	w := s.packet()
	w.UUID(e.H().UUID())
	w.VarInt(bossAdd)
	if name == "" {
		c := text.Component{Translate: "entity.minecraft." + strings.TrimPrefix(id, "minecraft:")}
		c.Write(w)
	} else {
		writeText(w, name)
	}
	w.Float32(float32(min(max(health, 0), 1)))
	col, flags := bossColour(colour), bit(darken, 0x01)
	if id == "minecraft:ender_dragon" {
		col, flags = 0, flags|0x02|0x04
	}
	w.VarInt(col)
	w.VarInt(0) // progress, no notches
	w.Byte(flags)
	s.queue(v777.ClientboundPlayBossEvent, w)
}

// UpdateMobBossBar changes the health shown on e's boss bar.
func (s *Session) UpdateMobBossBar(e world.Entity, health float64) {
	w := s.packet()
	w.UUID(e.H().UUID())
	w.VarInt(bossProgress)
	w.Float32(float32(min(max(health, 0), 1)))
	s.queue(v777.ClientboundPlayBossEvent, w)
}

// HideMobBossBar removes e's boss bar.
func (s *Session) HideMobBossBar(e world.Entity) {
	mobBossBars.Delete(mobBossKey{s, e.H()})
	s.removeMobBossBar(e.H())
}

// mobBossBars are the mob boss bars each session shows. A Java bar is not tied to its entity: when
// the boss leaves the session's view (HideEntity) its bar must be removed by hand.
var mobBossBars sync.Map // mobBossKey -> struct{}

type mobBossKey struct {
	s *Session
	h *world.EntityHandle
}

// hideMobBossBarOf removes the bar of a boss the session no longer sees, if it showed one.
func (s *Session) hideMobBossBarOf(h *world.EntityHandle) {
	if _, ok := mobBossBars.LoadAndDelete(mobBossKey{s, h}); ok {
		s.removeMobBossBar(h)
	}
}

// forgetMobBossBars drops the bars a closing session tracked.
func forgetMobBossBars(s *Session) {
	mobBossBars.Range(func(k, _ any) bool {
		if k.(mobBossKey).s == s {
			mobBossBars.Delete(k)
		}
		return true
	})
}

// removeMobBossBar sends the removal of the bar of the boss h.
func (s *Session) removeMobBossBar(h *world.EntityHandle) {
	w := s.packet()
	w.UUID(h.UUID())
	w.VarInt(bossRemove)
	s.queue(v777.ClientboundPlayBossEvent, w)
}

// mobExtraData is the extra Bedrock actor data of a mob (mob.Mob.ExtraActorData).
type mobExtraData interface{ ExtraActorData() map[uint32]any }

// mobExtraFlags is the extra Bedrock actor flags of a mob (mob.Mob.ExtraActorFlags).
type mobExtraFlags interface{ ExtraActorFlags() [3]uint64 }

// mobFlag reports whether e has the extra actor flag (protocol.EntityDataFlag*).
func mobFlag(e world.Entity, flag int) bool {
	f, ok := mobAs[mobExtraFlags](e)
	if !ok || flag/64 >= 3 {
		return false
	}
	return f.ExtraActorFlags()[flag/64]&(1<<(flag%64)) != 0
}

// Java poses (Pose ordinals) of the Bedrock actor flags of the warden and the breeze.
const (
	poseLongJumping = 6
	poseRoaring     = 11
	poseSniffing    = 12
	poseEmerging    = 13
	poseDigging     = 14
	poseShooting    = 16
)

// mobFlagPose is the Java pose a mob's extra actor flags stand for.
func mobFlagPose(e world.Entity) (int32, bool) {
	switch {
	case mobFlag(e, protocol.EntityDataFlagEmerging):
		return poseEmerging, true
	case mobFlag(e, protocol.EntityDataFlagDigging) && bedrockName(e) == "minecraft:warden":
		return poseDigging, true // the sniffer digs too, but Java shows that by its state
	case mobFlag(e, protocol.EntityDataFlagRoaring) && bedrockName(e) == "minecraft:warden":
		return poseRoaring, true
	case mobFlag(e, protocol.EntityDataFlagSniffing) && bedrockName(e) == "minecraft:warden":
		return poseSniffing, true
	case mobFlag(e, protocol.EntityDataFlagFacingTargetToRangeAttack) && bedrockName(e) == "minecraft:breeze":
		return poseShooting, true
	case mobFlag(e, protocol.EntityDataFlagJumpGoal) && bedrockName(e) == "minecraft:breeze":
		return poseLongJumping, true
	}
	return camelSittingPose(e)
}

// handleJavaID is the Java entity id of a handle this client sees, or 0.
func (s *Session) handleJavaID(h *world.EntityHandle) int32 {
	if h == nil {
		return 0
	}
	if h == s.ent {
		return selfEntityID
	}
	s.entMu.Lock()
	defer s.entMu.Unlock()
	return s.entityIDs[h]
}

// extraData returns e's extra actor data entry key.
func extraData(e world.Entity, key uint32) (any, bool) {
	x, ok := mobAs[mobExtraData](e)
	if !ok {
		return nil, false
	}
	v, ok := x.ExtraActorData()[key]
	return v, ok
}

func init() {
	set := func(name string, k mobKind) { mobKinds["minecraft:"+name] = &k }
	set("wither", mobKind{extra: func(s *Session, d *mobData, e world.Entity, _ string) {
		// WitherBoss.DATA_TARGET_A..C (16-18): the Java ids of the heads' targets, 0 for none;
		// DATA_ID_INV (19): the invulnerable ticks (the spawn charge, the blue glow).
		for i, key := range []uint32{protocol.EntityDataKeyTargetA, protocol.EntityDataKeyTargetB, protocol.EntityDataKeyTargetC} {
			var id int32
			if v, ok := extraData(e, key); ok {
				if h, ok := v.(*world.EntityHandle); ok {
					id = s.handleJavaID(h)
				}
			}
			d.varInt(byte(16+i), id)
		}
		var inv int32
		if v, ok := extraData(e, protocol.EntityDataKeyInvulnerableTicks); ok {
			inv, _ = v.(int32)
		}
		d.varInt(19, inv)
	}})
	set("ender_dragon", mobKind{extra: func(_ *Session, d *mobData, e world.Entity, _ string) {
		// EnderDragon.DATA_PHASE (16): sitting (scanning, 6) on the podium, else the holding
		// pattern (0); the client animates the wings and neck from it.
		phase := int32(0)
		if mobBool(e, mobSitting.Sitting) {
			phase = 6
		}
		if p, ok := mobAs[interface{ DragonPhase() int32 }](e); ok {
			// The Java mob mode dragon knows its real phase.
			phase = p.DragonPhase()
		}
		d.varInt(16, phase)
	}})
	set("warden", mobKind{extra: func(_ *Session, d *mobData, e world.Entity, _ string) {
		// Warden.CLIENT_ANGER_LEVEL (16): only drives the tendrils, so angry = 80.
		var anger int32
		if mobBool(e, mobAngry.Angry) {
			anger = 80
		}
		d.varInt(16, anger)
	}})
}

// bossEntityEvent plays the Java side of Bedrock actor events javaEntityEvents has no entry for.
// It reports whether it handled ev.
func (s *Session) bossEntityEvent(e world.Entity, id int32, ev uint8) bool {
	switch ev {
	case packet.ActorEventGuardianMiningFatigue:
		if id != selfEntityID {
			return true
		}
		w := s.packet()
		w.Byte(10) // GUARDIAN_ELDER_EFFECT: the ghost and its sound
		w.Float32(1)
		s.queue(v777.ClientboundPlayGameEvent, w)
		return true
	case packet.ActorEventVibrationDetected:
		if bedrockName(e) == "minecraft:warden" {
			w := s.packet()
			w.Int32(id)
			w.Byte(61) // TENDRILS_SHIVER
			s.queue(v777.ClientboundPlayEntityEvent, w)
		}
		return true
	}
	return false
}
