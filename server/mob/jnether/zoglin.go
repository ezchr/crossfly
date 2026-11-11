//go:build crossfly_full

package jnether

import (
	"math"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/jbrain"
	"github.com/df-mc/dragonfly/server/mob/jbrain/jmob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// ZoglinID is the zoglin's identifier.
const ZoglinID = "minecraft:zoglin"

func init() {
	javamob.Register(javamob.Type{
		ID: ZoglinID, Category: javamob.CategoryMonster,
		Width: 1.3964844, Height: 1.4,
		NotInPeaceful: true, Undead: true,
		// Zoglin.createAttributes.
		Attributes: func() map[javamob.Attribute]float64 {
			a := javamob.MonsterAttributes()
			a[javamob.AttrMaxHealth] = 40
			a[javamob.AttrMovementSpeed] = float64(float32(0.3))
			a[javamob.AttrKnockbackResistance] = float64(float32(0.6))
			a[javamob.AttrAttackKnockback] = 1
			a[javamob.AttrAttackDamage] = 6
			return a
		},
		Families: []string{"zoglin", "monster", "undead", "mob"},
		XPReward: 5,
		New:      func() javamob.Behaviour { return &Zoglin{} },
	})
}

// zoglinProvider is Zoglin.BRAIN_PROVIDER.
var zoglinProvider = &jbrain.Provider[*javamob.Mob]{
	EntityType: "zoglin",
	Sensors:    []*jbrain.SensorType[*javamob.Mob]{jmob.NearestLivingEntities, jmob.NearestPlayers},
	Activities: func(*javamob.Mob) []jbrain.ActivityData[*javamob.Mob] {
		isAdult := func(m *javamob.Mob) bool { return !m.Baby() }
		isBaby := func(m *javamob.Mob) bool { return m.Baby() }
		return []jbrain.ActivityData[*javamob.Mob]{
			{Activity: jbrain.ActCore, Behaviours: jbrain.Seq[*javamob.Mob](0, jmob.LookAtTargetSink(45, 90), jmob.MoveToTargetSink(150, 250))},
			{Activity: jbrain.ActIdle, Behaviours: jbrain.Seq[*javamob.Mob](10,
				StartAttacking(nil, zoglinFindTarget),
				SetEntityLookTargetSometimes(8, 30, 60, nil),
				jbrain.RunOne[*javamob.Mob](nil,
					jbrain.W[*javamob.Mob](RandomStroll(0.4), 2),
					jbrain.W[*javamob.Mob](jmob.SetWalkTargetFromLookTarget(0.4, 3), 2),
					jbrain.W[*javamob.Mob](jmob.DoNothing(30, 60), 1),
				),
			)},
			jbrain.ActivityErasing[*javamob.Mob](jbrain.ActFight, 10, jbrain.MemAttackTarget.ID(),
				SetWalkTargetFromAttackTargetIfTargetOutOfReach(func(*javamob.Mob) float32 { return 1 }),
				MeleeAttack(isAdult, 40),
				MeleeAttack(isBaby, 15),
				StopAttackingIfTargetInvalid(nil, nil, true),
			),
		}
	},
}

// zoglinFindTarget is Zoglin.findNearestValidAttackTarget: the closest
// visible attackable entity that is no zoglin and no creeper.
func zoglinFindTarget(c *jbrain.Ctx, m *javamob.Mob) (world.Entity, bool) {
	v, ok := jbrain.Get(c.Mem, jbrain.MemNearestVisibleLivingEntities)
	if !ok || v == nil {
		return nil, false
	}
	h, ok := v.FindClosest(c.Tx, func(h *world.EntityHandle) bool {
		e, ok := h.Entity(c.Tx)
		if !ok {
			return false
		}
		id := e.H().Type().EncodeEntity()
		return id != ZoglinID && id != "minecraft:creeper" && jmob.IsEntityAttackable(c, m, e)
	})
	if !ok {
		return nil, false
	}
	return h.Entity(c.Tx)
}

// Zoglin is the Zoglin class: a brain-driven hostile that attacks every
// living thing but zoglins and creepers.
type Zoglin struct {
	brain      *jmob.Brain
	attackAnim int
	baby       bool
}

// RegisterGoals makes the brain (Zoglin.makeBrain); the zoglin has no goals.
func (z *Zoglin) RegisterGoals(m *javamob.Mob) {
	z.brain = jmob.Attach(m, zoglinProvider, nil)
}

// FinalizeSpawn is Zoglin.finalizeSpawn: one in five is a baby.
func (z *Zoglin) FinalizeSpawn(m *javamob.Mob, _ javamob.SpawnReason) {
	if m.Rand().Float32() < 0.2 {
		m.SetBaby(true)
	}
}

// SetBaby is Zoglin.setBaby: the baby dimensions and 0.5 attack damage.
func (z *Zoglin) SetBaby(m *javamob.Mob, baby bool) {
	z.baby = baby
	if baby {
		m.SetDimensions(0.75, 0.85)
		m.Attributes().Instance(javamob.AttrAttackDamage).SetBase(0.5)
		m.SyncAttributes()
	}
}

// AiStep is Zoglin.aiStep: the attack animation timer.
func (z *Zoglin) AiStep(*javamob.Mob) {
	if z.attackAnim > 0 {
		z.attackAnim--
	}
}

// CustomServerAiStep is Zoglin.customServerAiStep after the brain tick:
// updateActivity, and the brain's attack target as the mob's target
// (getTarget is getTargetFromBrain).
func (z *Zoglin) CustomServerAiStep(m *javamob.Mob) {
	b := z.brain
	old, hadOld := b.ActiveNonCoreActivity()
	b.SetActiveActivityToFirstValid(jbrain.ActFight, jbrain.ActIdle)
	if a, ok := b.ActiveNonCoreActivity(); ok && a == jbrain.ActFight && (!hadOld || old != jbrain.ActFight) {
		m.PlaySound("angry")
	}
	c := jmob.RunCtx(m)
	t, ok := AttackTarget(c, b.Mem())
	m.SetAggressive(ok)
	if ok {
		m.SetTarget(t)
	} else if _, had := m.Mob.Target(); had {
		m.SetTarget(nil)
	}
}

// DoHurtTarget is Zoglin.doHurtTarget: entity event 4 and
// HoglinBase.hurtAndThrowTarget.
func (z *Zoglin) DoHurtTarget(m *javamob.Mob, target world.Entity) bool {
	if _, ok := target.(entity.Living); !ok {
		return false
	}
	z.attackAnim = 10
	broadcastHoglinEvent(m, hoglinEventAttack)
	m.PlaySound("attack")
	return hurtAndThrowTarget(m, target)
}

// OnHurt is the end of Zoglin.hurtServer: an attacker that may be attacked
// and is not much further away than the current target becomes it (200
// ticks).
func (z *Zoglin) OnHurt(m *javamob.Mob, _ float64, src world.DamageSource) {
	att, ok := jmob.SourceEntity(src)
	if !ok || !jmob.IsLiving(att) {
		return
	}
	c := jmob.RunCtx(m)
	if brainCanAttack(m, att) && !otherTargetMuchFurther(c, m, att, 4) {
		mem := z.brain.Mem()
		mem.Erase(jbrain.MemCantReachWalkTargetSince.ID())
		jbrain.SetWithExpiry(mem, jbrain.MemAttackTarget, att.H(), 200)
	}
}

// Persister: IsBaby.
func (z *Zoglin) EncodeNBT(data map[string]any) {
	if z.baby {
		data["IsBaby"] = uint8(1)
	} else {
		data["IsBaby"] = uint8(0)
	}
}
func (z *Zoglin) DecodeNBT(m *javamob.Mob, data map[string]any) {
	if v, ok := data["IsBaby"].(uint8); ok && v == 1 {
		m.SetBaby(true)
	}
}

// hurtAndThrowTarget is HoglinBase.hurtAndThrowTarget: damage/2 +
// rand(damage) for adults, then throwTarget.
func hurtAndThrowTarget(m *javamob.Mob, target world.Entity) bool {
	dmg := float32(m.Attr(javamob.AttrAttackDamage))
	if !m.Baby() && int(dmg) > 0 {
		dmg = dmg/2 + float32(m.Rand().IntN(int(dmg)))
	}
	hurt := hurtByMob(m, target, float64(dmg))
	if hurt {
		m.SetLastHurtMob(target)
		if !m.Baby() {
			throwTarget(m, target)
		}
	}
	return hurt
}

func hurtByMob(m *javamob.Mob, target world.Entity, dmg float64) bool {
	l, ok := target.(entity.Living)
	if !ok {
		return false
	}
	_, res := l.Hurt(dmg, entity.AttackDamageSource{Attacker: m})
	return res.Accepted()
}

// throwTarget is HoglinBase.throwTarget: a push of (knockback - target
// knockback resistance), turned up to 10 degrees either way.
func throwTarget(m *javamob.Mob, target world.Entity) {
	res := 0.0
	if jm, ok := javamob.Of(target); ok {
		res = jm.Attr(javamob.AttrKnockbackResistance)
	}
	power := m.Attr(javamob.AttrAttackKnockback) - res
	if power <= 0 {
		return
	}
	tp, mp := target.Position(), m.Position()
	r := m.Rand()
	angle := float32(r.IntN(21) - 10)
	hs := power * float64(r.Float32()*0.5+0.2)
	v := mgl64.Vec3{tp[0] - mp[0], 0, tp[2] - mp[2]}
	if v.Len() < 1e-4 {
		v = mgl64.Vec3{}
	} else {
		v = v.Normalize().Mul(hs)
	}
	// Vec3.yRot(angle): angle in radians as Java passes it.
	cos, sin := math.Cos(float64(angle)), math.Sin(float64(angle))
	v = mgl64.Vec3{v[0]*cos + v[2]*sin, 0, v[2]*cos - v[0]*sin}
	vs := power * float64(r.Float32()) * 0.5
	push := mgl64.Vec3{v[0], vs, v[2]}
	if jm, ok := javamob.Of(target); ok {
		jm.Push(push)
		return
	}
	if mv, ok := target.(interface {
		Velocity() mgl64.Vec3
		SetVelocity(mgl64.Vec3)
	}); ok {
		mv.SetVelocity(mv.Velocity().Add(push))
	}
}

// hoglinEvent is a Java entity event of hoglins and zoglins as a
// world.EntityAction.
type hoglinEvent byte

const hoglinEventAttack hoglinEvent = 4

// EntityAction ...
func (hoglinEvent) EntityAction() {}

// JavaEvent is the Java entity event id.
func (e hoglinEvent) JavaEvent() byte { return byte(e) }

// ActorEvent is the Bedrock actor event shown for it.
func (e hoglinEvent) ActorEvent() uint8 { return packet.ActorEventStartAttacking }

// broadcastHoglinEvent is Level.broadcastEntityEvent.
func broadcastHoglinEvent(m *javamob.Mob, e hoglinEvent) {
	for _, v := range m.Tx().Viewers(m.Position()) {
		v.ViewEntityAction(m, e)
	}
}
