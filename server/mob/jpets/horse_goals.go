//go:build crossfly_full

package jpets

import (
	"math"

	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/janimal"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func newEquine(k equineKind) javamob.Behaviour {
	h := &Equine{kind: k}
	if k == kindLlama {
		h.llama.Strength = 1
	}
	h.createInventory()
	return h
}

// RegisterGoals is AbstractHorse.registerGoals with addBehaviourGoals, and
// Llama.registerGoals.
func (h *Equine) RegisterGoals(m *javamob.Mob) {
	gs, ts := m.Goals(), m.Targets()
	breed := wrapGoal(&janimal.BreedGoal[petHost]{Speed: 1, CanMate: func(a, b petHost) bool {
		return equineCanMate(a.m, b.m)
	}})
	if h.kind == kindLlama {
		// Llama: getNavigation().setRequiredPathLength(40).
		m.Navigation().SetRequiredPathLength(m, 40)
		gs.Add(0, goal.NewFloat(m))
		gs.Add(1, &runAroundLikeCrazy{h: h, speed: 1.2})
		gs.Add(2, &llamaFollowCaravan{h: h, speed: 2.1})
		gs.Add(3, &rangedAttack{speed: 1.25, intervalMin: 40, intervalMax: 40, radius: 20, attack: h.spit})
		gs.Add(3, h.panicGoal(1.2, nil))
		gs.Add(4, breed)
		gs.Add(5, wrapGoal(janimal.NewTemptGoal[petHost](1.25, stackNamed("minecraft:hay_block"), false)))
		gs.Add(6, wrapGoal(&janimal.FollowParentGoal[petHost]{Speed: 1}))
		gs.Add(7, goal.NewWaterAvoidingRandomStroll(0.7, 0.001))
		gs.Add(8, goal.NewLookAtPlayer(6))
		gs.Add(9, goal.NewRandomLookAround())
		ts.Add(1, &llamaHurtBy{HurtByTarget: goal.NewHurtByTarget(), h: h})
		ts.Add(2, newLlamaAttackWolf())
		return
	}
	gs.Add(1, &runAroundLikeCrazy{h: h, speed: 1.2})
	gs.Add(2, breed)
	gs.Add(4, wrapGoal(&janimal.FollowParentGoal[petHost]{Speed: 1}))
	gs.Add(6, goal.NewWaterAvoidingRandomStroll(0.7, 0.001))
	gs.Add(7, goal.NewLookAtPlayer(6))
	gs.Add(8, goal.NewRandomLookAround())
	if h.info().canRear {
		gs.Add(9, &randomStand{h: h, nextStand: -400})
	}
	switch h.kind {
	case kindSkeletonHorse:
		// SkeletonHorse.addBehaviourGoals: none.
	case kindZombieHorse:
		gs.Add(0, goal.NewFloat(m))
		gs.Add(3, wrapGoal(janimal.NewTemptGoal[petHost](1.25, stackNamed("minecraft:red_mushroom"), false)))
	default:
		gs.Add(0, goal.NewFloat(m))
		gs.Add(1, h.panicGoal(1.2, func(petHost) bool { return h.mobControlled(m) }))
		gs.Add(3, wrapGoal(janimal.NewTemptGoal[petHost](1.25, func(s item.Stack) bool { return IsHorseTempt(itemName(s)) }, false)))
	}
	if h.kind == kindSkeletonHorse && h.skeleton.trap {
		h.skeleton.goal = &skeletonTrap{h: h}
		gs.Add(1, h.skeleton.goal)
	}
}

// mobControlled is AbstractHorse.isMobControlled (ZombieHorse: a mob rides
// it); MountPanicGoal does not panic then.
func (h *Equine) mobControlled(m *javamob.Mob) bool {
	if h.kind != kindZombieHorse {
		return false
	}
	ps := m.Passengers()
	if len(ps) == 0 {
		return false
	}
	_, isPlayer := ps[0].(*player.Player)
	return !isPlayer
}

// equineCanMate is the canMate of each equine: horses and donkeys with
// either (canParent on both), llamas with llamas, never mules, skeleton or
// zombie horses (AbstractHorse.canMate false).
func equineCanMate(a, b *javamob.Mob) bool {
	ha, ok1 := a.Behaviour().(*Equine)
	hb, ok2 := b.Behaviour().(*Equine)
	if !ok1 || !ok2 || a.H() == b.H() {
		return false
	}
	if _, ok := EquineFoalType(equines[ha.kind].id, equines[hb.kind].id); !ok {
		return false
	}
	return ha.canParent(a) && hb.canParent(b)
}

// canParent is AbstractHorse.canParent: not ridden or riding, tamed, adult,
// full health, in love.
func (h *Equine) canParent(m *javamob.Mob) bool {
	return len(m.Passengers()) == 0 && !m.Riding() && h.Tame && !m.Baby() && m.Health() >= m.MaxHealth() && h.Animal.InLove()
}

// sameBreedClass is the class BreedGoal looks for partners of:
// AbstractHorse for horses and donkeys (BreedGoal(this, 1.0,
// AbstractHorse.class)), Llama for llamas.
func (h *Equine) sameBreedClass(e world.Entity) bool {
	jm, ok := javamob.Of(e)
	if !ok {
		return false
	}
	o, ok := jm.Behaviour().(*Equine)
	return ok && (o.kind == kindLlama) == (h.kind == kindLlama)
}

// animal, panicking and breed implement Animalish.
func (h *Equine) animal() *janimal.Animal { return &h.Animal }
func (h *Equine) panicking() bool         { return h.panic != nil && h.panic.Running() }

// breed is getBreedOffspring + spawnChildFromBreeding for equines: the foal
// type, the inherited stats and looks.
func (h *Equine) breed(m, partner *javamob.Mob) bool {
	pb := partner.Behaviour().(*Equine)
	typ, ok := EquineFoalType(equines[h.kind].id, equines[pb.kind].id)
	if !ok {
		return false
	}
	baby, err := javamob.Spawn(m.Tx(), typ, m.Position(), javamob.SpawnBreeding, javamob.SpawnOptions{})
	if err != nil {
		return false
	}
	bb, ok := baby.Behaviour().(*Equine)
	if !ok {
		return false
	}
	// setOffspringAttributes: health, jump strength, speed.
	r := m.Rand()
	stats := func(x *javamob.Mob) HorseAttributes {
		a := x.Attributes()
		return HorseAttributes{MaxHealth: a.Base(javamob.AttrMaxHealth), JumpStrength: a.Base(javamob.AttrJumpStrength), Speed: a.Base(javamob.AttrMovementSpeed)}
	}
	if bb.kind != kindMule || true {
		s := OffspringAttributes(stats(m), stats(partner), r)
		ba := baby.Attributes()
		ba.Instance(javamob.AttrMaxHealth).SetBase(s.MaxHealth)
		ba.Instance(javamob.AttrJumpStrength).SetBase(s.JumpStrength)
		ba.Instance(javamob.AttrMovementSpeed).SetBase(s.Speed)
		baby.SyncAttributes()
		baby.SetHealth(baby.MaxHealth())
	}
	switch bb.kind {
	case kindHorse:
		v, mk := HorseFoalLook(r, h.Variant, h.Markings, pb.Variant, pb.Markings)
		bb.setLook(baby, v, mk)
	case kindLlama:
		s, v := LlamaBabyLook(r, h.llama.Strength, pb.llama.Strength, h.Variant, pb.Variant)
		bb.llama.Strength = s
		bb.setLook(baby, v, 0)
	}
	bb.Bred = true // AbstractHorse: the foal follows its mother
	bb.Animal.SetBaby(true)
	baby.SetBaby(true)
	baby.SetPersistenceRequired()
	return true
}

// panicGoal is TamableAnimalPanicGoal / MountPanicGoal / PanicGoal.
func (h *Equine) panicGoal(speed float64, skip func(petHost) bool) javamob.Goal {
	h.panic = &janimal.PanicGoal[petHost]{Speed: speed}
	if skip != nil {
		h.panic.ShouldPanic = func(p petHost) bool {
			if skip(p) {
				return false
			}
			_, ok := janimal.PanicCauses[p.LastDamageType()]
			return ok
		}
	}
	return wrapGoal(h.panic)
}

// runAroundLikeCrazy is RunAroundLikeCrazyGoal (MOVE): a wild mount with a
// rider runs to random spots; now and then (1 in 25 goal ticks) it either
// accepts a player rider (random < temper) or throws the rider off, rears and
// gets 5 more temper.
type runAroundLikeCrazy struct {
	h     *Equine
	speed float64
	pos   [3]float64
}

func (g *runAroundLikeCrazy) Flags() javamob.Flags { return javamob.FlagMove }

func (g *runAroundLikeCrazy) CanUse(m *javamob.Mob) bool {
	if g.h.mobControlled(m) || g.h.Tame || len(m.Passengers()) == 0 {
		return false
	}
	p, ok := m.DefaultRandomPos(5, 4)
	if !ok {
		return false
	}
	g.pos = [3]float64{p[0], p[1], p[2]}
	return true
}

func (g *runAroundLikeCrazy) Start(m *javamob.Mob) {
	m.Navigation().MoveTo(m, mgl64.Vec3(g.pos), 0, g.speed)
}

func (g *runAroundLikeCrazy) CanContinueToUse(m *javamob.Mob) bool {
	return !g.h.Tame && !m.Navigation().Done() && len(m.Passengers()) > 0
}

func (g *runAroundLikeCrazy) Tick(m *javamob.Mob) {
	if g.h.Tame || m.Rand().IntN(javamob.ReducedTickDelay(50)) != 0 {
		return
	}
	ps := m.Passengers()
	if len(ps) == 0 {
		return
	}
	if p, ok := ps[0].(*player.Player); ok {
		if TameByRiding(m.Rand(), g.h.Temper, g.h.maxTemper()) {
			g.h.tameWithName(m, p)
			return
		}
		g.h.Temper = ModifyTemper(g.h.Temper, 5, g.h.maxTemper())
	}
	m.EjectPassengers()
	g.h.makeMad(m)
	broadcastEvent(m, tameFailed{})
}

// tameWithName is AbstractHorse.tameWithName: owned by the player, tamed,
// hearts.
func (h *Equine) tameWithName(m *javamob.Mob, p *player.Player) {
	m.SetOwner(p)
	h.setTamed(m, true)
	m.SetPersistenceRequired()
	broadcastEvent(m, tameSucceeded{})
}

// randomStand is RandomStandGoal (no flags, every tick): now and then, once
// per ambient sound interval (400 ticks) on average, the horse rears with
// its ambient sound.
type randomStand struct {
	h         *Equine
	nextStand int
}

func (g *randomStand) Flags() javamob.Flags               { return 0 }
func (g *randomStand) RequiresUpdateEveryTick() bool      { return true }
func (g *randomStand) CanContinueToUse(*javamob.Mob) bool { return false }

func (g *randomStand) CanUse(m *javamob.Mob) bool {
	g.nextStand++
	if g.nextStand > 0 && m.Rand().IntN(1000) < g.nextStand {
		g.nextStand = -400
		return !g.h.immobile(m) && m.Rand().IntN(10) == 0
	}
	return false
}

func (g *randomStand) Start(m *javamob.Mob) {
	g.h.standIfPossible(m)
	m.PlaySound("ambient")
}

// immobile is AbstractHorse.isImmobile: eating or rearing (or dead/ridden
// saddled, which the base checks).
func (h *Equine) immobile(m *javamob.Mob) bool {
	if h.kind == kindLlama {
		return m.Dead() || h.eating
	}
	return h.eating || h.standing
}

// rangedAttack is RangedAttackGoal (MOVE, LOOK, every tick), the llama's
// spitting.
type rangedAttack struct {
	speed                    float64
	intervalMin, intervalMax int
	radius                   float64
	attack                   func(m *javamob.Mob, target world.Entity, power float64)

	target     *world.EntityHandle
	attackTime int
	seeTime    int
}

func (g *rangedAttack) Flags() javamob.Flags          { return javamob.FlagMove | javamob.FlagLook }
func (g *rangedAttack) RequiresUpdateEveryTick() bool { return true }

func (g *rangedAttack) CanUse(m *javamob.Mob) bool {
	t, ok := m.Target()
	if !ok || dead(t) {
		return false
	}
	g.target = t.H()
	return true
}

func (g *rangedAttack) CanContinueToUse(m *javamob.Mob) bool {
	if g.CanUse(m) {
		return true
	}
	t, ok := g.target.Entity(m.Tx())
	return ok && !dead(t) && !m.Navigation().Done()
}

func (g *rangedAttack) Start(*javamob.Mob) { g.attackTime = -1 }

func (g *rangedAttack) Stop(*javamob.Mob) {
	g.target, g.seeTime, g.attackTime = nil, 0, -1
}

func (g *rangedAttack) Tick(m *javamob.Mob) {
	t, ok := g.target.Entity(m.Tx())
	if !ok {
		return
	}
	d := distSq(m.Position(), t.Position())
	los := m.Sensing().HasLineOfSight(m, t)
	if los {
		g.seeTime++
	} else {
		g.seeTime = 0
	}
	if d <= g.radius*g.radius && g.seeTime >= 5 {
		m.Navigation().Stop()
	} else {
		m.Navigation().MoveToEntity(m, t, g.speed)
	}
	m.LookControl().SetLookAtEntity(t, 30, 30)
	g.attackTime--
	switch {
	case g.attackTime == 0:
		if !los {
			return
		}
		dist := float32(math.Sqrt(d)) / float32(g.radius)
		power := min(max(dist, 0.1), 1)
		g.attack(m, t, float64(power))
		g.attackTime = int(math.Floor(float64(dist*float32(g.intervalMax-g.intervalMin) + float32(g.intervalMin))))
	case g.attackTime < 0:
		f := math.Sqrt(d) / g.radius
		g.attackTime = int(math.Floor(float64(g.intervalMin) + f*float64(g.intervalMax-g.intervalMin)))
	}
}

// llamaHurtBy is Llama.LlamaHurtByTargetGoal: the target is dropped right
// after a spit.
type llamaHurtBy struct {
	*goal.HurtByTarget
	h *Equine
}

func (g *llamaHurtBy) CanContinueToUse(m *javamob.Mob) bool {
	if g.h.llama.didSpit {
		g.h.llama.didSpit = false
		return false
	}
	return g.HurtByTarget.CanContinueToUse(m)
}

// llamaAttackWolf is Llama.LlamaAttackWolfGoal: NearestAttackableTargetGoal
// for wild wolves (mustSee false, mustReach true) within a quarter of the
// follow range (getFollowDistance() * 0.25).
type llamaAttackWolf struct {
	goal.Target
	found *world.EntityHandle
}

func newLlamaAttackWolf() *llamaAttackWolf {
	return &llamaAttackWolf{Target: goal.Target{GoalBase: javamob.GoalBase{Control: javamob.FlagTarget}, MustReach: true, UnseenMemoryTicks: 60}}
}

func (g *llamaAttackWolf) CanUse(m *javamob.Mob) bool {
	if m.Rand().IntN(javamob.ReducedTickDelay(10)) != 0 {
		return false
	}
	r := m.FollowRange() * 0.25
	c := javamob.ForCombat(r)
	c.IgnoreLineOfSight = true // mustSee false
	c.Selector = func(e world.Entity, _ *world.Tx) bool {
		jm, ok := javamob.Of(e)
		if !ok {
			return false
		}
		w, ok := jm.Behaviour().(*Wolf)
		return ok && !w.Tame
	}
	t, ok := m.NearestEntity(m.BBox().GrowVec3(mgl64.Vec3{r, 4, r}), func(e world.Entity) bool {
		return entityID(e) == "minecraft:wolf"
	}, c)
	if !ok {
		return false
	}
	g.found = t.H()
	return true
}

func (g *llamaAttackWolf) Start(m *javamob.Mob) {
	if t, ok := g.found.Entity(m.Tx()); ok {
		m.SetTarget(t)
	}
	g.Target.Start(m)
}

// ---- skeleton horse trap ----

// skeletonState is SkeletonHorse's trap fields.
type skeletonState struct {
	trap     bool
	trapTime int
	goal     *skeletonTrap
}

// skeletonAiStep is SkeletonHorse.aiStep: a trap horse nobody claimed
// vanishes after 18000 ticks.
func (h *Equine) skeletonAiStep(m *javamob.Mob) {
	if !m.PersistenceRequired() && h.skeleton.trap {
		h.skeleton.trapTime++
		if h.skeleton.trapTime >= 18000 {
			_ = m.Close()
		}
	}
}

// SetTrap is SkeletonHorse.setTrap: the trap goal runs while it is a trap.
func (h *Equine) SetTrap(m *javamob.Mob, trap bool) {
	if trap == h.skeleton.trap {
		return
	}
	h.skeleton.trap = trap
	if trap {
		h.skeleton.goal = &skeletonTrap{h: h}
		m.Goals().Add(1, h.skeleton.goal)
	} else if h.skeleton.goal != nil {
		m.Goals().Remove(m, h.skeleton.goal)
	}
}

func stackNamed(name string) func(item.Stack) bool {
	return func(s item.Stack) bool { return itemName(s) == name }
}

// EncodeNBT is AbstractHorse/AbstractChestedHorse/Horse/Llama/SkeletonHorse
// addAdditionalSaveData (Java keys; the saddle, which Java keeps in the
// equipment, as jpets:Saddle).
func (h *Equine) EncodeNBT(data map[string]any) {
	h.Animal.EncodeNBT(data)
	data["EatingHaystack"] = boolByte(h.eating)
	data["Bred"] = boolByte(h.Bred)
	data["Temper"] = int32(h.Temper)
	data["Tame"] = boolByte(h.Tame)
	if !h.Saddle.Empty() {
		data["jpets:Saddle"] = item.WriteNBT(h.Saddle, true)
	}
	switch h.kind {
	case kindHorse:
		data["Variant"] = int32(h.Variant&0xff | h.Markings<<8&0xff00)
	case kindLlama:
		data["Variant"] = int32(h.Variant)
		data["Strength"] = int32(h.llama.Strength)
	case kindSkeletonHorse:
		data["SkeletonTrap"] = boolByte(h.skeleton.trap)
		data["SkeletonTrapTime"] = int32(h.skeleton.trapTime)
	}
	if h.info().chested {
		data["ChestedHorse"] = boolByte(h.Chest)
		if h.Chest && h.inv != nil {
			data["Items"] = nbtconv.InvToNBT(h.inv)
		}
	}
}

// DecodeNBT is readAdditionalSaveData.
func (h *Equine) DecodeNBT(m *javamob.Mob, data map[string]any) {
	h.Animal.DecodeNBT(data)
	m.SetBaby(h.Animal.Baby())
	h.setEating(m, janimal.NBTBool(data, "EatingHaystack", false))
	h.Bred = janimal.NBTBool(data, "Bred", false)
	h.Temper = int(janimal.NBTInt(data, "Temper", 0))
	h.setTamed(m, janimal.NBTBool(data, "Tame", false))
	if s, ok := data["jpets:Saddle"].(map[string]any); ok {
		h.setSaddle(m, item.ReadNBT(s, nil))
	}
	switch h.kind {
	case kindHorse:
		v := int(janimal.NBTInt(data, "Variant", 0))
		h.setLook(m, v&0xff%HorseVariants, (v&0xff00>>8)%HorseMarkings)
	case kindLlama:
		h.llama.Strength = ClampLlamaStrength(int(janimal.NBTInt(data, "Strength", 0)))
		h.setLook(m, min(max(int(janimal.NBTInt(data, "Variant", 0)), 0), LlamaVariants-1), 0)
	case kindSkeletonHorse:
		h.SetTrap(m, janimal.NBTBool(data, "SkeletonTrap", false))
		h.skeleton.trapTime = int(janimal.NBTInt(data, "SkeletonTrapTime", 0))
	}
	if h.info().chested {
		h.setChest(m, janimal.NBTBool(data, "ChestedHorse", false))
		if items, ok := data["Items"].([]any); ok && h.inv != nil {
			nbtconv.InvFromNBT(h.inv, items)
		}
	}
}
