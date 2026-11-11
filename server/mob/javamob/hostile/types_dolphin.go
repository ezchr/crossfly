//go:build crossfly_full

package hostile

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/mob/javamob/path"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func init() {
	javamob.Register(javamob.Type{
		ID: dolphinID, Category: javamob.CategoryWaterCreature, Width: 0.9, Height: 0.6, EyeHeight: 0.3,
		Aquatic: true, XPReward: 1,
		Families: []string{"dolphin", "mob"},
		Attributes: func() map[javamob.Attribute]float64 {
			a := javamob.LivingAttributes()
			a[javamob.AttrMaxHealth], a[javamob.AttrMovementSpeed], a[javamob.AttrAttackDamage] = DolphinAttributes()
			return a
		},
		New: func() javamob.Behaviour { return &DolphinMob{} },
	})
}

// DolphinMob is Dolphin on the base: SmoothSwimmingMoveControl(85, 10, 0.02,
// 0.1, true), SmoothSwimmingLookControl(10), the water-bound navigation with
// breaching, and the goals of dolphin.go. Not here: FollowPlayerRiddenEntity
// (Crossfly boats and nautili are not Java mobs the goal could follow) and
// treasure leading (no structures, see treasureFinder).
type DolphinMob struct{ d *Dolphin }

func isGuardian(e world.Entity) bool {
	id := entityID(e)
	return id == guardianID || id == elderGuardianID
}

// RegisterGoals is Dolphin.registerGoals and its constructor's controls.
func (b *DolphinMob) RegisterGoals(m *javamob.Mob) {
	b.d = dolphinOf(m.Mob)
	m.SetNavigation(path.NewWaterNavigation(m, true))
	m.SetPathfindingMalus(path.Water, 0) // AgeableWaterCreature
	m.SetCanPickUpLoot(true)
	m.MoveControl().Custom = func(m *javamob.Mob, c *javamob.MoveControl) {
		smoothSwimMoveTick(m, c, 85, 10, 0.02, 0.1, true)
	}
	m.LookControl().Custom = func(m *javamob.Mob, c *javamob.LookControl) { smoothSwimLookTick(m, c, 10) }
	m.Goals().Add(0, goalOf(breathAirGoal{}))
	m.Goals().Add(0, &tryFindWaterGoal{GoalBase: javamob.GoalBase{}})
	m.Goals().Add(1, goalOf(&dolphinTreasureGoal{}))
	m.Goals().Add(2, goalOf(&dolphinSwimWithPlayerGoal{speed: 4}))
	swim := goal.NewRandomStroll(1)
	swim.Interval = 10
	swim.Position = swimPosition
	m.Goals().Add(4, swim)
	m.Goals().Add(4, goal.NewRandomLookAround())
	m.Goals().Add(5, goal.NewLookAtPlayer(6))
	m.Goals().Add(5, goalOf(&dolphinJumpGoal{interval: 10}))
	m.Goals().Add(6, goal.NewMeleeAttack(float64(float32(1.2)), true))
	m.Goals().Add(7, goalOf(&dolphinMoveToItemGoal{}))
	m.Goals().Add(8, goalOf(dolphinPlayGoal{}))
	m.Goals().Add(9, goal.NewAvoidEntity(isGuardian, 8, 1, 1))
	hurt := goal.NewHurtByTarget(guardianID, elderGuardianID) // HurtByTargetGoal(this, Guardian.class)
	hurt.SetAlertOthers()
	m.Targets().Add(1, hurt)
}

// MaxHeadYRot, MaxHeadXRot are Dolphin's (1, 1); HeadRotSpeed the base 10.
func (b *DolphinMob) MaxHeadYRot() float64  { return 1 }
func (b *DolphinMob) MaxHeadXRot() float64  { return 1 }
func (b *DolphinMob) HeadRotSpeed() float64 { return 10 }

// Tick is the air of LivingEntity.baseTick for a dolphin (4800, refilled at
// once at the surface).
func (b *DolphinMob) Tick(m *javamob.Mob) {
	DolphinBaseTick(m.Mob, m.NoAI(), func(bm *mob.Mob, dmg float64) { bm.Hurt(dmg, entity.DrowningDamageSource{}) })
}

// PostTick is Dolphin.tick after super.tick (moistness, drying out, flops)
// and Mob.aiStep picking up items.
func (b *DolphinMob) PostTick(m *javamob.Mob) {
	if !m.NoAI() {
		DolphinTick(m.Mob, m.InWaterOrRain(), func(bm *mob.Mob) { bm.Hurt(1, DryOutDamageSource{}) })
		if m.Dead() {
			return
		}
	}
	b.pickUpItems(m)
}

// pickUpItems is the loot part of Mob.aiStep: items within the box grown by
// 1, 0, 1 that may be picked up go into the empty mouth.
func (b *DolphinMob) pickUpItems(m *javamob.Mob) {
	if m.Dead() || !m.CanPickUpLoot() {
		return
	}
	tx := m.Tx()
	for e := range tx.EntitiesWithin(m.BBox().GrowVec3(mgl64.Vec3{1, 0, 1})) {
		it, ok := e.(*entity.Ent)
		if !ok {
			continue
		}
		ib, ok := it.Behaviour().(*entity.ItemBehaviour)
		if !ok || ItemPickupDelayed(it) {
			continue
		}
		if DolphinPickUpItem(m.Mob, ib.Item()) {
			m.SetGuaranteedDrop(javamob.SlotMainHand)
			tx.RemoveEntity(it)
			return
		}
	}
}

// Travel is Dolphin.travelInWater in water.
func (b *DolphinMob) Travel(m *javamob.Mob, input mgl64.Vec3) bool {
	if !m.InWater() {
		return false
	}
	DolphinTravelInWater(m.Mob, input, m.MoveSpeed())
	return true
}

// CanAttack is Dolphin.canAttack.
func (b *DolphinMob) CanAttack(m *javamob.Mob, _ world.Entity) bool { return DolphinCanAttack(m.Mob) }

// MobInteract is Dolphin.mobInteract with a fish.
func (b *DolphinMob) MobInteract(m *javamob.Mob, _ item.User, held item.Stack) (item.Stack, bool) {
	if !DolphinInteract(m.Mob, held, false, nil) {
		return held, false
	}
	m.PlaySound("eat")
	return held.Grow(-1), true
}

// FarAwayRemover: AgeableWaterCreature keeps the default.

// XPReward is WaterAnimal.getBaseExperienceReward.
func (b *DolphinMob) XPReward(m *javamob.Mob, _ int) int { return waterAnimalXP(m) }

// smoothSwimMoveTick is SmoothSwimmingMoveControl.tick.
func smoothSwimMoveTick(m *javamob.Mob, c *javamob.MoveControl, maxTurnX, maxTurnY, inWater, outside float64, gravity bool) {
	if gravity && m.InWater() {
		m.SetVelocity(m.Velocity().Add(mgl64.Vec3{0, 0.005}))
	}
	if !c.HasWanted() || m.Navigation().Done() {
		m.SetMoveSpeed(0)
		m.SetXxa(0)
		m.SetYya(0)
		m.SetZza(0)
		return
	}
	d := c.Wanted().Sub(m.Position())
	if d.LenSqr() < float64(float32(2.5000003e-7)) {
		m.SetZza(0)
		return
	}
	yRotD := float64(float32(math.Atan2(d[2], d[0])*180/math.Pi) - 90)
	m.SetYRot(rotlerp(m.YRot(), yRotD, maxTurnY))
	m.SetBodyYRot(m.YRot())
	m.SetHeadYRot(m.YRot())
	speed := float32(c.SpeedModifier() * m.Attr(javamob.AttrMovementSpeed))
	if m.InWater() {
		m.SetMoveSpeed(float64(speed * float32(inWater)))
		h := math.Sqrt(d[0]*d[0] + d[2]*d[2])
		if math.Abs(d[1]) > 1e-5 || math.Abs(h) > 1e-5 {
			xRotD := float64(float32(-(math.Atan2(d[1], h) * 180 / math.Pi)))
			xRotD = math.Max(-maxTurnX, math.Min(maxTurnX, wrapDegrees(xRotD)))
			m.SetXRot(rotateTowards(m.XRot(), xRotD, 5))
		}
		cos := math.Cos(m.XRot() * math.Pi / 180)
		sin := math.Sin(m.XRot() * math.Pi / 180)
		m.SetZza(float64(float32(cos) * speed))
		m.SetYya(float64(-float32(sin) * speed))
		return
	}
	left := math.Abs(wrapDegrees(m.YRot() - yRotD))
	factor := 1 - math.Max(0, math.Min(1, (left-10)/50))
	m.SetMoveSpeed(float64(speed * float32(outside) * float32(factor)))
}

// smoothSwimLookTick is SmoothSwimmingLookControl.tick.
func smoothSwimLookTick(m *javamob.Mob, c *javamob.LookControl, maxYRotFromCenter float64) {
	const yMaxRotSpeed, xMaxRotAngle = 10, 1 // the dolphin's head limits (getHeadRotSpeed, getMaxHeadXRot)
	if c.TickCooldown() {
		if y, ok := c.YRotD(m); ok {
			m.SetHeadYRot(rotateTowards(m.HeadYRot(), y+20, yMaxRotSpeed))
		}
		if x, ok := c.XRotD(m); ok {
			m.SetXRot(rotateTowards(m.XRot(), x+10, xMaxRotAngle))
		}
	} else {
		if m.Navigation().Done() {
			m.SetXRot(rotateTowards(m.XRot(), 0, 5))
		}
		m.SetHeadYRot(rotateTowards(m.HeadYRot(), m.BodyYRot(), yMaxRotSpeed))
	}
	diff := wrapDegrees(m.HeadYRot() - m.BodyYRot())
	if diff < -maxYRotFromCenter {
		m.SetBodyYRot(m.BodyYRot() - 4)
	} else if diff > maxYRotFromCenter {
		m.SetBodyYRot(m.BodyYRot() + 4)
	}
}

// rotateTowards is Mth.rotateTowards.
func rotateTowards(from, to, max float64) float64 {
	return from + math.Max(-max, math.Min(max, wrapDegrees(to-from)))
}

// tryFindWaterGoal is TryFindLiquidGoal(mob, #dolphin_tries_to_find = water)
// (no flags): on the ground out of water, head for the first water block
// within 2 blocks (x fastest, then y up to its own level, then z).
type tryFindWaterGoal struct{ javamob.GoalBase }

func (g *tryFindWaterGoal) CanUse(m *javamob.Mob) bool {
	l, ok := m.Tx().Liquid(blockPos(m.Position()))
	return m.OnGround() && !(ok && isWaterLiquid(l))
}

func (g *tryFindWaterGoal) Start(m *javamob.Mob) {
	p := m.Position()
	x0, y0, z0 := int(math.Floor(p[0]-2)), int(math.Floor(p[1]-2)), int(math.Floor(p[2]-2))
	x1, y1, z1 := int(math.Floor(p[0]+2)), int(math.Floor(p[1])), int(math.Floor(p[2]+2))
	tx := m.Tx()
	for z := z0; z <= z1; z++ {
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				if l, ok := tx.Liquid(cube.Pos{x, y, z}); ok && isWaterLiquid(l) {
					m.SetWantedPosition(float64(x), float64(y), float64(z), 1)
					return
				}
			}
		}
	}
}
