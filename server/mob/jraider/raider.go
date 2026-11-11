//go:build crossfly_full

package jraider

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/jraid"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// The entity identifiers of the raiders. They are the Bedrock identifiers (the evoker is
// minecraft:evocation_illager); Java clients see the Java types through javasession.
const (
	PillagerID   = "minecraft:pillager"
	VindicatorID = "minecraft:vindicator"
	EvokerID     = "minecraft:evocation_illager"
	VexID        = "minecraft:vex"
	RavagerID    = "minecraft:ravager"
	WitchID      = "minecraft:witch"
)

// bedrockID maps the raid kinds to the identifiers of their types.
var bedrockID = map[jraid.Kind]string{
	jraid.Vindicator: VindicatorID, jraid.Evoker: EvokerID, jraid.Pillager: PillagerID,
	jraid.Witch: WitchID, jraid.Ravager: RavagerID,
}

// raiderIDs are the #minecraft:raiders entity tag (the illusioner has no Bedrock type).
var raiderIDs = []string{EvokerID, PillagerID, RavagerID, VindicatorID, WitchID}

// isRaiderEntity reports whether e is in the raiders tag (Java or Bedrock engine mob).
func isRaiderEntity(e world.Entity) bool {
	id := e.H().Type().EncodeEntity()
	for _, r := range raiderIDs {
		if id == r {
			return true
		}
	}
	return false
}

// isIllager is the illager entity tag (AbstractIllager): the raiders but witches and ravagers.
func isIllager(e world.Entity) bool {
	switch e.H().Type().EncodeEntity() {
	case EvokerID, PillagerID, VindicatorID:
		return true
	}
	return false
}

// Raider is the raider side of a raider mob (Raider and PatrollingMonster): its raid state and what raids read of
// it. jraid keeps it across ticks, so it holds a snapshot of the mob taken each tick rather than the *javamob.Mob.
type Raider struct {
	jraid.State
	kind jraid.Kind

	id      uuid.UUID
	h       *world.EntityHandle
	w       *world.World
	health  float32
	pos     cube.Pos
	alive   bool
	removed bool
	tick    int
	noAct   int

	// pending is set for a raider a raid created that is not in the world yet (EntityType.create).
	pending *level
	// banner is a captain banner to put on (SetCaptainBanner before the mob exists or between ticks).
	banner bool
	// celebrateShown is the celebrating flag the clients see.
	celebrateShown bool
	// captainBanner is the ominous banner on the raider's head. Dragonfly's armour inventory takes no banners
	// (helmets only), so the banner is kept here: neither client shows it, it drops on death.
	captainBanner bool
	// glowTicks is the glowing a resonating bell gave the raider.
	glowTicks int
}

func newRaider(k jraid.Kind) *Raider { return &Raider{kind: k, alive: true, id: uuid.New()} }

func newPendingRaider(k jraid.Kind, l level) *Raider {
	r := newRaider(k)
	r.health = float32(jraid.MobAttributes[k.ID()].MaxHealth)
	r.pending = &l
	return r
}

// raiderBehaviour is a behaviour with a Raider (every type of this package but the vex).
type raiderBehaviour interface {
	javamob.Behaviour
	raider() *Raider
	setRaider(r *Raider)
}

// raiderOf returns the Raider of e, if e is a raider of this package.
func raiderOf(e world.Entity) (*Raider, bool) {
	m, ok := javamob.Of(e)
	if !ok {
		return nil, false
	}
	rb, ok := m.Behaviour().(raiderBehaviour)
	if !ok {
		return nil, false
	}
	return rb.raider(), true
}

func (r *Raider) RaiderState() *jraid.State { return &r.State }
func (r *Raider) UUID() uuid.UUID           { return r.id }
func (r *Raider) CanBeLeader() bool         { return r.kind.CanBeLeader() }
func (r *Raider) Health() float32           { return r.health }
func (r *Raider) Alive() bool               { return r.alive && !r.Removed() }
func (r *Raider) Removed() bool             { return r.removed || r.h != nil && r.h.Closed() }
func (r *Raider) BlockPos() cube.Pos        { return r.pos }
func (r *Raider) TickCount() int            { return r.tick }
func (r *Raider) NoActionTime() int         { return r.noAct }

// Celebrate is Raider.setCelebrating; the clients see it from the raider's next tick.
func (r *Raider) Celebrate(on bool) { r.Celebrating = on }

// SetCaptainBanner puts the ominous banner on the raider's head at its next tick (or when it spawns).
func (r *Raider) SetCaptainBanner() { r.banner = true }

// SpawnForRaid is the part of Raid.joinRaid for a new raider: the mob is created at the position, finalized for
// EntitySpawnReason.EVENT, given its raid buffs and added to the world.
func (r *Raider) SpawnForRaid(pos cube.Pos, wave int) {
	l := r.pending
	if l == nil {
		return
	}
	r.pending = nil
	at := mgl64.Vec3{float64(pos[0]) + 0.5, float64(pos[1]) + 1, float64(pos[2]) + 0.5}
	m, err := javamob.Spawn(l.tx, bedrockID[r.kind], at, javamob.SpawnEvent, javamob.SpawnOptions{Configure: func(m *javamob.Mob) {
		if rb, ok := m.Behaviour().(raiderBehaviour); ok {
			rb.setRaider(r)
			r.sync(m)
		}
	}})
	if err != nil {
		r.removed = true
		return
	}
	if b, ok := m.Behaviour().(interface{ ApplyRaidBuffs(*javamob.Mob, int) }); ok {
		b.ApplyRaidBuffs(m, wave)
	}
	r.apply(m)
}

// RideForRaid would put the rider on the raid's ravager (startRiding). Java mode mobs have no riding tick yet
// (javamob README: ridden travel is not done), so the rider stays on foot beside it.
func (r *Raider) RideForRaid(jraid.Raider, cube.Pos) {}

// sync takes the snapshot of the mob that raids read between ticks.
func (r *Raider) sync(m *javamob.Mob) {
	tx := m.Tx()
	r.h, r.id, r.w = m.H(), m.H().UUID(), tx.World()
	r.health = float32(m.Health())
	r.pos = cube.PosFromVec3(m.Position())
	r.alive = !m.Dead()
	r.tick = m.TickCount()
	r.noAct = m.NoActionTime()
	st := stateOf(r.w)
	st.tx = tx
	st.refs[r.id] = r
}

// apply shows what raids changed between ticks: the captain banner and the celebrating flag.
func (r *Raider) apply(m *javamob.Mob) {
	if r.banner && !m.Dead() {
		r.banner = false
		r.captainBanner = true
	}
	if r.celebrateShown != r.Celebrating {
		r.celebrateShown = r.Celebrating
		m.SetActorFlag(protocol.EntityDataFlagCelebrating, r.Celebrating)
	}
}

// OminousBanner is Raid.getOminousBannerInstance.
func OminousBanner() item.Stack {
	return item.NewStack(core.OminousBanner(), 1)
}

func isOminousBanner(s item.Stack) bool {
	b, ok := s.Item().(block.Banner)
	return ok && b.Illager
}

// wearsOminousBanner is the head slot check of Raider.isCaptain.
func wearsOminousBanner(r *Raider) bool { return r.captainBanner }

// IsCaptain is Raider.isCaptain.
func (r *Raider) IsCaptain() bool { return r.State.IsCaptain(r.captainBanner) }

// DropCustomDeathLoot drops the captain banner (drop chance 2: always) and, for an illager captain, the ominous
// bottle of its loot table (the entity_properties raider is_captain pool with set_ominous_bottle_amplifier 0-4,
// which the base loot code does not model).
func (b *base) DropCustomDeathLoot(m *javamob.Mob, _ world.DamageSource, _ bool) {
	if !b.captainBanner {
		return
	}
	if b.IsCaptain() && (b.kind == jraid.Pillager || b.kind == jraid.Vindicator || b.kind == jraid.Evoker) {
		m.SpawnAtLocation(item.NewStack(item.OminousBottle{Amplifier: m.Rand().IntN(5)}, 1))
	}
	m.SpawnAtLocation(OminousBanner())
	b.captainBanner = false
}

// base is the behaviour shared by the raider types (Raider, PatrollingMonster, AbstractIllager). Each type embeds
// it and calls its hooks from its own overrides.
type base struct {
	*Raider
	celebrateSound string
}

func newBase(k jraid.Kind, celebrate string) base {
	return base{Raider: newRaider(k), celebrateSound: celebrate}
}

func (b *base) raider() *Raider     { return b.Raider }
func (b *base) setRaider(r *Raider) { b.Raider = r }

// registerRaiderGoals is PatrollingMonster.registerGoals and Raider.registerGoals.
func (b *base) registerRaiderGoals(m *javamob.Mob) {
	m.Goals().Add(4, newLongDistancePatrol(b, 0.7, 0.595))
	m.Goals().Add(1, newObtainBanner(b))
	m.Goals().Add(3, newPathfindToRaid(b))
	m.Goals().Add(4, newMoveThroughVillage(b, 1.05, 1))
	m.Goals().Add(5, newCelebration(b, b.celebrateSound))
}

// Tick runs before the mob's tick: the snapshot, a raid read from NBT and what raids changed.
func (b *base) Tick(m *javamob.Mob) {
	r := b.Raider
	r.sync(m)
	jraid.Rejoin(stateOf(r.w).raids, r)
	r.apply(m)
	if r.glowTicks > 0 {
		r.glowTicks--
	}
}

// AiStep is the part of Raider.aiStep before Mob.aiStep.
func (b *base) AiStep(m *javamob.Mob) {
	l := levelOf(m.Tx())
	t, ok := m.Target()
	busy := ok && (isPlayer(t) || t.H().Type().EncodeEntity() == "minecraft:iron_golem")
	if jraid.AIStep(l, l.st.raids, b.Raider, busy) {
		m.SetNoActionTime(0)
	}
}

// CustomServerAiStep is Raider.updateNoActionTime: raiders idle twice as fast (the base counts one).
func (b *base) CustomServerAiStep(m *javamob.Mob) {
	m.SetNoActionTime(m.NoActionTime() + jraid.NoActionTimeStep - 1)
	b.noAct = m.NoActionTime()
}

// OnHurt is Raider.hurtServer: the raid bar follows the health.
func (b *base) OnHurt(m *javamob.Mob, _ float64, _ world.DamageSource) {
	b.health = float32(m.Health())
	levelOf(m.Tx())
	jraid.Hurt(b.Raider)
}

// OnDeath is Raider.die.
func (b *base) OnDeath(m *javamob.Mob, src world.DamageSource) {
	b.health, b.alive = 0, false
	levelOf(m.Tx())
	var killer *uuid.UUID
	if e := javamob.SourceEntity(src); e != nil && isPlayer(e) {
		id := e.H().UUID()
		killer = &id
	}
	jraid.Die(b.Raider, killer)
	b.removed = true
}

// RemoveWhenFarAway is Raider.removeWhenFarAway.
func (b *base) RemoveWhenFarAway(_ *javamob.Mob, distSqr float64) bool {
	return b.State.RemoveWhenFarAway(distSqr)
}

// finalizeRaider is PatrollingMonster.finalizeSpawn and Raider.finalizeSpawn.
func (b *base) finalizeRaider(m *javamob.Mob, reason javamob.SpawnReason) {
	b.sync(m)
	b.State.FinalizeSpawn(m.Rand(), spawnReason(reason), b.CanBeLeader(), b.kind == jraid.Witch, b.SetCaptainBanner)
	b.apply(m)
}

// FinalizeSpawn is the finalizeSpawn of raiders without equipment of their own (witch, ravager).
func (b *base) FinalizeSpawn(m *javamob.Mob, reason javamob.SpawnReason) { b.finalizeRaider(m, reason) }

func spawnReason(r javamob.SpawnReason) jraid.SpawnReason {
	switch r {
	case javamob.SpawnNatural, javamob.SpawnChunkGeneration:
		return jraid.ReasonNatural
	case javamob.SpawnPatrol:
		return jraid.ReasonPatrol
	case javamob.SpawnEvent:
		return jraid.ReasonEvent
	case javamob.SpawnStructure:
		return jraid.ReasonStructure
	case javamob.SpawnSpawner, javamob.SpawnTrialSpawner:
		return jraid.ReasonSpawner
	case javamob.SpawnCommand:
		return jraid.ReasonCommand
	case javamob.SpawnSpawnItemUse:
		return jraid.ReasonSpawnEgg
	}
	return jraid.ReasonOther
}

// EncodeNBT is Raider.addAdditionalSaveData.
func (b *base) EncodeNBT(data map[string]any) { b.State.SaveData(data) }

// DecodeNBT is Raider.readAdditionalSaveData: the raid is joined at the first tick.
func (b *base) DecodeNBT(_ *javamob.Mob, data map[string]any) { b.State.LoadData(data) }

// illagerCanAttack is AbstractIllager.canAttack: no baby villagers.
func illagerCanAttack(e world.Entity) bool {
	if bm, ok := mob.Of(e); ok && bm.Baby() {
		switch e.H().Type().EncodeEntity() {
		case "minecraft:villager_v2", "minecraft:villager", "minecraft:wandering_trader":
			return false
		}
	}
	return true
}

func isPlayer(e world.Entity) bool { _, ok := e.(*player.Player); return ok }
