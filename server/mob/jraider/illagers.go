//go:build crossfly_full

package jraider

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/mob/jraid"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// attributes are the createAttributes of a raid mob (jraid.MobAttributes) on top of the monster attributes.
func attributes(javaID string) func() map[javamob.Attribute]float64 {
	return func() map[javamob.Attribute]float64 {
		j := jraid.MobAttributes[javaID]
		a := javamob.MonsterAttributes()
		a[javamob.AttrMaxHealth] = j.MaxHealth
		a[javamob.AttrMovementSpeed] = float64(float32(j.MovementSpeed))
		a[javamob.AttrAttackDamage] = j.AttackDamage
		a[javamob.AttrFollowRange] = j.FollowRange
		if j.KnockbackResistance > 0 {
			a[javamob.AttrKnockbackResistance] = j.KnockbackResistance
		}
		if j.AttackKnockback > 0 {
			a[javamob.AttrAttackKnockback] = j.AttackKnockback
		}
		if j.StepHeight > 0 {
			a[javamob.AttrStepHeight] = j.StepHeight
		}
		return a
	}
}

func init() {
	javamob.Register(javamob.Type{
		ID: PillagerID, Category: javamob.CategoryMonster, Width: 0.6, Height: 1.95, EyeHeight: 1.62,
		NotInPeaceful: true, Illager: true, XPReward: 5, LootTable: "entities/pillager",
		Families:   []string{"pillager", "illager", "monster", "mob"},
		Attributes: attributes("minecraft:pillager"),
		New:        func() javamob.Behaviour { return &Pillager{base: newBase(jraid.Pillager, "celebrate")} },
	})
	javamob.Register(javamob.Type{
		ID: VindicatorID, Category: javamob.CategoryMonster, Width: 0.6, Height: 1.95, EyeHeight: 1.62,
		NotInPeaceful: true, Illager: true, XPReward: 5, LootTable: "entities/vindicator",
		Families:   []string{"vindicator", "illager", "monster", "mob"},
		Attributes: attributes("minecraft:vindicator"),
		New:        func() javamob.Behaviour { return &Vindicator{base: newBase(jraid.Vindicator, "celebrate")} },
	})
}

// raiderTargets are the target goals every illager has: retaliation (not against raiders, calling the others)
// and players.
func hurtByTarget() *goal.HurtByTarget { return goal.NewHurtByTarget(raiderIDs...) }

func lookAtMobs(dist float64) *goal.LookAt {
	g := goal.NewLookAtPlayer(dist)
	g.Match = func(e world.Entity) bool { _, ok := javamob.Of(e); return ok }
	return g
}

func villagerTarget(mustSee bool) *goal.NearestAttackableTarget {
	return goal.NewNearestAttackableTarget(goal.IsType("minecraft:villager_v2", "minecraft:villager", "minecraft:wandering_trader"), mustSee, false)
}

func golemTarget(mustSee bool) *goal.NearestAttackableTarget {
	return goal.NewNearestAttackableTarget(goal.IsType("minecraft:iron_golem"), mustSee, false)
}

// Pillager is the Pillager class.
type Pillager struct {
	base
	crossbow *crossbowAttack
}

// RegisterGoals is Pillager.registerGoals.
func (p *Pillager) RegisterGoals(m *javamob.Mob) {
	p.registerRaiderGoals(m)
	p.crossbow = &crossbowAttack{GoalBase: javamob.GoalBase{Control: javamob.FlagMove | javamob.FlagLook}, p: p, g: jraid.NewCrossbowGoal(8)}
	m.Goals().Add(0, goal.NewFloat(m))
	m.Goals().Add(1, goal.NewAvoidEntity(goal.IsType("minecraft:creaking"), 8, 1, 1.2))
	m.Goals().Add(2, newHoldGroundAttack(&p.base, 10))
	m.Goals().Add(3, p.crossbow)
	m.Goals().Add(8, goal.NewRandomStroll(0.6))
	m.Goals().Add(9, goal.NewLookAtPlayer(15))
	m.Goals().Add(10, lookAtMobs(15))
	m.Targets().Add(1, hurtByTarget().SetAlertOthers())
	m.Targets().Add(2, goal.NewNearestAttackableTarget(nil, true, false))
	m.Targets().Add(3, villagerTarget(false))
	m.Targets().Add(3, golemTarget(true))
}

// CanAttack is AbstractIllager.canAttack.
func (p *Pillager) CanAttack(_ *javamob.Mob, e world.Entity) bool { return illagerCanAttack(e) }

// WalkTargetValue is Pillager.getWalkTargetValue: 0 everywhere (no light preference).
func (p *Pillager) WalkTargetValue(*javamob.Mob, cube.Pos) float64 { return 0 }

// MaxSpawnClusterSize is Pillager.getMaxSpawnClusterSize.
func (p *Pillager) MaxSpawnClusterSize() int       { return 1 }
func (p *Pillager) IsMaxGroupSizeReached(int) bool { return false }

// FinalizeSpawn is Pillager.finalizeSpawn: a crossbow, then PatrollingMonster's captain roll.
func (p *Pillager) FinalizeSpawn(m *javamob.Mob, reason javamob.SpawnReason) {
	m.SetItemSlot(javamob.SlotMainHand, item.NewStack(item.Crossbow{}, 1))
	p.finalizeRaider(m, reason)
}

// ApplyRaidBuffs is Pillager.applyRaidBuffs. The enchanted crossbows of later waves (RAID_PILLAGER_POST_WAVE_3/5
// enchantment providers) are not ported: raid pillagers keep their plain crossbow.
func (p *Pillager) ApplyRaidBuffs(*javamob.Mob, int) {}

func holdingCrossbow(m *javamob.Mob) bool {
	main, off := m.HeldItems()
	_, a := main.Item().(item.Crossbow)
	_, b := off.Item().(item.Crossbow)
	return a || b
}

// PerformRangedAttack is Pillager.performRangedAttack (CrossbowAttackMob.performCrossbowAttack): the charged
// arrow at 1.6 blocks a tick, inaccuracy 14 - 4 × difficulty.
func (p *Pillager) PerformRangedAttack(m *javamob.Mob, target world.Entity) {
	main, _ := m.HeldItems()
	if c, ok := main.Item().(item.Crossbow); ok {
		start := m.EyePosition().Sub(mgl64.Vec3{0, 0.1})
		bb := target.H().Type().BBox(target)
		tp := target.Position()
		xd, zd := tp[0]-start[0], tp[2]-start[2]
		yd := tp[1] + (bb.Max()[1]-bb.Min()[1])/3 - start[1]
		dist := math.Sqrt(xd*xd + zd*zd)
		vel := shootVelocity(m.Rand(), mgl64.Vec3{xd, yd + dist*0.2, zd}, 1.6, float64(jraid.RangedAttackUncertainty(jraid.Difficulty(m.DifficultyID()))))
		m.Tx().AddEntity(entity.NewArrowWithDamage(world.EntitySpawnOpts{Position: start, Velocity: vel}, 2, m))
		m.PlaySound("crossbow.shoot")
		c.Item = item.Stack{}
		m.SetItemSlot(javamob.SlotMainHand, main.WithItem(c))
	}
	m.SetNoActionTime(0)
}

// shootVelocity is Projectile.shoot: the direction normalized, a gaussian spread of 0.0075 × uncertainty, times
// the power.
func shootVelocity(r *rand.Rand, dir mgl64.Vec3, power, uncertainty float64) mgl64.Vec3 {
	if dir.Len() > 0 {
		dir = dir.Normalize()
	}
	s := 0.0075 * uncertainty
	return dir.Add(mgl64.Vec3{r.NormFloat64() * s, r.NormFloat64() * s, r.NormFloat64() * s}).Mul(power)
}

// crossbowAttack is RangedCrossbowAttackGoal(pillager, 1.0, 8) with the state machine of jraid.CrossbowGoal.
type crossbowAttack struct {
	javamob.GoalBase
	p *Pillager
	g *jraid.CrossbowGoal
}

func (c *crossbowAttack) validTarget(m *javamob.Mob) bool {
	t, ok := m.Target()
	if !ok {
		return false
	}
	l, ok := t.(entity.Living)
	return !ok || !l.Dead()
}

func (c *crossbowAttack) CanUse(m *javamob.Mob) bool { return c.validTarget(m) && holdingCrossbow(m) }
func (c *crossbowAttack) CanContinueToUse(m *javamob.Mob) bool {
	return c.validTarget(m) && (c.CanUse(m) || !m.Navigation().Done()) && holdingCrossbow(m)
}
func (c *crossbowAttack) Start(m *javamob.Mob) { m.SetAggressive(true) }

// Stop is RangedCrossbowAttackGoal.stop: the target is dropped and a half-drawn crossbow is let go.
func (c *crossbowAttack) Stop(m *javamob.Mob) {
	m.SetAggressive(false)
	m.SetTarget(nil)
	c.g.SeeTime = 0
	if m.UsingItem() {
		m.StopUsingItem()
	}
}
func (c *crossbowAttack) RequiresUpdateEveryTick() bool { return true }

func (c *crossbowAttack) Tick(m *javamob.Mob) {
	t, ok := m.Target()
	if !ok {
		return
	}
	los := m.HasLineOfSight(t)
	c.g.Tick(m.Rand(), crossbowMob{m: m, p: c.p, t: t}, los, t.Position().Sub(m.Position()).LenSqr())
}

// crossbowMob is what RangedCrossbowAttackGoal drives on the pillager.
type crossbowMob struct {
	m *javamob.Mob
	p *Pillager
	t world.Entity
}

func (c crossbowMob) MoveTo(speed float64)     { c.m.Navigation().MoveToEntity(c.m, c.t, speed) }
func (c crossbowMob) StopNavigation()          { c.m.Navigation().Stop() }
func (c crossbowMob) LookAtTarget()            { c.m.LookControl().SetLookAtEntity(c.t, 30, 30) }
func (c crossbowMob) StartUsingCrossbow()      { c.m.StartUsingItem() }
func (c crossbowMob) IsUsingItem() bool        { return c.m.UsingItem() }
func (c crossbowMob) TicksUsingItem() int      { return c.m.TicksUsingItem() }
func (c crossbowMob) ChargeDuration() int      { return 25 }
func (c crossbowMob) SetChargingCrossbow(bool) {}
func (c crossbowMob) PerformRangedAttack()     { c.p.PerformRangedAttack(c.m, c.t) }

// ReleaseUsingItem loads the crossbow (CrossbowItem.releaseUsing with a full charge).
func (c crossbowMob) ReleaseUsingItem() {
	c.m.StopUsingItem()
	main, _ := c.m.HeldItems()
	if cb, ok := main.Item().(item.Crossbow); ok {
		cb.Item = item.NewStack(item.Arrow{}, 1)
		c.m.SetItemSlot(javamob.SlotMainHand, main.WithItem(cb))
	}
}

// EncodeNBT and DecodeNBT: Pillager.readAdditionalSaveData sets canPickUpLoot.
func (p *Pillager) DecodeNBT(m *javamob.Mob, data map[string]any) {
	p.base.DecodeNBT(m, data)
	m.SetCanPickUpLoot(true)
}

// Vindicator is the Vindicator class.
type Vindicator struct {
	base
	johnny bool
}

// RegisterGoals is Vindicator.registerGoals. AbstractIllager.RaiderOpenDoorGoal (priority 3) is not ported: the
// Java mob base has no OpenDoorGoal yet.
func (v *Vindicator) RegisterGoals(m *javamob.Mob) {
	v.registerRaiderGoals(m)
	melee := goal.NewMeleeAttack(1, false)
	melee.OnStart = func(m *javamob.Mob, _ *goal.MeleeAttack) { m.SetAggressive(true) }
	melee.OnStop = func(m *javamob.Mob, _ *goal.MeleeAttack) { m.SetAggressive(false) }
	m.Goals().Add(0, goal.NewFloat(m))
	m.Goals().Add(1, goal.NewAvoidEntity(goal.IsType("minecraft:creaking"), 8, 1, 1.2))
	m.Goals().Add(2, &vindicatorBreakDoor{BreakDoor: goal.NewBreakDoor(func(id int) bool { return jraid.VindicatorBreaksDoors(jraid.Difficulty(id)) }), v: v})
	m.Goals().Add(4, newHoldGroundAttack(&v.base, 10))
	m.Goals().Add(5, melee)
	m.Targets().Add(1, hurtByTarget().SetAlertOthers())
	m.Targets().Add(2, goal.NewNearestAttackableTarget(nil, true, false))
	m.Targets().Add(3, villagerTarget(true))
	m.Targets().Add(3, golemTarget(true))
	johnny := goal.NewNearestAttackableTarget(func(e world.Entity) bool { _, ok := e.(entity.Living); return ok }, true, true)
	johnny.RandomInterval = 0
	m.Targets().Add(4, &johnnyAttack{NearestAttackableTarget: johnny, v: v})
	m.Goals().Add(8, goal.NewRandomStroll(0.6))
	m.Goals().Add(9, goal.NewLookAtPlayer(3))
	m.Goals().Add(10, lookAtMobs(8))
}

// CanAttack is AbstractIllager.canAttack.
func (v *Vindicator) CanAttack(_ *javamob.Mob, e world.Entity) bool { return illagerCanAttack(e) }

// CustomServerAiStep is Vindicator.customServerAiStep: doors open only where a raid is.
func (v *Vindicator) CustomServerAiStep(m *javamob.Mob) {
	if !m.NoAI() {
		raided := stateOf(m.Tx().World()).raids.IsRaided(v.BlockPos())
		m.Navigation().SetCanOpenDoors(raided)
	}
	v.base.CustomServerAiStep(m)
}

// FinalizeSpawn is Vindicator.finalizeSpawn: an iron axe outside raids (raids give theirs in ApplyRaidBuffs).
func (v *Vindicator) FinalizeSpawn(m *javamob.Mob, reason javamob.SpawnReason) {
	v.finalizeRaider(m, reason)
	m.Navigation().SetCanOpenDoors(true)
	if v.State.Raid() == nil {
		m.SetItemSlot(javamob.SlotMainHand, item.NewStack(item.Axe{Tier: item.ToolTierIron}, 1))
	}
	if jraid.IsJohnny(m.NameTag()) {
		v.johnny = true
	}
}

// ApplyRaidBuffs is Vindicator.applyRaidBuffs: an iron axe (its raid enchantments are not ported).
func (v *Vindicator) ApplyRaidBuffs(m *javamob.Mob, _ int) {
	m.SetItemSlot(javamob.SlotMainHand, item.NewStack(item.Axe{Tier: item.ToolTierIron}, 1))
}

// Tick notices a name tag "Johnny" (Vindicator.setCustomName).
func (v *Vindicator) Tick(m *javamob.Mob) {
	v.base.Tick(m)
	if !v.johnny && jraid.IsJohnny(m.NameTag()) {
		v.johnny = true
	}
}

func (v *Vindicator) EncodeNBT(data map[string]any) {
	v.base.EncodeNBT(data)
	if v.johnny {
		data["Johnny"] = uint8(1)
	}
}

func (v *Vindicator) DecodeNBT(m *javamob.Mob, data map[string]any) {
	v.base.DecodeNBT(m, data)
	if b, ok := data["Johnny"].(uint8); ok && b != 0 {
		v.johnny = true
	}
}

// vindicatorBreakDoor is Vindicator.VindicatorBreakDoorGoal: only in an active raid, tried every 5 ticks on average.
type vindicatorBreakDoor struct {
	*goal.BreakDoor
	v *Vindicator
}

func (g *vindicatorBreakDoor) Flags() javamob.Flags { return javamob.FlagMove }
func (g *vindicatorBreakDoor) CanUse(m *javamob.Mob) bool {
	return g.v.HasActiveRaid() && m.Rand().IntN(javamob.ReducedTickDelay(10)) == 0 && g.BreakDoor.CanUse(m)
}
func (g *vindicatorBreakDoor) CanContinueToUse(m *javamob.Mob) bool {
	return g.v.HasActiveRaid() && g.BreakDoor.CanContinueToUse(m)
}
func (g *vindicatorBreakDoor) Start(m *javamob.Mob) {
	g.BreakDoor.Start(m)
	m.SetNoActionTime(0)
}

// johnnyAttack is Vindicator.VindicatorJohnnyAttackGoal: a vindicator named Johnny attacks every living entity.
type johnnyAttack struct {
	*goal.NearestAttackableTarget
	v *Vindicator
}

func (g *johnnyAttack) CanUse(m *javamob.Mob) bool {
	return g.v.johnny && g.NearestAttackableTarget.CanUse(m)
}

func (g *johnnyAttack) Start(m *javamob.Mob) {
	g.NearestAttackableTarget.Start(m)
	m.SetNoActionTime(0)
}
