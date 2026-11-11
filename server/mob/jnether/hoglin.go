//go:build crossfly_full

package jnether

import (
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/janimal"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/jbrain"
	"github.com/df-mc/dragonfly/server/mob/jbrain/jmob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// HoglinID is the hoglin's identifier.
const HoglinID = "minecraft:hoglin"

// hoglinFood is ItemTags.HOGLIN_FOOD.
var hoglinFood = janimal.ItemTag{"crimson_fungus": {}}

func init() {
	javamob.Register(javamob.Type{
		ID: HoglinID, Category: javamob.CategoryMonster,
		Width: 1.3964844, Height: 1.4,
		// Hoglin.createAttributes.
		Attributes: func() map[javamob.Attribute]float64 {
			a := javamob.MonsterAttributes()
			a[javamob.AttrMaxHealth] = 40
			a[javamob.AttrMovementSpeed] = float64(float32(0.3))
			a[javamob.AttrKnockbackResistance] = float64(float32(0.6))
			a[javamob.AttrAttackKnockback] = 1
			a[javamob.AttrAttackDamage] = 6
			return a
		},
		Families: []string{"hoglin", "monster", "mob"},
		XPReward: 5,
		New: func() javamob.Behaviour {
			h := &Hoglin{}
			h.Food = janimal.StackIn(hoglinFood)
			return h
		},
	})
}

// The hoglin's brain (HoglinAi).
var (
	// hoglinAdultSensor is SensorType.NEAREST_ADULT (AdultSensor): the
	// closest visible adult of the same type.
	hoglinAdultSensor = &jbrain.SensorType[*javamob.Mob]{Name: "minecraft:nearest_adult", Key: "adultsensor", New: func() jmob.Sensor {
		return sensorFn{req: []jbrain.MemoryID{jbrain.MemNearestVisibleAdult.ID(), jbrain.MemNearestVisibleLivingEntities.ID()}, sense: senseNearestAdult}
	}}
	// hoglinSpecificSensor is SensorType.HOGLIN_SPECIFIC_SENSOR.
	hoglinSpecificSensor = &jbrain.SensorType[*javamob.Mob]{Name: "minecraft:hoglin_specific_sensor", Key: "hoglinspecificsensor", New: func() jmob.Sensor {
		return sensorFn{req: []jbrain.MemoryID{
			jbrain.MemNearestVisibleLivingEntities.ID(), jbrain.MemNearestRepellent.ID(), jbrain.MemNearestVisibleAdultPiglin.ID(),
			jbrain.MemNearestVisibleAdultHoglins.ID(), jbrain.MemVisibleAdultPiglinCount.ID(), jbrain.MemVisibleAdultHoglinCount.ID(),
		}, sense: senseHoglinSpecific}
	}}
	hoglinProvider = &jbrain.Provider[*javamob.Mob]{
		EntityType: "hoglin",
		Memories:   []jbrain.MemoryID{jbrain.MemBreedTarget.ID(), jbrain.MemPacified.ID(), jbrain.MemAvoidTarget.ID(), jbrain.MemIsPanicking.ID()},
		Sensors:    []*jbrain.SensorType[*javamob.Mob]{jmob.NearestLivingEntities, jmob.NearestPlayers, hoglinAdultSensor, hoglinSpecificSensor},
		Activities: hoglinActivities,
	}
)

type sensorFn struct {
	req   []jbrain.MemoryID
	sense func(c *jbrain.Ctx, m *javamob.Mob)
}

func (s sensorFn) Requires() []jbrain.MemoryID         { return s.req }
func (s sensorFn) Sense(c *jbrain.Ctx, m *javamob.Mob) { s.sense(c, m) }

func senseNearestAdult(c *jbrain.Ctx, m *javamob.Mob) {
	v, ok := jbrain.Get(c.Mem, jbrain.MemNearestVisibleLivingEntities)
	if !ok || v == nil {
		c.Mem.Erase(jbrain.MemNearestVisibleAdult.ID())
		return
	}
	id := m.JavaType().ID
	h, ok := v.FindClosest(c.Tx, func(h *world.EntityHandle) bool {
		e, ok := h.Entity(c.Tx)
		return ok && h.Type().EncodeEntity() == id && !isBaby(e)
	})
	jbrain.SetOpt(c.Mem, jbrain.MemNearestVisibleAdult, h, ok)
}

// isBaby is LivingEntity.isBaby for any entity.
func isBaby(e world.Entity) bool {
	if b, ok := e.(interface{ Baby() bool }); ok {
		return b.Baby()
	}
	return false
}

func senseHoglinSpecific(c *jbrain.Ctx, m *javamob.Mob) {
	rep, ok := NearestHoglinRepellent(c.Tx, jmob.BlockPos(m))
	jbrain.SetOpt(c.Mem, jbrain.MemNearestRepellent, rep, ok)
	var piglin *world.EntityHandle
	piglins := 0
	var hoglins []*world.EntityHandle
	if v, ok := jbrain.Get(c.Mem, jbrain.MemNearestVisibleLivingEntities); ok && v != nil {
		for _, h := range v.FindAll(c.Tx, func(h *world.EntityHandle) bool {
			e, ok := h.Entity(c.Tx)
			id := h.Type().EncodeEntity()
			return ok && !isBaby(e) && (id == "minecraft:piglin" || id == HoglinID)
		}) {
			if h.Type().EncodeEntity() == "minecraft:piglin" {
				piglins++
				if piglin == nil {
					piglin = h
				}
			} else {
				hoglins = append(hoglins, h)
			}
		}
	}
	jbrain.SetOpt(c.Mem, jbrain.MemNearestVisibleAdultPiglin, piglin, piglin != nil)
	jbrain.Set(c.Mem, jbrain.MemNearestVisibleAdultHoglins, hoglins)
	jbrain.Set(c.Mem, jbrain.MemVisibleAdultPiglinCount, piglins)
	jbrain.Set(c.Mem, jbrain.MemVisibleAdultHoglinCount, len(hoglins))
}

func hoglinActivities(*javamob.Mob) []jbrain.ActivityData[*javamob.Mob] {
	isAdult := func(m *javamob.Mob) bool { return !m.Baby() }
	isBabyMob := func(m *javamob.Mob) bool { return m.Baby() }
	idleMove := func() jmob.Control {
		return jbrain.RunOne[*javamob.Mob](nil,
			jbrain.W[*javamob.Mob](RandomStroll(0.4), 2),
			jbrain.W[*javamob.Mob](jmob.SetWalkTargetFromLookTarget(0.4, 3), 2),
			jbrain.W[*javamob.Mob](jmob.DoNothing(30, 60), 1),
		)
	}
	adultOnly := func(o *oneShot) *oneShot {
		return jbrain.TriggerIf(func(_ *jbrain.Ctx, m *javamob.Mob) bool { return isAdult(m) }, o)
	}
	return []jbrain.ActivityData[*javamob.Mob]{
		{Activity: jbrain.ActCore, Behaviours: jbrain.Seq[*javamob.Mob](0, jmob.LookAtTargetSink(45, 90), jmob.MoveToTargetSink(150, 250))},
		{Activity: jbrain.ActIdle, Behaviours: jbrain.Seq[*javamob.Mob](10,
			BecomePassiveIfMemoryPresent(jbrain.MemNearestRepellent.ID(), 200),
			lift(janimal.AnimalMakeLove[*hogBody](HoglinID, 0.6, 2)),
			SetWalkTargetAwayFromPos(jbrain.MemNearestRepellent, 1, 8, true),
			StartAttacking(nil, hoglinFindTarget),
			adultOnly(jmob.SetWalkTargetAwayFromEntity(jbrain.MemNearestVisibleAdultPiglin, 0.4, 8, false)),
			SetEntityLookTargetSometimes(8, 30, 60, nil),
			lift(janimal.BabyFollowAdult[*hogBody](5, 16, func(*hogBody) float32 { return 0.6 })),
			idleMove(),
		)},
		jbrain.ActivityErasing[*javamob.Mob](jbrain.ActFight, 10, jbrain.MemAttackTarget.ID(),
			BecomePassiveIfMemoryPresent(jbrain.MemNearestRepellent.ID(), 200),
			lift(janimal.AnimalMakeLove[*hogBody](HoglinID, 0.6, 2)),
			SetWalkTargetFromAttackTargetIfTargetOutOfReach(func(*javamob.Mob) float32 { return 1 }),
			MeleeAttack(isAdult, 40),
			MeleeAttack(isBabyMob, 15),
			StopAttackingIfTargetInvalid(nil, nil, true),
			EraseMemoryIf("isBreeding", func(c *jbrain.Ctx, _ *javamob.Mob) bool { return c.Mem.Has(jbrain.MemBreedTarget.ID()) }, jbrain.MemAttackTarget.ID()),
		),
		jbrain.ActivityErasing[*javamob.Mob](jbrain.ActAvoid, 10, jbrain.MemAvoidTarget.ID(),
			jmob.SetWalkTargetAwayFromEntity(jbrain.MemAvoidTarget, 1.3, 15, false),
			idleMove(),
			SetEntityLookTargetSometimes(8, 30, 60, nil),
			EraseMemoryIf("wantsToStopFleeing", func(c *jbrain.Ctx, m *javamob.Mob) bool {
				return !m.Baby() && !piglinsOutnumberHoglins(c.Mem, m)
			}, jbrain.MemAvoidTarget.ID()),
		),
	}
}

// hoglinFindTarget is HoglinAi.findNearestValidAttackTarget: the nearest
// visible attackable player unless pacified or breeding.
func hoglinFindTarget(c *jbrain.Ctx, m *javamob.Mob) (world.Entity, bool) {
	if c.Mem.Has(jbrain.MemPacified.ID()) || c.Mem.Has(jbrain.MemBreedTarget.ID()) {
		return nil, false
	}
	h, ok := jbrain.Get(c.Mem, jbrain.MemNearestVisibleAttackablePlayer)
	if !ok || h == nil {
		return nil, false
	}
	return h.Entity(c.Tx)
}

// piglinsOutnumberHoglins is HoglinAi.piglinsOutnumberHoglins.
func piglinsOutnumberHoglins(mem *jbrain.Memories, m *javamob.Mob) bool {
	if m.Baby() {
		return false
	}
	return jbrain.GetOr(mem, jbrain.MemVisibleAdultPiglinCount, 0) > jbrain.GetOr(mem, jbrain.MemVisibleAdultHoglinCount, 0)+1
}

// Hoglin is the Hoglin class (an Animal and an Enemy).
type Hoglin struct {
	janimal.Base
	brain           *jmob.Brain
	attackAnim      int
	timeInOverworld int
	immune          bool
	cannotBeHunted  bool
}

// RegisterGoals makes the brain; the hoglin has no goals.
func (h *Hoglin) RegisterGoals(m *javamob.Mob) { h.brain = jmob.Attach(m, hoglinProvider, nil) }

// CanBeHunted is Hoglin.canBeHunted (piglins hunt adults).
func (h *Hoglin) CanBeHunted(m *javamob.Mob) bool { return !m.Baby() && !h.cannotBeHunted }

// SetCannotBeHunted marks a hoglin piglins never hunt (a bastion's).
func (h *Hoglin) SetCannotBeHunted(v bool) { h.cannotBeHunted = v }

// SetImmuneToZombification is Hoglin.setImmuneToZombification.
func (h *Hoglin) SetImmuneToZombification(v bool) { h.immune = v }

// FinalizeSpawnGroup is Hoglin.finalizeSpawn: one in five is a baby, then
// AgeableMob.finalizeSpawn.
func (h *Hoglin) FinalizeSpawnGroup(m *javamob.Mob, r javamob.SpawnReason, group any) any {
	if m.Rand().Float32() < 0.2 {
		h.A.SetBaby(true)
		m.SetBaby(true)
	}
	return h.Base.FinalizeSpawnGroup(m, r, group)
}

// SetBaby is the dimensions and Hoglin.ageBoundaryReached.
func (h *Hoglin) SetBaby(m *javamob.Mob, baby bool) {
	dmg := 6.0
	if baby {
		m.SetDimensions(0.75, 0.85)
		dmg = 0.5
	}
	m.Attributes().Instance(javamob.AttrAttackDamage).SetBase(dmg)
	m.SyncAttributes()
}

// XPReward is Hoglin.getBaseExperienceReward: 5, 3 for a baby.
func (h *Hoglin) XPReward(m *javamob.Mob, _ int) int {
	if m.Baby() {
		return 3
	}
	return 5
}

// RemoveWhenFarAway is Hoglin.removeWhenFarAway: true (unlike animals).
func (h *Hoglin) RemoveWhenFarAway(*javamob.Mob, float64) bool { return true }

// WalkTargetValue is Hoglin.getWalkTargetValue: away from repellents, onto
// crimson nylium.
func (h *Hoglin) WalkTargetValue(m *javamob.Mob, pos cube.Pos) float64 {
	if rep, ok := jbrain.Get(h.brain.Mem(), jbrain.MemNearestRepellent); ok && rep.Vec3().Sub(pos.Vec3()).LenSqr() < 64 {
		return -1
	}
	if name, _ := m.Tx().Block(pos.Side(cube.FaceDown)).EncodeBlock(); name == "minecraft:crimson_nylium" {
		return 10
	}
	return 0
}

// AiStep is Hoglin.aiStep: the attack animation timer.
func (h *Hoglin) AiStep(*javamob.Mob) {
	if h.attackAnim > 0 {
		h.attackAnim--
	}
}

// CustomServerAiStep is Hoglin.customServerAiStep after the brain tick:
// updateActivity, then zombification outside the Nether, then
// Animal.customServerAiStep.
func (h *Hoglin) CustomServerAiStep(m *javamob.Mob) {
	b := h.brain
	old, hadOld := b.ActiveNonCoreActivity()
	b.SetActiveActivityToFirstValid(jbrain.ActFight, jbrain.ActAvoid, jbrain.ActIdle)
	if a, ok := b.ActiveNonCoreActivity(); ok != hadOld || a != old {
		if snd := h.activitySound(m, a, ok); snd != "" {
			m.PlaySound(snd)
		}
	}
	c := jmob.RunCtx(m)
	t, ok := AttackTarget(c, b.Mem())
	m.SetAggressive(ok)
	if ok {
		m.SetTarget(t)
	} else if _, had := m.Mob.Target(); had {
		m.SetTarget(nil)
	}
	if h.converting(m) {
		h.timeInOverworld++
		if h.timeInOverworld > 300 {
			m.PlaySound("converted_to_zombified")
			m.ConvertTo(ZoglinID, true, func(n *javamob.Mob) {
				n.AddEffect(effect.New(effect.Nausea, 1, 10*time.Second))
			})
			return
		}
	} else {
		h.timeInOverworld = 0
	}
	h.Base.CustomServerAiStep(m)
}

// converting is Hoglin.isConverting: not immune, AI on, and piglins
// zombify here (every dimension but the Nether).
func (h *Hoglin) converting(m *javamob.Mob) bool {
	return !h.immune && !m.NoAI() && m.Tx().World().Dimension() != world.Nether
}

// activitySound is HoglinAi.getSoundForActivity.
func (h *Hoglin) activitySound(m *javamob.Mob, a jbrain.Activity, ok bool) string {
	switch {
	case !ok:
		return ""
	case a == jbrain.ActAvoid || h.converting(m):
		return "retreat"
	case a == jbrain.ActFight:
		return "angry"
	case h.brain.Mem().Has(jbrain.MemNearestRepellent.ID()):
		return "retreat"
	}
	return "ambient"
}

// MobInteract is Hoglin.mobInteract: Animal.mobInteract (crimson fungus,
// no love while pacified) and persistence when used.
func (h *Hoglin) MobInteract(m *javamob.Mob, user item.User, held item.Stack) (item.Stack, bool) {
	pacified := h.brain.Mem().Has(jbrain.MemPacified.ID())
	res, ok := janimal.InteractAnimal(m, &h.A, user, held, h.IsFood(held), !pacified && h.A.CanFallInLove())
	if ok {
		m.SetPersistenceRequired()
	}
	return res, ok
}

// BreedOffspring is Hoglin.getBreedOffspring: a persistent hoglin.
func (h *Hoglin) BreedOffspring(_, _ *janimal.Body) (string, func(*javamob.Mob), bool) {
	return HoglinID, func(n *javamob.Mob) { n.SetPersistenceRequired() }, true
}

// DoHurtTarget is Hoglin.doHurtTarget.
func (h *Hoglin) DoHurtTarget(m *javamob.Mob, target world.Entity) bool {
	if _, ok := target.(entity.Living); !ok {
		return false
	}
	h.attackAnim = 10
	broadcastHoglinEvent(m, hoglinEventAttack)
	m.PlaySound("attack")
	h.onHitTarget(m, target)
	return hurtAndThrowTarget(m, target)
}

// onHitTarget is HoglinAi.onHitTarget.
func (h *Hoglin) onHitTarget(m *javamob.Mob, target world.Entity) {
	if m.Baby() {
		return
	}
	if target.H().Type().EncodeEntity() == "minecraft:piglin" && piglinsOutnumberHoglins(h.brain.Mem(), m) {
		setAvoidTarget(m, h.brain.Mem(), target)
		h.forVisibleAdultHoglins(m, func(o *javamob.Mob, oh *Hoglin) { oh.retreatFromNearest(o, target) })
		return
	}
	h.broadcastAttackTarget(m, target)
}

func (h *Hoglin) forVisibleAdultHoglins(m *javamob.Mob, f func(o *javamob.Mob, oh *Hoglin)) {
	list, _ := jbrain.Get(h.brain.Mem(), jbrain.MemNearestVisibleAdultHoglins)
	for _, hh := range list {
		o, ok := javamob.OfHandle(m.Tx(), hh)
		if !ok {
			continue
		}
		if oh, ok := o.Behaviour().(*Hoglin); ok && oh.brain != nil {
			f(o, oh)
		}
	}
}

// nearestOf is BehaviorUtils.getNearestTarget(body, memory, other).
func nearestOf(m *javamob.Mob, mem *jbrain.Memories, id jbrain.Memory[*world.EntityHandle], other world.Entity) world.Entity {
	h, ok := jbrain.Get(mem, id)
	if !ok || h == nil {
		return other
	}
	cur, ok := h.Entity(m.Tx())
	if !ok {
		return other
	}
	p := m.Position()
	if cur.Position().Sub(p).LenSqr() < other.Position().Sub(p).LenSqr() {
		return cur
	}
	return other
}

// retreatFromNearest is HoglinAi.retreatFromNearestTarget.
func (h *Hoglin) retreatFromNearest(m *javamob.Mob, avoid world.Entity) {
	mem := h.brain.Mem()
	n := nearestOf(m, mem, jbrain.MemAvoidTarget, avoid)
	n = nearestOf(m, mem, jbrain.MemAttackTarget, n)
	setAvoidTarget(m, mem, n)
}

// setAvoidTarget is HoglinAi.setAvoidTarget: 5 to 20 seconds.
func setAvoidTarget(m *javamob.Mob, mem *jbrain.Memories, t world.Entity) {
	mem.Erase(jbrain.MemAttackTarget.ID())
	mem.Erase(jbrain.MemWalkTarget.ID())
	jbrain.SetWithExpiry(mem, jbrain.MemAvoidTarget, t.H(), int64(100+m.Rand().IntN(301)))
}

// setAttackTarget is HoglinAi.setAttackTarget.
func (h *Hoglin) setAttackTarget(t world.Entity) {
	mem := h.brain.Mem()
	mem.Erase(jbrain.MemCantReachWalkTargetSince.ID())
	mem.Erase(jbrain.MemBreedTarget.ID())
	jbrain.SetWithExpiry(mem, jbrain.MemAttackTarget, t.H(), 200)
}

// broadcastAttackTarget is HoglinAi.broadcastAttackTarget.
func (h *Hoglin) broadcastAttackTarget(m *javamob.Mob, t world.Entity) {
	h.forVisibleAdultHoglins(m, func(o *javamob.Mob, oh *Hoglin) {
		if oh.brain.Mem().Has(jbrain.MemPacified.ID()) {
			return
		}
		oh.setAttackTarget(nearestOf(o, oh.brain.Mem(), jbrain.MemAttackTarget, t))
	})
}

// OnHurt is Animal.actuallyHurt and HoglinAi.wasHurtBy.
func (h *Hoglin) OnHurt(m *javamob.Mob, dealt float64, src world.DamageSource) {
	h.Base.OnHurt(m, dealt, src)
	att, ok := jmob.SourceEntity(src)
	if !ok || !jmob.IsLiving(att) {
		return
	}
	mem := h.brain.Mem()
	mem.Erase(jbrain.MemPacified.ID())
	mem.Erase(jbrain.MemBreedTarget.ID())
	if m.Baby() {
		h.retreatFromNearest(m, att)
		return
	}
	id := att.H().Type().EncodeEntity()
	if h.brain.IsActive(jbrain.ActAvoid) && id == "minecraft:piglin" || id == HoglinID {
		return
	}
	c := jmob.RunCtx(m)
	if otherTargetMuchFurther(c, m, att, 4) || !jmob.IsEntityAttackable(c, m, att) {
		return
	}
	h.setAttackTarget(att)
	h.broadcastAttackTarget(m, att)
}

// Persister: the Animal fields and Hoglin's own.
func (h *Hoglin) EncodeNBT(data map[string]any) {
	h.Base.EncodeNBT(data)
	data["IsImmuneToZombification"] = boolNBT(h.immune)
	data["TimeInOverworld"] = int32(h.timeInOverworld)
	data["CannotBeHunted"] = boolNBT(h.cannotBeHunted)
}

func (h *Hoglin) DecodeNBT(m *javamob.Mob, data map[string]any) {
	h.Base.DecodeNBT(m, data)
	h.immune = data["IsImmuneToZombification"] == uint8(1)
	if v, ok := data["TimeInOverworld"].(int32); ok {
		h.timeInOverworld = int(v)
	}
	h.cannotBeHunted = data["CannotBeHunted"] == uint8(1)
}

func boolNBT(v bool) uint8 {
	if v {
		return 1
	}
	return 0
}

// hogBody is a hoglin as janimal's brain animal body, so that
// AnimalMakeLove and BabyFollowAdult run on the hoglin's brain (lift).
type hogBody struct{ *janimal.Body }

func hogOf(m *javamob.Mob) (*hogBody, bool) {
	b, ok := janimal.Wrap(m)
	if !ok {
		return nil, false
	}
	return &hogBody{b}, true
}

func (b *hogBody) Mem() *jbrain.Memories { return jmob.Mem(b.Mob) }
func (b *hogBody) Open(tx *world.Tx, h *world.EntityHandle) (*hogBody, bool) {
	m, ok := javamob.OfHandle(tx, h)
	if !ok || jmob.Mem(m) == nil {
		return nil, false
	}
	return hogOf(m)
}
func (b *hogBody) TypeID() string                              { return b.JavaType().ID }
func (b *hogBody) CanMate(p *hogBody) bool                     { return b.AnimalState().CanMate(p.AnimalState()) }
func (b *hogBody) PanicPos() (mgl64.Vec3, bool)                { return b.LandRandomPos(5, 4) }
func (b *hogBody) BreedWith(p *hogBody)                        { janimal.SpawnChildFromBreeding(b.Body, p.Body) }
func (b *hogBody) TemptingPlayer() (*world.EntityHandle, bool) { return nil, false }

// lifted runs a janimal brain behaviour on a *javamob.Mob brain.
type lifted struct{ in jbrain.Control[*hogBody] }

func lift(c jbrain.Control[*hogBody]) jmob.Control { return lifted{c} }

func (l lifted) State() jbrain.RunState              { return l.in.State() }
func (l lifted) Name() string                        { return l.in.Name() }
func (l lifted) RequiredMemories() []jbrain.MemoryID { return l.in.RequiredMemories() }
func (l lifted) TryStart(c *jbrain.Ctx, m *javamob.Mob) bool {
	b, ok := hogOf(m)
	return ok && l.in.TryStart(c, b)
}
func (l lifted) TickOrStop(c *jbrain.Ctx, m *javamob.Mob) {
	if b, ok := hogOf(m); ok {
		l.in.TickOrStop(c, b)
	}
}
func (l lifted) DoStop(c *jbrain.Ctx, m *javamob.Mob) {
	if b, ok := hogOf(m); ok {
		l.in.DoStop(c, b)
	}
}

// BecomePassiveIfMemoryPresent is BecomePassiveIfMemoryPresent.create.
func BecomePassiveIfMemoryPresent(mem jbrain.MemoryID, ticks int) *oneShot {
	return jmob.Trigger("BecomePassiveIfMemoryPresent", []jbrain.Req{
		jbrain.Reg(jbrain.MemAttackTarget), jbrain.Absent(jbrain.MemPacified), {Mem: mem, Status: jbrain.ValuePresent},
	}, func(c *jbrain.Ctx, _ *javamob.Mob) bool {
		jbrain.SetWithExpiry(c.Mem, jbrain.MemPacified, true, int64(ticks))
		c.Mem.Erase(jbrain.MemAttackTarget.ID())
		return true
	})
}

// EraseMemoryIf is EraseMemoryIf.create(predicate, memory).
func EraseMemoryIf(name string, pred func(c *jbrain.Ctx, m *javamob.Mob) bool, mem jbrain.MemoryID) *oneShot {
	return jmob.Trigger("EraseMemoryIf "+name, []jbrain.Req{{Mem: mem, Status: jbrain.ValuePresent}}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		if !pred(c, m) {
			return false
		}
		c.Mem.Erase(mem)
		return true
	})
}

// SetWalkTargetAwayFromPos is SetWalkTargetAwayFrom.pos(memory, speed,
// desiredDistance, interruptCurrentWalk): flee from the bottom centre of
// the block.
func SetWalkTargetAwayFromPos(mem jbrain.Memory[cube.Pos], speed float32, desired int, interrupt bool) *oneShot {
	return jmob.Trigger("SetWalkTargetAwayFrom", []jbrain.Req{jbrain.Reg(jbrain.MemWalkTarget), jbrain.Present(mem)}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		cur, hasCur := jbrain.Get(c.Mem, jbrain.MemWalkTarget)
		if hasCur && !interrupt {
			return false
		}
		p, _ := jbrain.Get(c.Mem, mem)
		avoid := mgl64.Vec3{float64(p[0]) + 0.5, float64(p[1]), float64(p[2]) + 0.5}
		body := m.Position()
		if body.Sub(avoid).LenSqr() >= float64(desired*desired) {
			return false
		}
		if hasCur && cur.Speed == speed && cur.Target.Position(c.Tx).Sub(body).Dot(avoid.Sub(body)) < 0 {
			return false
		}
		for range 10 {
			if flee, ok := m.LandRandomPosAway(16, 7, avoid); ok {
				jbrain.Set(c.Mem, jbrain.MemWalkTarget, jbrain.WalkTarget{Target: jbrain.TrackVec(flee), Speed: speed})
				break
			}
		}
		return true
	})
}
