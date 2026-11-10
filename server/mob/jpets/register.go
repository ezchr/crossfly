//go:build crossfly_full

package jpets

import (
	"math"

	"github.com/df-mc/dragonfly/server/mob/janimal"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/mob/javamob/path"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// The Java types of this package (EntityTypes builders and each class's
// createAttributes / registerGoals).

func animalAttributes(health, speed float64) func() map[javamob.Attribute]float64 {
	return func() map[javamob.Attribute]float64 {
		a := javamob.LivingAttributes()
		a[javamob.AttrTemptRange] = 10 // Animal.createAnimalAttributes
		a[javamob.AttrMaxHealth] = health
		a[javamob.AttrMovementSpeed] = speed
		return a
	}
}

func withAttr(f func() map[javamob.Attribute]float64, k javamob.Attribute, v float64) func() map[javamob.Attribute]float64 {
	return func() map[javamob.Attribute]float64 {
		a := f()
		a[k] = v
		return a
	}
}

func horseAttributes(base HorseAttributes, health, speed float64) func() map[javamob.Attribute]float64 {
	return func() map[javamob.Attribute]float64 {
		a := animalAttributes(base.MaxHealth, base.Speed)()
		a[javamob.AttrJumpStrength] = base.JumpStrength
		a[javamob.AttrStepHeight] = base.StepHeight
		a[javamob.AttrSafeFallDistance] = base.SafeFall
		a[javamob.AttrFallDamageMultiplier] = base.FallMult
		if health > 0 {
			a[javamob.AttrMaxHealth] = health
		}
		if speed > 0 {
			a[javamob.AttrMovementSpeed] = speed
		}
		return a
	}
}

func init() {
	javamob.Register(javamob.Type{
		ID: "minecraft:wolf", Category: javamob.CategoryCreature, Width: 0.6, Height: 0.85, EyeHeight: 0.68,
		Attributes: withAttr(animalAttributes(8, f32(0.3)), javamob.AttrAttackDamage, 4),
		Families:   []string{"wolf", "mob"},
		New:        newWolf,
	})
	javamob.Register(javamob.Type{
		ID: "minecraft:cat", Category: javamob.CategoryCreature, Width: 0.6, Height: 0.7, EyeHeight: 0.35,
		Attributes: withAttr(animalAttributes(10, f32(0.3)), javamob.AttrAttackDamage, 3),
		Families:   []string{"cat", "mob"},
		New:        newCat,
	})
	horse := func(k equineKind, w, h, eye float64, cat javamob.Category, attrs func() map[javamob.Attribute]float64, fam ...string) {
		javamob.Register(javamob.Type{
			ID: equines[k].id, Category: cat, Width: w, Height: h, EyeHeight: eye,
			Attributes: attrs, Families: append(fam, "mob"),
			Components: []string{MountComponent},
			New:        func() javamob.Behaviour { return newEquine(k) },
		})
	}
	horse(kindHorse, f32(1.3964844), 1.6, f32(1.52), javamob.CategoryCreature, horseAttributes(BaseHorseAttributes, 0, 0), "horse")
	horse(kindDonkey, f32(1.3964844), 1.5, f32(1.425), javamob.CategoryCreature, horseAttributes(BaseChestedAttributes, 0, 0), "donkey")
	horse(kindMule, f32(1.3964844), 1.6, f32(1.52), javamob.CategoryCreature, horseAttributes(BaseChestedAttributes, 0, 0), "mule")
	horse(kindSkeletonHorse, f32(1.3964844), 1.6, f32(1.52), javamob.CategoryCreature, horseAttributes(BaseHorseAttributes, 15, f32(0.2)), "skeletonhorse", "undead")
	horse(kindZombieHorse, f32(1.3964844), 1.6, f32(1.52), javamob.CategoryMonster, horseAttributes(BaseHorseAttributes, 25, 0), "zombie_horse", "undead")
	horse(kindLlama, 0.9, 1.87, f32(1.7765), javamob.CategoryCreature, horseAttributes(BaseChestedAttributes, 0, 0), "llama")
}

// ---- wolf goals (Wolf.registerGoals) ----

// RegisterGoals is Wolf.registerGoals.
func (w *Wolf) RegisterGoals(m *javamob.Mob) {
	m.SetPathfindingMalus(path.PowderSnow, -1)
	m.SetPathfindingMalus(path.OnTopOfPowderSnow, -1)
	w.SetTame(m, false, false)
	w.Anger.AngerTime = [2]int{400, 780} // TimeUtil.rangeOfSeconds(20, 39)

	gs, ts := m.Goals(), m.Targets()
	gs.Add(1, goal.NewFloat(m))
	gs.Add(1, w.panicGoal(1.5, environmentalPanic))
	gs.Add(2, &sitWhenOrdered{t: &w.Tamable})
	gs.Add(3, &wolfAvoidLlama{w: w, AvoidEntity: goal.NewAvoidEntity(isLlama, 24, 1.5, 1.5)})
	gs.Add(4, goal.NewLeapAtTarget(0.4))
	gs.Add(5, goal.NewMeleeAttack(1, true))
	gs.Add(6, newFollowOwner(&w.Tamable, 1, 10, 2))
	gs.Add(7, wrapGoal(&janimal.BreedGoal[petHost]{Speed: 1, CanMate: func(a, b petHost) bool {
		return wolfCanMate(a.m.Behaviour().(*Wolf), b.m.Behaviour().(*Wolf))
	}}))
	gs.Add(8, goal.NewWaterAvoidingRandomStroll(1, 0.001))
	gs.Add(9, &begGoal{w: w, lookDistance: 8})
	gs.Add(10, goal.NewLookAtPlayer(8))
	gs.Add(10, goal.NewRandomLookAround())

	ts.Add(1, newOwnerHurtBy(&w.Tamable, false, wolfWants))
	ts.Add(2, newOwnerHurtBy(&w.Tamable, true, wolfWants))
	ts.Add(3, goal.NewHurtByTarget().SetAlertOthers())
	angry := goal.NewNearestAttackableTarget(javamob.IsPlayer, true, false)
	angry.Selector = func(e world.Entity, tx *world.Tx) bool {
		jm, ok := javamob.OfHandle(tx, w.self)
		return ok && w.Anger.IsAngryAt(jm, e)
	}
	ts.Add(4, angry)
	prey := goal.NewNearestAttackableTarget(func(e world.Entity) bool {
		switch entityID(e) {
		case "minecraft:sheep", "minecraft:rabbit", "minecraft:fox":
			return true
		}
		return false
	}, false, false)
	ts.Add(5, &nonTameRandomTarget{t: &w.Tamable, NearestAttackableTarget: prey})
	turtles := goal.NewNearestAttackableTarget(isBabyTurtleOnLand, false, false)
	ts.Add(6, &nonTameRandomTarget{t: &w.Tamable, NearestAttackableTarget: turtles})
	ts.Add(7, goal.NewNearestAttackableTarget(isSkeleton, false, false))
	w.self = m.H()
}

func wolfWants(m *javamob.Mob, target, owner world.Entity) bool {
	return WolfWantsToAttack(target, owner)
}

func isLlama(e world.Entity) bool {
	id := entityID(e)
	return id == "minecraft:llama" || id == "minecraft:trader_llama"
}

// isSkeleton is AbstractSkeleton: skeleton, stray, wither skeleton, bogged,
// parched.
func isSkeleton(e world.Entity) bool {
	switch entityID(e) {
	case "minecraft:skeleton", "minecraft:stray", "minecraft:wither_skeleton", "minecraft:bogged", "minecraft:parched":
		return true
	}
	return false
}

// isBabyTurtleOnLand is Turtle.BABY_ON_LAND_SELECTOR.
func isBabyTurtleOnLand(e world.Entity) bool {
	if entityID(e) != "minecraft:turtle" {
		return false
	}
	jm, ok := javamob.Of(e)
	return ok && jm.Baby() && !jm.InWater()
}

// wolfAvoidLlama is Wolf.WolfAvoidEntityGoal: a wild wolf runs from a llama
// with strength >= nextInt(5) within 24 blocks, forgetting its target.
type wolfAvoidLlama struct {
	*goal.AvoidEntity
	w *Wolf
}

func (g *wolfAvoidLlama) CanUse(m *javamob.Mob) bool {
	if !g.AvoidEntity.CanUse(m) || g.w.Tame {
		return false
	}
	box := m.BBox().GrowVec3(mgl64.Vec3{24, 3, 24})
	l, ok := m.NearestEntity(box, isLlama, javamob.ForCombat(24))
	if !ok {
		return false
	}
	lm, ok := javamob.Of(l)
	if !ok {
		return false
	}
	strength := 1
	if b, ok := lm.Behaviour().(*Equine); ok {
		strength = b.llama.Strength
	}
	return strength >= m.Rand().IntN(5)
}

func (g *wolfAvoidLlama) Start(m *javamob.Mob) {
	m.SetTarget(nil)
	g.AvoidEntity.Start(m)
}

func (g *wolfAvoidLlama) Tick(m *javamob.Mob) {
	m.SetTarget(nil)
	g.AvoidEntity.Tick(m)
}

// nonTameRandomTarget is NonTameRandomTargetGoal: only while wild.
type nonTameRandomTarget struct {
	*goal.NearestAttackableTarget
	t *Tamable
}

func (g *nonTameRandomTarget) CanUse(m *javamob.Mob) bool {
	return !g.t.Tame && g.NearestAttackableTarget.CanUse(m)
}

// begGoal is BegGoal (flags LOOK): a wolf looks with its head tilted at the
// nearest player within 8 blocks holding a bone or its food, for 2 to 4
// seconds.
type begGoal struct {
	w            *Wolf
	lookDistance float64
	player       *world.EntityHandle
	lookTime     int
}

func (g *begGoal) Flags() javamob.Flags { return javamob.FlagLook }

func (g *begGoal) CanUse(m *javamob.Mob) bool {
	p, ok := m.NearestPlayer(javamob.ForNonCombat(g.lookDistance))
	if !ok || !g.interesting(p) {
		return false
	}
	g.player = p.H()
	return true
}

func (g *begGoal) interesting(p world.Entity) bool {
	return janimal.HoldsItem(p, func(s itemStack) bool {
		n := itemName(s)
		return n == "minecraft:bone" || IsWolfFood(n)
	})
}

func (g *begGoal) CanContinueToUse(m *javamob.Mob) bool {
	p, ok := g.player.Entity(m.Tx())
	if !ok || dead(p) {
		return false
	}
	if distSq(m.Position(), p.Position()) > g.lookDistance*g.lookDistance {
		return false
	}
	return g.lookTime > 0 && g.interesting(p)
}

func (g *begGoal) Start(m *javamob.Mob) {
	g.w.setInterested(m, true)
	g.lookTime = javamob.ReducedTickDelay(40 + m.Rand().IntN(40))
}

func (g *begGoal) Stop(m *javamob.Mob) {
	g.w.setInterested(m, false)
	g.player = nil
}

func (g *begGoal) Tick(m *javamob.Mob) {
	if p, ok := g.player.Entity(m.Tx()); ok {
		eye := p.Position()
		if l, ok := p.(interface{ EyeHeight() float64 }); ok {
			eye[1] += l.EyeHeight()
		}
		m.LookControl().SetLookAtWith(eye, 10, maxHeadXRot(m))
	}
	g.lookTime--
}

var _ = math.Pi
