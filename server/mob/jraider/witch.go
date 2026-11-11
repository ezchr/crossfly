//go:build crossfly_full

package jraider

import (
	"fmt"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/mob/jraid"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func init() {
	javamob.Register(javamob.Type{
		ID: WitchID, Category: javamob.CategoryMonster, Width: 0.6, Height: 1.95, EyeHeight: 1.62,
		NotInPeaceful: true, XPReward: 5, LootTable: "entities/witch",
		Families:   []string{"witch", "monster", "mob"},
		Attributes: attributes("minecraft:witch"),
		New:        func() javamob.Behaviour { return &Witch{base: newBase(jraid.Witch, "celebrate")} },
	})
}

// Witch is the Witch class.
type Witch struct {
	base
	usingTime int
	drinking  bool
	heal      *healRaiders
	attack    *witchAttackPlayers
}

// RegisterGoals is Witch.registerGoals.
func (w *Witch) RegisterGoals(m *javamob.Mob) {
	w.registerRaiderGoals(m)
	heal := goal.NewNearestAttackableTarget(func(e world.Entity) bool {
		return isRaiderEntity(e) && e.H().Type().EncodeEntity() != WitchID
	}, true, false)
	heal.RandomInterval = javamob.ReducedTickDelay(500)
	heal.Selector = func(world.Entity, *world.Tx) bool { return w.HasActiveRaid() }
	w.heal = &healRaiders{NearestAttackableTarget: heal, w: w}
	attack := goal.NewNearestAttackableTarget(nil, true, false)
	w.attack = &witchAttackPlayers{NearestAttackableTarget: attack, canAttack: true}
	m.Goals().Add(1, goal.NewFloat(m))
	m.Goals().Add(2, newRangedAttack(1, 60, 60, 10, w.PerformRangedAttack))
	m.Goals().Add(2, goal.NewWaterAvoidingRandomStroll(1, 0.001))
	m.Goals().Add(3, goal.NewLookAtPlayer(8))
	m.Goals().Add(3, goal.NewRandomLookAround())
	m.Targets().Add(1, hurtByTarget())
	m.Targets().Add(2, w.heal)
	m.Targets().Add(3, w.attack)
}

// ApplyRaidBuffs is Witch.applyRaidBuffs: nothing.
func (w *Witch) ApplyRaidBuffs(*javamob.Mob, int) {}

// AiStep is Witch.aiStep before super.aiStep: the heal goal's cooldown switches the player goal; a drinking witch
// finishes its potion; otherwise it may start drinking water breathing, fire resistance, healing or swiftness.
func (w *Witch) AiStep(m *javamob.Mob) {
	w.base.AiStep(m)
	if m.Dead() {
		return
	}
	w.heal.cooldown--
	w.attack.canAttack = w.heal.cooldown <= 0
	speed := m.Attributes().Instance(javamob.AttrMovementSpeed)
	if w.drinking {
		if w.usingTime--; w.usingTime <= 0 {
			w.drinking = false
			m.StopUsingItem()
			main, _ := m.HeldItems()
			m.SetItemSlot(javamob.SlotMainHand, item.Stack{})
			if p, ok := main.Item().(item.Potion); ok {
				for _, e := range p.Type.Effects() {
					m.AddEffect(e)
				}
			}
			speed.RemoveModifier("minecraft:drinking")
		}
		return
	}
	s := jraid.WitchState{
		EyeInWater: m.UnderWater(), Health: float32(m.Health()), MaxHealth: float32(m.MaxHealth()),
		OnFireOrHurtByFire: m.IsOnFire(),
	}
	_, s.WaterBreathing = m.Effect(effect.WaterBreathing)
	_, s.FireResist = m.Effect(effect.FireResistance)
	_, s.Speed = m.Effect(effect.Speed)
	if t, ok := m.Target(); ok {
		s.HasTarget = true
		s.TargetDistSqr = t.Position().Sub(m.Position()).LenSqr()
	}
	var p potion.Potion
	switch jraid.WitchDrink(m.Rand(), s) {
	case jraid.PotionWaterBreathing:
		p = potion.WaterBreathing()
	case jraid.PotionFireResistance:
		p = potion.FireResistance()
	case jraid.PotionHealing:
		p = potion.Healing()
	case jraid.PotionSwiftness:
		p = potion.Swiftness()
	default:
		return
	}
	m.SetItemSlot(javamob.SlotMainHand, item.NewStack(item.Potion{Type: p}, 1))
	w.usingTime = jraid.PotionDrinkTicks
	w.drinking = true
	m.StartUsingItem()
	m.PlaySound("drink")
	speed.RemoveModifier("minecraft:drinking")
	speed.AddModifier(javamob.Modifier{ID: "minecraft:drinking", Amount: jraid.WitchDrinkingSpeed, Op: javamob.OpAddValue})
}

// FilterDamage is Witch.getDamageAfterMagicAbsorb: none from its own potions, 15% from magic.
func (w *Witch) FilterDamage(m *javamob.Mob, d *mob.Damage) {
	if e := javamob.SourceEntity(d.Source); e != nil && e.H() == m.H() {
		d.Amount = 0
		return
	}
	if witchResists(d.Source) {
		d.Amount *= jraid.WitchMagicDamageFactor
	}
}

// witchResists is DamageTypeTags.WITCH_RESISTANT_TO: magic, indirect magic, sonic boom and thorns.
func witchResists(src world.DamageSource) bool {
	n := strings.ToLower(fmt.Sprintf("%T", src))
	for _, s := range []string{"poison", "instant", "magic", "thorns", "fang", "sonic"} {
		if strings.Contains(n, s) {
			return true
		}
	}
	return false
}

// PerformRangedAttack is Witch.performRangedAttack: a splash potion picked by jraid.WitchThrow, thrown where the
// target will be, at 0.75 (0.45 within 2 blocks) with inaccuracy 8.
func (w *Witch) PerformRangedAttack(m *javamob.Mob, t world.Entity, _ float64) {
	if w.drinking {
		return
	}
	tv := mgl64.Vec3{}
	if v, ok := t.(interface{ Velocity() mgl64.Vec3 }); ok {
		tv = v.Velocity()
	}
	p, tp := m.Position(), t.Position()
	xd := tp[0] + tv[0] - p[0]
	yd := eyeOf(t)[1] - 1.1 - p[1]
	zd := tp[2] + tv[2] - p[2]
	dist := math.Sqrt(xd*xd + zd*zd)
	wt := jraid.WitchTarget{Raider: isRaiderEntity(t)}
	if l, ok := t.(entity.Living); ok {
		wt.Health = float32(l.Health())
	}
	if el, ok := t.(interface {
		Effect(effect.Type) (effect.Effect, bool)
	}); ok {
		_, wt.Slowness = el.Effect(effect.Slowness)
		_, wt.Poison = el.Effect(effect.Poison)
		_, wt.Weakness = el.Effect(effect.Weakness)
	}
	choice, untarget := jraid.WitchThrow(m.Rand(), wt, dist)
	if untarget {
		m.SetTarget(nil)
	}
	var pt potion.Potion
	switch choice {
	case jraid.PotionHealing:
		pt = potion.Healing()
	case jraid.PotionRegeneration:
		pt = potion.Regeneration()
	case jraid.PotionSlowness:
		pt = potion.Slowness()
	case jraid.PotionPoison:
		pt = potion.Poison()
	case jraid.PotionWeakness:
		pt = potion.Weakness()
	default:
		pt = potion.Harming()
	}
	start := m.EyePosition().Sub(mgl64.Vec3{0, 0.1})
	vel := shootVelocity(m.Rand(), mgl64.Vec3{xd, yd + dist*0.2, zd}, float64(jraid.WitchThrowPower(dist)), jraid.WitchRangedAttackUncertainty)
	m.Tx().AddEntity(entity.NewSplashPotion(world.EntitySpawnOpts{Position: start, Velocity: vel}, pt, m))
	m.PlaySound("throw")
}

func (w *Witch) EncodeNBT(data map[string]any)                 { w.base.EncodeNBT(data) }
func (w *Witch) DecodeNBT(m *javamob.Mob, data map[string]any) { w.base.DecodeNBT(m, data) }

// healRaiders is NearestHealableRaiderTargetGoal: during a raid, now and then the witch targets a hurt raider to
// throw healing at it; then the players goal waits 200 ticks.
type healRaiders struct {
	*goal.NearestAttackableTarget
	w        *Witch
	cooldown int
}

func (g *healRaiders) CanUse(m *javamob.Mob) bool {
	if g.cooldown > 0 || m.Rand().IntN(2) == 0 || !g.w.HasActiveRaid() {
		return false
	}
	g.FindTarget(m)
	return g.NearestAttackableTarget.CanUse(m)
}

func (g *healRaiders) Start(m *javamob.Mob) {
	g.cooldown = javamob.ReducedTickDelay(200)
	g.NearestAttackableTarget.Start(m)
}

// witchAttackPlayers is NearestAttackableWitchTargetGoal.
type witchAttackPlayers struct {
	*goal.NearestAttackableTarget
	canAttack bool
}

func (g *witchAttackPlayers) CanUse(m *javamob.Mob) bool {
	return g.canAttack && g.NearestAttackableTarget.CanUse(m)
}

// rangedAttack is RangedAttackGoal(mob, speed, attackIntervalMin, attackIntervalMax, attackRadius).
type rangedAttack struct {
	javamob.GoalBase
	speed            float64
	min, max         int
	radius, radiusSq float64
	attackTime       int
	seeTime          int
	target           *world.EntityHandle
	shoot            func(m *javamob.Mob, t world.Entity, power float64)
}

func newRangedAttack(speed float64, min, max int, radius float64, shoot func(*javamob.Mob, world.Entity, float64)) *rangedAttack {
	return &rangedAttack{GoalBase: javamob.GoalBase{Control: javamob.FlagMove | javamob.FlagLook}, speed: speed, min: min, max: max,
		radius: radius, radiusSq: radius * radius, attackTime: -1, shoot: shoot}
}

func (g *rangedAttack) CanUse(m *javamob.Mob) bool {
	t, ok := m.Target()
	if !ok || !alive(t) {
		return false
	}
	g.target = t.H()
	return true
}

func (g *rangedAttack) CanContinueToUse(m *javamob.Mob) bool {
	if g.CanUse(m) {
		return true
	}
	if g.target == nil {
		return false
	}
	t, ok := g.target.Entity(m.Tx())
	return ok && alive(t) && !m.Navigation().Done()
}

func (g *rangedAttack) Stop(*javamob.Mob) {
	g.target, g.seeTime, g.attackTime = nil, 0, -1
}

func (g *rangedAttack) RequiresUpdateEveryTick() bool { return true }

func (g *rangedAttack) Tick(m *javamob.Mob) {
	if g.target == nil {
		return
	}
	t, ok := g.target.Entity(m.Tx())
	if !ok {
		return
	}
	dist := t.Position().Sub(m.Position()).LenSqr()
	los := m.HasLineOfSight(t)
	if los {
		g.seeTime++
	} else {
		g.seeTime = 0
	}
	if dist <= g.radiusSq && g.seeTime >= 5 {
		m.Navigation().Stop()
	} else {
		m.Navigation().MoveToEntity(m, t, g.speed)
	}
	m.LookControl().SetLookAtEntity(t, 30, 30)
	if g.attackTime--; g.attackTime == 0 {
		if !los {
			return
		}
		dd := math.Sqrt(dist) / g.radius
		g.shoot(m, t, math.Max(0.1, math.Min(1, dd)))
		g.attackTime = int(math.Floor(dd*float64(g.max-g.min) + float64(g.min)))
	} else if g.attackTime < 0 {
		dd := math.Sqrt(dist) / g.radius
		g.attackTime = int(math.Floor(float64(g.min) + dd*float64(g.max-g.min)))
	}
}
