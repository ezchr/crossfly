package javasession

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/java/protocol/version"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// damageGeneric is minecraft:generic in the damage_type registry.
var damageGeneric = v777.RegistryID("minecraft:damage_type", "minecraft:generic")

// LivingEntity.DATA_HEALTH_ID, a FLOAT.
const dataLivingHealth = 9

// Java entity type ids (minecraft:entity_type protocol ids in Mojang's registries.json).
const entityTypePlayer = 159

// entityID returns the Java entity id this client knows e by.
func (s *Session) entityID(e world.Entity) (int32, bool) {
	if e.H() == s.ent {
		return selfEntityID, true
	}
	s.entMu.Lock()
	defer s.entMu.Unlock()
	id, ok := s.entityIDs[e.H()]
	return id, ok
}

func (s *Session) addEntityID(e world.Entity) int32 {
	s.entMu.Lock()
	defer s.entMu.Unlock()
	if id, ok := s.entityIDs[e.H()]; ok {
		return id
	}
	s.nextEntityID++
	if s.nextEntityID == selfEntityID {
		s.nextEntityID++
	}
	s.entityIDs[e.H()] = s.nextEntityID
	return s.nextEntityID
}

func (s *Session) removeEntityID(e world.Entity) (int32, bool) {
	s.entMu.Lock()
	defer s.entMu.Unlock()
	id, ok := s.entityIDs[e.H()]
	delete(s.entityIDs, e.H())
	delete(s.tracks, id)
	delete(s.textModelIDs, id)
	return id, ok
}

// ownerID is the Java id of the entity that cast or shot e (a fishing hook's player), if this
// client knows it.
func (s *Session) ownerID(e world.Entity) (int32, bool) {
	var owner *world.EntityHandle
	if o, ok := mobAs[projectileOwner](e); ok {
		owner = o.Owner()
	} else if o, ok := e.(interface{ ProjectileOwner() *world.EntityHandle }); ok {
		owner = o.ProjectileOwner()
	}
	if owner == nil {
		return 0, false
	}
	if owner == s.ent {
		return selfEntityID, true
	}
	s.entMu.Lock()
	defer s.entMu.Unlock()
	id, ok := s.entityIDs[owner]
	return id, ok
}

// entityYaws are the Java body and head yaw of e: a mob's own body and head rotation when it has
// them (BodyYaw, HeadYaw), else its yaw for both. Bedrock boats face 90 degrees off Java's.
func entityYaws(e world.Entity, rot cube.Rotation) (body, head float64) {
	body, head = rot.Yaw(), rot.Yaw()
	if b, ok := mobAs[mobBodyYaw](e); ok {
		body = b.BodyYaw()
	}
	if h, ok := mobAs[mobHeadYaw](e); ok {
		head = h.HeadYaw()
	}
	if isBoat(e) {
		body, head = body-90, head-90
	}
	return body, head
}

// tabName is a name the Java client accepts in the tab list (at most 16 characters).
func tabName(n string) string {
	r := []rune(n)
	if len(r) > 16 {
		r = r[:16]
	}
	return string(r)
}

// ViewEntity shows an entity that came into view.
func (s *Session) ViewEntity(e world.Entity) {
	if e.H() == s.ent {
		return
	}
	s.entMu.Lock()
	hidden := s.hiddenEnts[e.H()]
	s.entMu.Unlock()
	if hidden {
		return
	}
	p, ok := e.(*player.Player)
	if !ok {
		s.viewOtherEntity(e)
		return
	}
	if s.deferForSkin(p) {
		return
	}
	id := s.addEntityID(e)
	u := p.UUID()

	// Tab list entry first: the client needs the profile to spawn a player entity.
	s.showTabFor(p)

	pos, rot := p.Position(), p.Rotation()
	w := s.packet()
	w.VarInt(id)
	w.UUID(u)
	w.VarInt(s.ver.Builtin("minecraft:entity_type", entityTypePlayer))
	w.Float64(pos[0])
	w.Float64(pos[1])
	w.Float64(pos[2])
	w.LpVec3(0, 0, 0)
	w.Angle(float32(rot.Pitch()))
	w.Angle(float32(rot.Yaw()))
	w.Angle(float32(rot.Yaw())) // head
	w.VarInt(0)
	s.queue(v777.ClientboundPlayAddEntity, w)
	s.setTrack(id, pos, rot)
	s.viewPlayerNameTag(p)
	s.sendSkinParts(id, skinPartsOf(u)) // else the outer skin layer is not drawn: a flat skin
	// Its state as it is now (invisible, sneaking, on fire...): a player that came into view
	// already invisible was drawn with its skin until its state next changed.
	s.ViewEntityState(p)
	s.noteRiding(e)
}

// StopShowingEntity hides e from this player for good (player.HideEntity): it goes from their
// screen and is not shown again until StartShowingEntity.
func (s *Session) StopShowingEntity(e world.Entity) {
	if e.H() == s.ent {
		return
	}
	s.entMu.Lock()
	if s.hiddenEnts == nil {
		s.hiddenEnts = map[*world.EntityHandle]bool{}
	}
	s.hiddenEnts[e.H()] = true
	s.entMu.Unlock()
	s.HideEntity(e)
}

// StartShowingEntity shows e again after StopShowingEntity.
func (s *Session) StartShowingEntity(e world.Entity) {
	s.entMu.Lock()
	hidden := s.hiddenEnts[e.H()]
	delete(s.hiddenEnts, e.H())
	s.entMu.Unlock()
	if hidden {
		s.ViewEntity(e)
	}
}

// HideEntity removes an entity that left view.
func (s *Session) HideEntity(e world.Entity) {
	if e.H() == s.ent {
		return
	}
	s.entMu.Lock()
	delete(s.deferred, e.H()) // a deferred spawn that has not happened yet is called off
	s.entMu.Unlock()
	id, ok := s.removeEntityID(e)
	if !ok {
		return
	}
	s.hideModel(id)
	s.hideNameDisplay(id)
	s.forgetCustomName(id)
	w := s.packet()
	w.VarInt(1)
	w.VarInt(id)
	s.queue(v777.ClientboundPlayRemoveEntities, w)
	s.forgetEntityText(e)
	s.forgetRiding(e.H())
	s.hideMobBossBarOf(e.H())
	if p, ok := e.(*player.Player); ok {
		s.hideTabFor(p.UUID()) // online players stay listed
	}
}

// EntityUpdateInterval is how many ticks apart vanilla sends the movement of the Java entity type e
// is shown as (EntityType.updateInterval): 3 for most mobs, 2 for allays. The client spreads each
// move over as many ticks (SteppedInterpolationHandler), so movement sent more often only queues up
// on its side. Entities drawn with displays (a text display stand-in, or a model of item displays)
// are sent every tick, as displays are.
func (s *Session) EntityUpdateInterval(e world.Entity) int {
	name := bedrockName(e)
	entityModelsMu.RLock()
	_, model := entityModels[name]
	entityModelsMu.RUnlock()
	if model {
		return 1
	}
	typ, _ := javaTypeName(e)
	switch typ {
	case "minecraft:text_display", "minecraft:item_display", "minecraft:block_display":
		return 1
	case "minecraft:allay", "minecraft:player", "minecraft:mannequin", "minecraft:evoker_fangs":
		return 2
	}
	return 3
}

// ViewEntityMovement moves another entity to an absolute position.
func (s *Session) ViewEntityMovement(e world.Entity, pos mgl64.Vec3, rot cube.Rotation, onGround bool) {
	if e.H() == s.ent || s.drivenBySelf(e) {
		return // the client moves itself (and the vehicle it drives)
	}
	s.positionSync(e, pos, rot, onGround)
}

// ViewEntityDisplacement is a server-made move (knockback correction, pushing).
func (s *Session) ViewEntityDisplacement(e world.Entity, pos mgl64.Vec3, rot cube.Rotation, onGround bool) {
	if e.H() == s.ent {
		s.teleport(pos[0], pos[1], pos[2], float32(rot.Yaw()), float32(rot.Pitch()))
		return
	}
	s.positionSync(e, pos, rot, onGround)
}

// ViewEntityTeleport moves an entity instantly.
func (s *Session) ViewEntityTeleport(e world.Entity, pos mgl64.Vec3) {
	rot := e.Rotation()
	if e.H() == s.ent {
		s.teleport(pos[0], pos[1], pos[2], float32(rot.Yaw()), float32(rot.Pitch()))
		return
	}
	s.positionSync(e, pos, rot, false)
}

// track is what the client last knew about an entity's position: the base its 26.3 delta packets
// are relative to (VecDeltaCodec), the last rotation bytes and when it last got a full sync.
type track struct {
	base           mgl64.Vec3
	yaw, pitch, hd byte
	sinceSync      int
	synced         bool
}

func encodeDelta(v float64) int64 { return int64(math.Floor(v*4096 + 0.5)) }

// positionSync moves another entity: a delta move (move_entity_pos/_pos_rot/_rot) when it fits,
// else a full entity_position_sync, which vanilla also sends every 60 moves to undo drift.
func (s *Session) positionSync(e world.Entity, pos mgl64.Vec3, rot cube.Rotation, onGround bool) {
	id, ok := s.entityID(e)
	if !ok {
		return
	}
	body, head := entityYaws(e, rot)
	s.syncEntityHead(id, pos, cube.Rotation{body, rot.Pitch()}, head, onGround)
	s.moveModel(id, pos, rot, onGround) // the parts of its model, if it has one (entitymodel.go)
}

// syncEntity is positionSync for the entity the client knows by id, its head turned as its body.
func (s *Session) syncEntity(id int32, pos mgl64.Vec3, rot cube.Rotation, onGround bool) {
	s.syncEntityHead(id, pos, rot, rot.Yaw(), onGround)
}

// syncEntityHead is syncEntity with the head yaw apart from the body's (rot): rotate_head carries
// the head, the move packets the body.
func (s *Session) syncEntityHead(id int32, pos mgl64.Vec3, rot cube.Rotation, headYaw float64, onGround bool) {
	yaw, pitch, head := angleByte(rot.Yaw()), angleByte(rot.Pitch()), angleByte(headYaw)
	s.entMu.Lock()
	t := s.tracks[id]
	if t == nil {
		t = &track{}
		s.tracks[id] = t
	}
	var d [3]int64
	fits := t.synced && t.sinceSync < 60
	for i := range d {
		d[i] = encodeDelta(pos[i]) - encodeDelta(t.base[i])
		if d[i] < math.MinInt16 || d[i] > math.MaxInt16 {
			fits = false
		}
	}
	moved := d[0] != 0 || d[1] != 0 || d[2] != 0
	turned := yaw != t.yaw || pitch != t.pitch
	headTurned := head != t.hd
	if fits {
		t.sinceSync++
		for i := range d {
			if d[i] != 0 { // the client's VecDeltaCodec.decode
				t.base[i] = float64(encodeDelta(t.base[i])+d[i]) / 4096
			}
		}
	} else {
		t.base, t.sinceSync, t.synced = pos, 0, true
	}
	t.yaw, t.pitch, t.hd = yaw, pitch, head
	s.entMu.Unlock()

	if !s.ver.Native() {
		s.positionSync262(id, fits, moved, turned, headTurned, d, yaw, pitch, head, pos, rot, onGround)
		return
	}
	if fits {
		props := int32(0)
		if onGround {
			props = 1 // bit 0 on ground, step count 0 (a linear delta)
		}
		switch {
		case moved && turned:
			w := s.packet()
			w.VarInt(id)
			w.VarInt(props)
			w.Int16(int16(d[0]))
			w.Int16(int16(d[1]))
			w.Int16(int16(d[2]))
			w.Byte(yaw)
			w.Byte(pitch)
			s.queue(v777.ClientboundPlayMoveEntityPosRot, w)
		case moved:
			w := s.packet()
			w.VarInt(id)
			w.VarInt(props)
			w.Int16(int16(d[0]))
			w.Int16(int16(d[1]))
			w.Int16(int16(d[2]))
			s.queue(v777.ClientboundPlayMoveEntityPos, w)
		case turned:
			w := s.packet()
			w.VarInt(id)
			w.Bool(onGround)
			w.Byte(yaw)
			w.Byte(pitch)
			s.queue(v777.ClientboundPlayMoveEntityRot, w)
		}
		if headTurned {
			w := s.packet()
			w.VarInt(id)
			w.Byte(head)
			s.queue(v777.ClientboundPlayRotateHead, w)
		}
		return
	}
	w := s.packet()
	w.VarInt(id)
	w.VarInt(0) // PositionPath.LINEAR
	w.Float64(pos[0])
	w.Float64(pos[1])
	w.Float64(pos[2])
	w.Float32(float32(rot.Yaw()))
	w.Float32(float32(rot.Pitch()))
	w.Bool(onGround)
	s.queue(v777.ClientboundPlayEntityPositionSync, w)
	w = s.packet()
	w.VarInt(id)
	w.Byte(head)
	s.queue(v777.ClientboundPlayRotateHead, w)
}

// positionSync262 writes positionSync's packets in the 26.2 layouts: delta moves are three shorts
// with onGround as a trailing bool (move_entity_rot: yaw, pitch, onGround), and
// entity_position_sync is position, delta movement (unused by the client), yaw, pitch, onGround.
// The delta codec is the same as 26.3's.
func (s *Session) positionSync262(id int32, fits, moved, turned, headTurned bool, d [3]int64, yaw, pitch, head byte, pos mgl64.Vec3, rot cube.Rotation, onGround bool) {
	if fits {
		switch {
		case moved && turned:
			w := s.packet()
			w.VarInt(id)
			w.Int16(int16(d[0]))
			w.Int16(int16(d[1]))
			w.Int16(int16(d[2]))
			w.Byte(yaw)
			w.Byte(pitch)
			w.Bool(onGround)
			s.queue(v777.ClientboundPlayMoveEntityPosRot, w)
		case moved:
			w := s.packet()
			w.VarInt(id)
			w.Int16(int16(d[0]))
			w.Int16(int16(d[1]))
			w.Int16(int16(d[2]))
			w.Bool(onGround)
			s.queue(v777.ClientboundPlayMoveEntityPos, w)
		case turned:
			w := s.packet()
			w.VarInt(id)
			w.Byte(yaw)
			w.Byte(pitch)
			w.Bool(onGround)
			s.queue(v777.ClientboundPlayMoveEntityRot, w)
		}
		if headTurned {
			w := s.packet()
			w.VarInt(id)
			w.Byte(head)
			s.queue(v777.ClientboundPlayRotateHead, w)
		}
		return
	}
	w := s.packet()
	w.VarInt(id)
	w.Float64(pos[0])
	w.Float64(pos[1])
	w.Float64(pos[2])
	w.Float64(0) // delta movement
	w.Float64(0)
	w.Float64(0)
	w.Float32(float32(rot.Yaw()))
	w.Float32(float32(rot.Pitch()))
	w.Bool(onGround)
	s.queue(v777.ClientboundPlayEntityPositionSync, w)
	w = s.packet()
	w.VarInt(id)
	w.Byte(head)
	s.queue(v777.ClientboundPlayRotateHead, w)
}

// setTrack records what an add_entity told the client: the delta base starts at the spawn position.
func (s *Session) setTrack(id int32, pos mgl64.Vec3, rot cube.Rotation) {
	s.setTrackHead(id, pos, rot, rot.Yaw())
}

// setTrackHead is setTrack for an entity spawned with its head yaw apart from its body's.
func (s *Session) setTrackHead(id int32, pos mgl64.Vec3, rot cube.Rotation, headYaw float64) {
	s.entMu.Lock()
	s.tracks[id] = &track{base: pos, yaw: angleByte(rot.Yaw()), pitch: angleByte(rot.Pitch()), hd: angleByte(headYaw), synced: true}
	s.entMu.Unlock()
}

// angleByte is an angle in degrees as the 1/256-turn byte Java uses.
func angleByte(deg float64) byte { return byte(int32(math.Floor(deg * 256 / 360))) }

// ViewEntityVelocity sets an entity's motion; for the player itself this is knockback.
func (s *Session) ViewEntityVelocity(e world.Entity, vel mgl64.Vec3) {
	id, ok := s.entityID(e)
	if !ok || s.drivenBySelf(e) {
		return
	}
	w := s.packet()
	w.VarInt(id)
	w.LpVec3(vel[0], vel[1], vel[2])
	s.queue(v777.ClientboundPlaySetEntityMotion, w)
}

// ViewEntityAction plays an entity animation.
func (s *Session) ViewEntityAction(e world.Entity, a world.EntityAction) {
	id, ok := s.entityID(e)
	if !ok {
		return
	}
	switch a := a.(type) {
	case entity.DeathAction:
		if id == selfEntityID {
			return // the client has its own death screen
		}
		// Health 0 makes the client play the death animation (falling over, turning red); the
		// DEATH entity event plays the death sound. The entity is spawned anew on respawn.
		w := s.packet()
		w.VarInt(id)
		w.Byte(dataLivingHealth)
		w.VarInt(dataTypeFloat)
		w.Float32(0)
		w.Byte(0xff)
		s.queue(v777.ClientboundPlaySetEntityData, w)
		w = s.packet()
		w.Int32(id)
		w.Byte(3) // EntityEvent.DEATH
		s.queue(v777.ClientboundPlayEntityEvent, w)
	case entity.SwingArmAction:
		if id == selfEntityID {
			s.selfSwingCount().Add(1)
			return // the client already swung
		}
		if !s.ver.Native() {
			// 26.2 has no swing_animation: animate action 0 swings the main hand (the client casts
			// the entity to LivingEntity; SwingArmAction is only sent for living entities).
			w := s.packet()
			w.VarInt(id)
			w.Byte(animate262SwingMainHand)
			s.queue(v777.ClientboundPlayAnimate, w)
			return
		}
		// 26.3 swings with swing_animation (animate no longer has a swing action).
		w := s.packet()
		w.VarInt(id)
		w.VarInt(0) // main hand
		w.VarInt(1) // SwingAnimationType.WHACK
		w.VarInt(6) // duration in ticks (SwingAnimation.DEFAULT)
		s.queue(v777.ClientboundPlaySwingAnimation, w)
	case entity.HurtAction:
		// damage_event makes the client play both the hurt animation and the entity's hurt sound.
		dt := damageGeneric
		if l := s.legacy(); l != nil {
			dt = version.Map(l.damageType, dt)
		}
		w := s.packet()
		w.VarInt(id)
		w.VarInt(dt)
		w.VarInt(0) // no cause entity
		w.VarInt(0) // no direct entity
		w.Bool(false)
		s.queue(v777.ClientboundPlayDamageEvent, w)
		if id != selfEntityID {
			s.viewMobData(e, id) // its health (an iron golem's cracks, a wolf's tail)
		}
	case interface{ TNTMinecartPrimed() }: // entity.MinecartPrimeAction (Crossfly Full)
		w := s.packet()
		w.Int32(id)
		w.Byte(70) // EntityEvent: a TNT minecart is primed
		s.queue(v777.ClientboundPlayEntityEvent, w)
	case entity.CriticalHitAction:
		s.animate(id, 1) // CRITICAL_HIT
	case entity.EnchantedHitAction:
		s.animate(id, 2) // MAGIC_CRITICAL_HIT
	case mobActorEvent:
		if s.bossEntityEvent(e, id, a.ActorEvent()) { // mobdata_boss.go
			return
		}
		if id != selfEntityID {
			s.viewActorEvent(e, id, a.ActorEvent()) // a sheep eating grass, a wolf shaking...
		}
	}
}

// animate sends an animate packet (26.3 actions: 0 wake up, 1 critical hit, 2 magic critical hit).
// 26.2 numbers them by id instead: 0 swing main hand, 2 wake up, 3 swing off hand, 4 critical
// hit, 5 magic critical hit.
func (s *Session) animate(id int32, action byte) {
	if !s.ver.Native() {
		if int(action) >= len(animate262) {
			return
		}
		action = animate262[action]
	}
	w := s.packet()
	w.VarInt(id)
	w.Byte(action)
	s.queue(v777.ClientboundPlayAnimate, w)
}

// animate262 maps the 26.3 animate actions (ordinals) to the 26.2 action ids.
var animate262 = [...]byte{0: 2, 1: 4, 2: 5}

const animate262SwingMainHand = 0

// ViewBlockUpdate sends a changed block.
func (s *Session) ViewBlockUpdate(pos cube.Pos, b world.Block, layer int) {
	if layer != 0 {
		return // water layer changes: waterlogging fixer to come
	}
	s.updateFrame(pos, b)
	rid := world.BlockRuntimeID(b)
	bi := s.blk
	if int(rid) >= len(bi.java) {
		return
	}
	w := s.packet()
	w.Position(pos[0], pos[1], pos[2])

	state := int32(bi.java[rid])
	if jb, ok := b.(JavaBlock); ok {
		state = s.ver.BlockState(jb.JavaState())
	}
	w.VarInt(state)
	s.queue(v777.ClientboundPlayBlockUpdate, w)
	if jb, ok := b.(JavaBlock); ok {
		s.sendBlockEntity(pos, jb)
	}
}
