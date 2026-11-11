//go:build crossfly_full

package hostile

import (
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

const (
	guardianID      = "minecraft:guardian"
	elderGuardianID = "minecraft:elder_guardian"
)

// Guardian is the per-mob state of a guardian or an elder guardian
// (Guardian, ElderGuardian).
type Guardian struct {
	elder bool
	// moving is DATA_ID_MOVING: spikes in. Both clients see it as the
	// Bedrock actor flag moving (javasession guardianData sends it as 16).
	moving bool
	// attackTarget is DATA_ID_ATTACK_TARGET: who the beam points at.
	attackTarget *world.EntityHandle
	// stroll is the RandomStrollGoal that hurt and the attack goal trigger
	// (randomStrollGoal.trigger()).
	stroll StrollTrigger
	attack *guardianAttackGoal
}

// StrollTrigger is RandomStrollGoal.trigger: the stroll runs on its next
// check instead of waiting for its random interval. The base's random stroll
// goal implements it.
type StrollTrigger interface{ Trigger() }

// guardianOf returns the guardian state of m.
func guardianOf(m *mob.Mob) *Guardian {
	return stateOf(m, func() *Guardian { return &Guardian{elder: m.Identifier() == elderGuardianID} })
}

// AttackDuration is Guardian.getAttackDuration: 80 ticks for a guardian, 60
// for an elder guardian.
func (g *Guardian) AttackDuration() int {
	if g.elder {
		return 60
	}
	return 80
}

// Moving reports DATA_ID_MOVING.
func (g *Guardian) Moving() bool { return g.moving }

// setMoving is Guardian.setMoving.
func (g *Guardian) setMoving(m *mob.Mob, v bool) {
	g.moving = v
	if m.ActorFlag(protocol.EntityDataFlagMoving) != v {
		m.SetActorFlag(protocol.EntityDataFlagMoving, v)
	}
}

// setActiveAttackTarget is Guardian.setActiveAttackTarget (nil for none).
func (g *Guardian) setActiveAttackTarget(h *world.EntityHandle) { g.attackTarget = h }

// ActiveAttackTarget is the beam's target, if any.
func (g *Guardian) ActiveAttackTarget() *world.EntityHandle { return g.attackTarget }

// GuardianAttributes are Guardian.createAttributes and
// ElderGuardian.createAttributes: max health, movement speed, attack damage.
func GuardianAttributes(elder bool) (health, speed, attack float64) {
	if elder {
		return 80, float64(float32(0.3)), 8
	}
	return 30, 0.5, 6
}

// guardianAttackSelector is Guardian.GuardianAttackSelector: players, squid
// (glow squid too) and axolotls more than 3 blocks away.
func guardianAttackSelector(m *mob.Mob, target world.Entity) bool {
	if distanceSqr(m, target) <= 9 {
		return false
	}
	if isPlayer(target) {
		return true
	}
	switch entityID(target) {
	case "minecraft:squid", "minecraft:glow_squid", "minecraft:axolotl":
		return true
	}
	return false
}

// entityID returns the identifier of a mob, or the encoded entity type.
func entityID(e world.Entity) string {
	if m, ok := mob.Of(e); ok {
		return m.Identifier()
	}
	return e.H().Type().EncodeEntity()
}

// guardianAttackGoal is Guardian.GuardianAttackGoal (priority 4,
// MOVE|LOOK): stop, stare at the target for 10 ticks, then charge the beam
// for the attack duration (80, elder 60) and hit: 1 magic damage (+2 on
// hard, +2 for the elder) plus a melee hit, then drop the target. Losing
// sight of the target drops it; a guardian (not the elder) gives up when
// the target comes within 3 blocks.
type guardianAttackGoal struct {
	attackTime int
	elder      bool
}

func (g *guardianAttackGoal) Flags() mob.ControlFlags { return mob.FlagMove | mob.FlagLook }

func (g *guardianAttackGoal) RequiresUpdateEveryTick() bool { return true }

func (g *guardianAttackGoal) CanUse(m *mob.Mob) bool {
	t, ok := m.Target()
	return ok && alive(t)
}

func (g *guardianAttackGoal) CanContinueToUse(m *mob.Mob) bool {
	if !g.CanUse(m) {
		return false
	}
	if g.elder {
		return true
	}
	t, _ := m.Target()
	return distanceSqr(m, t) > 9
}

func (g *guardianAttackGoal) Start(m *mob.Mob) {
	g.attackTime = -10
	navStop(m)
	if t, ok := m.Target(); ok {
		lookControlAt(m, t, 90, 90)
	}
	m.SyncMovement()
}

func (g *guardianAttackGoal) Stop(m *mob.Mob) {
	gs := guardianOf(m)
	gs.setActiveAttackTarget(nil)
	m.SetTarget(nil)
	if gs.stroll != nil {
		gs.stroll.Trigger()
	}
}

func (g *guardianAttackGoal) Tick(m *mob.Mob) {
	t, ok := m.Target()
	if !ok {
		return
	}
	navStop(m)
	lookControlAt(m, t, 90, 90)
	if !hasLineOfSight(m, t) {
		m.SetTarget(nil)
		return
	}
	gs := guardianOf(m)
	g.attackTime++
	switch {
	case g.attackTime == 0:
		gs.setActiveAttackTarget(t.H())
		// Entity event 21 (GuardianAttackSound): the beam's charging sound.
		for _, v := range m.Tx().Viewers(m.Position()) {
			v.ViewEntityAction(m, guardianAttackSoundAction{})
		}
	case g.attackTime >= gs.AttackDuration():
		dmg := 1.0
		if difficultyID(m.Tx()) == difficultyHard {
			dmg += 2
		}
		if g.elder {
			dmg += 2
		}
		if l, ok := t.(entity.Living); ok {
			l.Hurt(dmg, indirectMagicSource{Source: m, Attacker: m})
		}
		doHurtTarget(m, t)
		m.SetTarget(nil)
	}
}

// guardianAttackSoundAction is entity event 21 (EntityEvent.GUARDIAN_
// ATTACK_SOUND): Java plays the beam sound for its whole length (javasession
// maps Bedrock's actor event).
type guardianAttackSoundAction struct{}

func (guardianAttackSoundAction) EntityAction()     {}
func (guardianAttackSoundAction) ActorEvent() uint8 { return packet.ActorEventGuardianAttackSound }

// indirectMagicSource is DamageSources.indirectMagic(guardian, guardian):
// magic, not reduced by armour.
type indirectMagicSource struct {
	Source, Attacker world.Entity
}

func (indirectMagicSource) ReducedByArmour() bool     { return false }
func (indirectMagicSource) ReducedByResistance() bool { return true }
func (indirectMagicSource) Fire() bool                { return false }
func (indirectMagicSource) IgnoreTotem() bool         { return false }

// GuardianHurt is the part of Guardian.hurtServer before the damage: hit by
// a living entity directly (not by a projectile it shot) while the spikes are
// out (not moving), the attacker takes 2 thorns damage, unless the damage
// type avoids guardian thorns (magic, thorns, explosions of blocks...: the
// base passes directLiving only for melee-like hits). Every hit makes the
// guardian stroll away.
func GuardianHurt(m *mob.Mob, directLiving world.Entity, thornsLike bool) {
	g := guardianOf(m)
	if !g.moving && !thornsLike && directLiving != nil && directLiving.H() != m.H() {
		if l, ok := directLiving.(entity.Living); ok {
			l.Hurt(2, thornsSource{Owner: m})
		}
	}
	if g.stroll != nil {
		g.stroll.Trigger()
	}
}

// thornsSource is DamageSources.thorns(guardian).
type thornsSource struct{ Owner world.Entity }

func (thornsSource) ReducedByArmour() bool     { return false }
func (thornsSource) ReducedByResistance() bool { return true }
func (thornsSource) Fire() bool                { return false }
func (thornsSource) IgnoreTotem() bool         { return false }

// GuardianAIStep is the server part of Guardian.aiStep, before the base's
// aiStep: in water the air supply stays full; on land on the ground the
// guardian flops (a random hop and turn); while the beam is on, the body
// turns with the head.
func GuardianAIStep(m *mob.Mob) {
	if m.Dead() {
		return
	}
	if !inWater(m) && onGround(m) {
		r := m.Rand()
		v := m.Vel()
		m.SetVelocity(v.Add(mgl64.Vec3{(nextFloat(r)*2 - 1) * 0.4, 0.5, (nextFloat(r)*2 - 1) * 0.4}))
		m.SetYaw(nextFloat(r) * 360)
		setOnGround(m, false)
		m.SyncMovement()
	}
	if guardianOf(m).attackTarget != nil {
		m.SetYaw(m.HeadYaw())
	}
}

// GuardianTravelInWater is Guardian.travelInWater: move with 0.1 of the
// input, 0.9 drag, and sink slowly when idle (not moving, no target).
func GuardianTravelInWater(m *mob.Mob, input mgl64.Vec3) {
	v := m.Vel().Add(inputVector(input, 0.1, m.Yaw()))
	v = Hooks.Move(m, v)
	v = v.Mul(0.9)
	if _, ok := m.Target(); !guardianOf(m).moving && !ok {
		v[1] -= 0.005
	}
	m.SetVelocity(v)
}

// inputVector is Entity.getInputVector.
func inputVector(input mgl64.Vec3, speed, yRot float64) mgl64.Vec3 {
	l := input.LenSqr()
	if l < 1e-7 {
		return mgl64.Vec3{}
	}
	if l > 1 {
		input = input.Normalize()
	}
	mv := input.Mul(speed)
	rad := yRot * math.Pi / 180
	sin, cos := math.Sin(rad), math.Cos(rad)
	return mgl64.Vec3{mv[0]*cos - mv[2]*sin, mv[1], mv[2]*cos + mv[0]*sin}
}

// entityNetID stands in for Entity.getId in formulas that only need a
// per-entity offset (the wobble phase, flap offsets): a stable number from
// the entity's UUID.
func entityNetID(e world.Entity) int32 {
	id := e.H().UUID()
	return int32(uint32(id[12])<<24|uint32(id[13])<<16|uint32(id[14])<<8|uint32(id[15])) & 0x7fffffff
}

// Elder guardian curse (ElderGuardian.customServerAiStep).
const (
	elderEffectInterval     = 1200
	elderEffectRadius       = 50
	elderEffectDuration     = 6000
	elderEffectAmplifier    = 2
	elderEffectDisplayLimit = 1200
)

// curseAction is the elder guardian ghost the cursed player sees
// (ClientboundGameEventPacket.GUARDIAN_ELDER_EFFECT for Java, actor event 35
// for Bedrock; javasession maps the actor event).
type curseAction struct{}

func (curseAction) EntityAction()     {}
func (curseAction) ActorEvent() uint8 { return packet.ActorEventGuardianMiningFatigue }

// ElderGuardianServerAIStep is ElderGuardian.customServerAiStep: every 1200
// ticks (offset per elder) Mining Fatigue III for 5 minutes on every player
// in survival or adventure within 50 blocks who does not have it at least as
// strong with more than a minute left, and they see the ghost; the elder
// keeps its home (16 blocks) where it first was.
func ElderGuardianServerAIStep(m *mob.Mob) {
	if (tickCount(m)+int64(entityNetID(m)))%elderEffectInterval == 0 {
		for _, pl := range elderCurseTargets(m) {
			pl.AddEffect(effect.New(effect.MiningFatigue, elderEffectAmplifier+1, elderEffectDuration*time.Second/20))
			for _, v := range m.Tx().Viewers(pl.Position()) {
				v.ViewEntityAction(pl, curseAction{})
			}
		}
	}
	if _, ok := m.Home(); !ok {
		m.SetHome(blockPos(m.Pos()))
	}
}

// elderCurseTargets is the player filter of MobEffectUtil.
// addEffectToPlayersAround for the elder's curse.
func elderCurseTargets(m *mob.Mob) []*player.Player {
	var out []*player.Player
	pos := m.Pos()
	for e := range m.Tx().Players() {
		pl, ok := e.(*player.Player)
		if !ok {
			continue
		}
		gm := pl.GameMode()
		if gm == world.GameModeCreative || gm == world.GameModeSpectator {
			continue // gameMode.isSurvival: survival and adventure
		}
		if pl.Position().Sub(pos).LenSqr() >= elderEffectRadius*elderEffectRadius {
			continue
		}
		if cur, ok := pl.Effect(effect.MiningFatigue); ok {
			endsWithin := !cur.Infinite() && cur.Duration() <= time.Duration(elderEffectDisplayLimit-1)*time.Second/20
			if cur.Level()-1 >= elderEffectAmplifier && !endsWithin {
				continue
			}
		}
		out = append(out, pl)
	}
	return out
}
