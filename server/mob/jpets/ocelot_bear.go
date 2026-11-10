//go:build crossfly_full

package jpets

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/janimal"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// Ocelots (animal/feline/Ocelot.java) and polar bears
// (animal/polarbear/PolarBear.java).

func init() {
	javamob.Register(javamob.Type{
		ID: "minecraft:ocelot", Category: javamob.CategoryCreature, Width: 0.6, Height: 0.7,
		Attributes: withAttr(animalAttributes(10, f32(0.3)), javamob.AttrAttackDamage, 3),
		Families:   []string{"ocelot", "mob"},
		New:        func() javamob.Behaviour { return &Ocelot{} },
	})
	javamob.Register(javamob.Type{
		ID: "minecraft:polar_bear", Category: javamob.CategoryCreature, Width: 1.4, Height: 1.4,
		Attributes: func() map[javamob.Attribute]float64 {
			a := animalAttributes(30, 0.25)()
			a[javamob.AttrFollowRange] = 20
			a[javamob.AttrAttackDamage] = 6
			return a
		},
		Families: []string{"polarbear", "mob"},
		New:      func() javamob.Behaviour { return &PolarBear{Anger: javamob.NeutralMob{AngerTime: [2]int{400, 780}}} },
	})
}

// Ocelot is the behaviour of an ocelot.
type Ocelot struct {
	Animal   janimal.Animal
	Trusting bool

	avoid *catAvoidPlayers
	tempt *ocelotTempt
	panic *janimal.PanicGoal[petHost]
}

func (o *Ocelot) animal() *janimal.Animal { return &o.Animal }
func (o *Ocelot) panicking() bool         { return false }

func (o *Ocelot) breed(m, partner *javamob.Mob) bool {
	baby, err := javamob.Spawn(m.Tx(), "minecraft:ocelot", m.Position(), javamob.SpawnBreeding, javamob.SpawnOptions{})
	if err != nil {
		return false
	}
	baby.Behaviour().(*Ocelot).Animal.SetBaby(true)
	baby.SetBaby(true)
	return true
}

// RegisterGoals is Ocelot.registerGoals and reassessTrustingGoals.
func (o *Ocelot) RegisterGoals(m *javamob.Mob) {
	gs, ts := m.Goals(), m.Targets()
	o.tempt = &ocelotTempt{TemptGoal: janimal.NewTemptGoal[petHost](0.6, func(s item.Stack) bool { return IsCatFood(itemName(s)) }, true), o: o}
	gs.Add(1, goal.NewFloat(m))
	gs.Add(3, o.tempt)
	gs.Add(7, goal.NewLeapAtTarget(0.3))
	gs.Add(8, &ocelotAttack{})
	gs.Add(9, wrapGoal(&janimal.BreedGoal[petHost]{Speed: 0.8}))
	gs.Add(10, goal.NewWaterAvoidingRandomStroll(0.8, float32(1.0000001e-5)))
	gs.Add(11, goal.NewLookAtPlayer(10))
	ts.Add(1, goal.NewNearestAttackableTarget(func(e world.Entity) bool { return entityID(e) == "minecraft:chicken" }, false, false))
	ts.Add(1, goal.NewNearestAttackableTarget(isBabyTurtleOnLand, false, false))
	o.avoid = &catAvoidPlayers{AvoidEntity: goal.NewAvoidEntity(func(e world.Entity) bool {
		p, ok := e.(*player.Player)
		return ok && !p.GameMode().CreativeInventory() && p.GameMode() != world.GameModeSpectator
	}, 16, 0.8, 1.33), trusting: func() bool { return o.Trusting }}
	o.reassess(m)
}

// reassess is Ocelot.reassessTrustingGoals: a wild ocelot avoids players.
func (o *Ocelot) reassess(m *javamob.Mob) {
	if o.avoid == nil {
		return
	}
	m.Goals().Remove(m, o.avoid)
	if !o.Trusting {
		m.Goals().Add(4, o.avoid)
	}
}

// setTrusting is Ocelot.setTrusting (DATA_TRUSTING, the Bedrock trusting
// flag).
func (o *Ocelot) setTrusting(m *javamob.Mob, v bool) {
	o.Trusting = v
	m.SetActorFlag(protocol.EntityDataFlagTrusting, v)
	o.reassess(m)
}

// MobInteract is Ocelot.mobInteract: fish held out while the ocelot is
// tempted and within 3 blocks makes it trust the player one time in three
// (entity events 41 hearts, 40 smoke).
func (o *Ocelot) MobInteract(m *javamob.Mob, user item.User, held item.Stack) (item.Stack, bool) {
	p, ok := user.(*player.Player)
	if !ok {
		return held, false
	}
	if (o.tempt == nil || o.tempt.Running()) && !o.Trusting && IsCatFood(itemName(held)) && distSq(p.Position(), m.Position()) < 9 {
		left := consumeOne(user, held)
		if m.Rand().IntN(3) == 0 {
			o.setTrusting(m, true)
			broadcastEvent(m, tameSucceeded{})
		} else {
			broadcastEvent(m, tameFailed{})
		}
		return left, true
	}
	return animalFeed(m, &o.Animal, p, held, IsCatFood(itemName(held)))
}

// CustomServerAiStep is Ocelot.customServerAiStep: crouch tempted, sprint
// fleeing.
func (o *Ocelot) CustomServerAiStep(m *javamob.Mob) {
	sneak, sprint := false, false
	if mc := m.MoveControl(); mc.HasWanted() {
		switch mc.SpeedModifier() {
		case 0.6:
			sneak = true
		case 1.33:
			sprint = true
		}
	}
	m.UpdateMeta(func(meta *mob.Meta) { meta.Sneaking, meta.Sprinting = sneak, sprint })
}

// AiStep: age and love.
func (o *Ocelot) AiStep(m *javamob.Mob) { animalTick(m, &o.Animal) }

// RemoveWhenFarAway is Ocelot.removeWhenFarAway with Paper's patch: not a
// named or leashed ocelot.
func (o *Ocelot) RemoveWhenFarAway(m *javamob.Mob, distSqr float64) bool {
	return !o.Trusting && m.TickCount() > 2400 && m.NameTag() == "" && !leashed(m)
}

func (o *Ocelot) EncodeNBT(data map[string]any) {
	o.Animal.EncodeNBT(data)
	data["Trusting"] = boolByte(o.Trusting)
}

func (o *Ocelot) DecodeNBT(m *javamob.Mob, data map[string]any) {
	o.Animal.DecodeNBT(data)
	m.SetBaby(o.Animal.Baby())
	o.setTrusting(m, janimal.NBTBool(data, "Trusting", false))
}

// ocelotTempt is Ocelot.OcelotTemptGoal: a trusting ocelot is not scared.
type ocelotTempt struct {
	*janimal.TemptGoal[petHost]
	o *Ocelot
}

func (g *ocelotTempt) Flags() javamob.Flags { return javamob.Flags(g.TemptGoal.Flags()) }
func (g *ocelotTempt) CanUse(m *javamob.Mob) bool {
	g.TemptGoal.CanScare = !g.o.Trusting
	return g.TemptGoal.CanUse(hostOf(m))
}
func (g *ocelotTempt) CanContinueToUse(m *javamob.Mob) bool {
	g.TemptGoal.CanScare = !g.o.Trusting
	return g.TemptGoal.CanContinueToUse(hostOf(m))
}
func (g *ocelotTempt) Start(m *javamob.Mob) { g.TemptGoal.Start(hostOf(m)) }
func (g *ocelotTempt) Stop(m *javamob.Mob)  { g.TemptGoal.Stop(hostOf(m)) }
func (g *ocelotTempt) Tick(m *javamob.Mob)  { g.TemptGoal.Tick(hostOf(m)) }

// ---- polar bear ----

// PolarBear is the behaviour of a polar bear.
type PolarBear struct {
	Animal            janimal.Animal
	Anger             javamob.NeutralMob
	standing          bool
	warningSoundTicks int
	panic             *janimal.PanicGoal[petHost]
	self              *world.EntityHandle
}

func (b *PolarBear) animal() *janimal.Animal { return &b.Animal }
func (b *PolarBear) panicking() bool         { return b.panic != nil && b.panic.Running() }

func (b *PolarBear) breed(m, partner *javamob.Mob) bool {
	baby, err := javamob.Spawn(m.Tx(), "minecraft:polar_bear", m.Position(), javamob.SpawnBreeding, javamob.SpawnOptions{})
	if err != nil {
		return false
	}
	baby.Behaviour().(*PolarBear).Animal.SetBaby(true)
	baby.SetBaby(true)
	return true
}

// setStanding is PolarBear.setStanding (the Bedrock standing flag).
func (b *PolarBear) setStanding(m *javamob.Mob, v bool) {
	b.standing = v
	if m.ActorFlag(protocol.EntityDataFlagStanding) != v {
		m.SetActorFlag(protocol.EntityDataFlagStanding, v)
	}
}

// RegisterGoals is PolarBear.registerGoals.
func (b *PolarBear) RegisterGoals(m *javamob.Mob) {
	b.self = m.H()
	gs, ts := m.Goals(), m.Targets()
	gs.Add(0, goal.NewFloat(m))
	melee := goal.NewMeleeAttack(1.25, true)
	melee.Attack = b.checkAndPerformAttack
	melee.OnStop = func(m *javamob.Mob, _ *goal.MeleeAttack) { b.setStanding(m, false) }
	gs.Add(1, melee)
	b.panic = &janimal.PanicGoal[petHost]{Speed: 2, ShouldPanic: func(h petHost) bool {
		t := h.LastDamageType()
		if h.m.Baby() {
			_, ok := janimal.PanicCauses[t]
			return ok
		}
		return environmentalPanic[t]
	}}
	gs.Add(1, wrapGoal(b.panic))
	gs.Add(4, wrapGoal(&janimal.FollowParentGoal[petHost]{Speed: 1.25}))
	gs.Add(5, goal.NewRandomStroll(1))
	gs.Add(6, goal.NewLookAtPlayer(6))
	gs.Add(7, goal.NewRandomLookAround())

	hurt := goal.NewHurtByTarget()
	hurt.AlertOther = func(_, other *javamob.Mob, target world.Entity) {
		if _, ok := other.Behaviour().(*PolarBear); ok && !other.Baby() {
			other.SetTarget(target)
		}
	}
	ts.Add(1, &bearHurtBy{HurtByTarget: hurt})
	ts.Add(2, &bearAttackPlayers{})
	angry := goal.NewNearestAttackableTarget(javamob.IsPlayer, true, false)
	angry.Selector = func(e world.Entity, tx *world.Tx) bool {
		jm, ok := javamob.OfHandle(tx, b.self)
		return ok && b.Anger.IsAngryAt(jm, e)
	}
	ts.Add(3, angry)
	foxes := goal.NewNearestAttackableTarget(func(e world.Entity) bool { return entityID(e) == "minecraft:fox" }, true, true)
	foxes.Selector = func(_ world.Entity, tx *world.Tx) bool {
		jm, ok := javamob.OfHandle(tx, b.self)
		return ok && !jm.Baby()
	}
	ts.Add(4, foxes)
}

// checkAndPerformAttack is PolarBearMeleeAttackGoal.checkAndPerformAttack:
// stand up and growl in the last 10 ticks before a bite.
func (b *PolarBear) checkAndPerformAttack(m *javamob.Mob, g *goal.MeleeAttack, t world.Entity) {
	if g.TicksUntilNextAttack() <= 0 && m.IsWithinMeleeAttackRange(t) && m.Sensing().HasLineOfSight(m, t) {
		g.ResetAttackCooldown()
		m.DoHurtTarget(t)
		b.setStanding(m, false)
		return
	}
	w := t.H().Type().BBox(t).Width() + 3
	if distSq(m.Position(), t.Position()) < w*w {
		if g.TicksUntilNextAttack() <= 0 {
			b.setStanding(m, false)
			g.ResetAttackCooldown()
		}
		if g.TicksUntilNextAttack() <= 10 {
			b.setStanding(m, true)
			if b.warningSoundTicks <= 0 {
				m.PlaySound("warning")
				b.warningSoundTicks = 40
			}
		}
		return
	}
	g.ResetAttackCooldown()
	b.setStanding(m, false)
}

// PostTick is PolarBear.tick: the warning sound cooldown and anger.
func (b *PolarBear) PostTick(m *javamob.Mob) {
	if b.warningSoundTicks > 0 {
		b.warningSoundTicks--
	}
	b.Anger.UpdatePersistentAnger(m, true)
	if a := b.Anger.IsAngry(m); m.Meta().Angry != a {
		m.UpdateMeta(func(meta *mob.Meta) { meta.Angry = a })
	}
}

// AiStep: age and love.
func (b *PolarBear) AiStep(m *javamob.Mob) { animalTick(m, &b.Animal) }

// OnHurt: love ends.
func (b *PolarBear) OnHurt(m *javamob.Mob, dealt float64, src world.DamageSource) {
	b.Animal.ResetLove()
}

// MobInteract: polar bears eat nothing (isFood false).
func (b *PolarBear) MobInteract(m *javamob.Mob, user item.User, held item.Stack) (item.Stack, bool) {
	return held, false
}

func (b *PolarBear) EncodeNBT(data map[string]any) {
	b.Animal.EncodeNBT(data)
	b.Anger.EncodeAnger(data)
}

func (b *PolarBear) DecodeNBT(m *javamob.Mob, data map[string]any) {
	b.Animal.DecodeNBT(data)
	m.SetBaby(b.Animal.Baby())
	b.Anger.DecodeAnger(m, data)
}

// bearHurtBy is PolarBearHurtByTargetGoal: a hurt cub alerts the grown bears
// around and does not fight itself.
type bearHurtBy struct{ *goal.HurtByTarget }

func (g *bearHurtBy) Start(m *javamob.Mob) {
	g.AlertOthers = m.Baby()
	g.HurtByTarget.Start(m)
	if m.Baby() {
		g.HurtByTarget.Stop(m)
	}
}

// bearAttackPlayers is PolarBearAttackPlayersGoal: an adult bear with a cub
// within 8 x 4 x 8 attacks players within half its follow range (mustSee,
// mustReach).
type bearAttackPlayers struct {
	goal.Target
	found *world.EntityHandle
}

func (g *bearAttackPlayers) Flags() javamob.Flags { return javamob.FlagTarget }

func (g *bearAttackPlayers) CanUse(m *javamob.Mob) bool {
	g.MustSee, g.MustReach, g.UnseenMemoryTicks = true, true, 60
	if m.Baby() || m.Rand().IntN(javamob.ReducedTickDelay(20)) != 0 {
		return false
	}
	p, ok := m.NearestPlayer(javamob.ForCombat(m.FollowRange() * 0.5))
	if !ok {
		return false
	}
	cub := false
	for e := range m.Tx().EntitiesWithin(m.BBox().GrowVec3(mgl64.Vec3{8, 4, 8})) {
		if o, ok := javamob.Of(e); ok && entityID(e) == "minecraft:polar_bear" && o.Baby() {
			cub = true
			break
		}
	}
	if !cub {
		return false
	}
	g.found = p.H()
	return true
}

func (g *bearAttackPlayers) Start(m *javamob.Mob) {
	if t, ok := g.found.Entity(m.Tx()); ok {
		m.SetTarget(t)
	}
	g.Target.Start(m)
}
