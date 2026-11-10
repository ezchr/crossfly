//go:build crossfly_full

package monster

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func init() {
	javamob.Register(javamob.Type{
		ID: "minecraft:skeleton", Category: javamob.CategoryMonster, Width: 0.6, Height: 1.99, EyeHeight: 1.74,
		NotInPeaceful: true, BurnsInDaylight: true, Undead: true, XPReward: 5,
		Families:   []string{"skeleton", "undead", "monster", "mob"},
		Attributes: SkeletonAttributes,
		New: func() javamob.Behaviour {
			s := NewAbstractSkeleton()
			s.FreezesInto = "minecraft:stray"
			return s
		},
	})
}

// SkeletonAttributes is AbstractSkeleton.createAttributes.
func SkeletonAttributes() map[javamob.Attribute]float64 {
	a := javamob.MonsterAttributes()
	a[javamob.AttrMovementSpeed] = 0.25
	return a
}

// AbstractSkeleton is Java's AbstractSkeleton (and Skeleton with FreezesInto set): a bow from range or melee
// without one, hiding from the sun. Subclasses embed it and set the exported fields in their Type.New.
type AbstractSkeleton struct {
	// AttackInterval and HardAttackInterval are getAttackInterval (40) and getHardAttackInterval (20), the
	// ticks between shots.
	AttackInterval, HardAttackInterval int
	// FreezesInto is the type the skeleton becomes after 7 seconds in powder snow and 15 more shaking
	// (Skeleton: stray). Empty: it does not.
	FreezesInto string
	// Arrow, if set, replaces getArrow: the arrow entity to shoot (a stray's slowness arrow).
	Arrow func(m *javamob.Mob, opts world.EntitySpawnOpts, damage float64) *world.EntityHandle

	bowGoal   *goal.RangedBowAttack
	meleeGoal *goal.MeleeAttack
	hadBow    bool
	assessed  bool

	freezeTime, convertTime int
	converting              bool
}

// NewAbstractSkeleton returns the behaviour of a skeleton-like mob.
func NewAbstractSkeleton() *AbstractSkeleton {
	return &AbstractSkeleton{AttackInterval: 40, HardAttackInterval: 20}
}

// RegisterGoals is AbstractSkeleton.registerGoals.
func (s *AbstractSkeleton) RegisterGoals(m *javamob.Mob) {
	s.bowGoal = goal.NewRangedBowAttack(1, 20, 15)
	s.meleeGoal = goal.NewMeleeAttack(1.2, false)
	s.meleeGoal.OnStart = func(m *javamob.Mob, _ *goal.MeleeAttack) { m.SetAggressive(true) }
	s.meleeGoal.OnStop = func(m *javamob.Mob, _ *goal.MeleeAttack) { m.SetAggressive(false) }
	m.Goals().Add(2, &goal.RestrictSun{})
	m.Goals().Add(3, goal.NewFleeSun(1))
	m.Goals().Add(3, goal.NewAvoidEntity(goal.IsType("minecraft:wolf"), 6, 1, 1.2))
	m.Goals().Add(5, goal.NewWaterAvoidingRandomStroll(1, 0.001))
	m.Goals().Add(6, goal.NewLookAtPlayer(8))
	m.Goals().Add(6, goal.NewRandomLookAround())
	m.Targets().Add(1, goal.NewHurtByTarget())
	m.Targets().Add(2, goal.NewNearestAttackableTarget(nil, true, false))
	m.Targets().Add(3, goal.NewNearestAttackableTarget(goal.IsType("minecraft:iron_golem"), true, false))
	turtles := goal.NewNearestAttackableTarget(goal.IsType("minecraft:turtle"), true, false)
	turtles.Selector = babyOnLand
	m.Targets().Add(3, turtles)
	s.ReassessWeaponGoal(m)
}

// holdingBow is getItemInHand(getWeaponHoldingHand(this, Items.BOW)).is(Items.BOW).
func holdingBow(m *javamob.Mob) bool { return goal.HoldingBow(m) }

// ReassessWeaponGoal is reassessWeaponGoal: the bow goal with a bow in hand (a shot every 2 seconds, every second
// on hard), the melee goal otherwise.
func (s *AbstractSkeleton) ReassessWeaponGoal(m *javamob.Mob) {
	m.Goals().Remove(m, s.meleeGoal)
	m.Goals().Remove(m, s.bowGoal)
	s.hadBow = holdingBow(m)
	s.assessed = true
	if s.hadBow {
		interval := s.HardAttackInterval
		if m.DifficultyID() != 3 {
			interval = s.AttackInterval
		}
		s.bowGoal.AttackIntervalMin = interval
		m.Goals().Add(4, s.bowGoal)
		return
	}
	m.Goals().Add(4, s.meleeGoal)
}

// Tick is Skeleton.tick before super.tick: the freezing conversion; and onEquipItem: a new weapon reassesses the
// attack goal.
func (s *AbstractSkeleton) Tick(m *javamob.Mob) {
	if !s.assessed || holdingBow(m) != s.hadBow {
		s.ReassessWeaponGoal(m)
	}
	if s.FreezesInto == "" || m.Dead() || m.NoAI() {
		return
	}
	if !inPowderSnow(m) {
		s.freezeTime = -1
		s.setConverting(m, false)
		return
	}
	if s.converting {
		if s.convertTime--; s.convertTime < 0 {
			if n, ok := m.ConvertTo(s.FreezesInto, true, nil); ok {
				if bm, ok := mob.Of(n); ok {
					bm.PlaySound("convert_to_stray")
				}
			}
		}
		return
	}
	if s.freezeTime++; s.freezeTime >= 140 {
		s.convertTime = 300
		s.setConverting(m, true)
	}
}

func inPowderSnow(m *javamob.Mob) bool {
	b := m.Tx().Block(cube.PosFromVec3(m.Position()))
	name, _ := b.EncodeBlock()
	return name == "minecraft:powder_snow"
}

func (s *AbstractSkeleton) setConverting(m *javamob.Mob, v bool) {
	s.converting = v
	if m.Meta().Shaking != v {
		m.UpdateMeta(func(meta *mob.Meta) { meta.Shaking = v })
	}
}

// FinalizeSpawn is AbstractSkeleton.finalizeSpawn: armour, a bow, picking up loot.
func (s *AbstractSkeleton) FinalizeSpawn(m *javamob.Mob, _ javamob.SpawnReason) {
	d := m.Difficulty()
	m.PopulateDefaultEquipmentSlots(d)
	m.SetItemSlot(javamob.SlotMainHand, item.NewStack(item.Bow{}, 1))
	s.ReassessWeaponGoal(m)
	m.SetCanPickUpLoot(m.Rand().Float32() < 0.55*float32(d.Special))
}

// PerformRangedAttack is AbstractSkeleton.performRangedAttack: an arrow at a third of the target's height, aimed
// 0.2 per block higher, at 1.6 blocks a tick with an inaccuracy of 14 - 4 × difficulty.
func (s *AbstractSkeleton) PerformRangedAttack(m *javamob.Mob, target world.Entity, power float64) {
	r := m.Rand()
	tp := target.Position()
	bb := target.H().Type().BBox(target)
	start := m.EyePosition().Sub(mgl64.Vec3{0, 0.1})
	xd := tp[0] - m.Position()[0]
	yd := tp[1] + (bb.Max()[1]-bb.Min()[1])/3 - start[1]
	zd := tp[2] - m.Position()[2]
	dist := math.Sqrt(xd*xd + zd*zd)
	dir := mgl64.Vec3{xd, yd + dist*float64(float32(0.2)), zd}.Normalize()
	inaccuracy := float64(14 - m.DifficultyID()*4)
	dir = dir.Add(mgl64.Vec3{r.NormFloat64(), r.NormFloat64(), r.NormFloat64()}.Mul(0.0075 * inaccuracy)).Mul(1.6)
	// AbstractArrow.setBaseDamageFromMob: power × 2 plus a random bonus that grows with the difficulty.
	damage := float64(power)*2 + triangle(r.Float64, float64(m.DifficultyID())*0.11, 0.57425)
	opts := world.EntitySpawnOpts{Position: start, Velocity: dir}
	var h *world.EntityHandle
	if s.Arrow != nil {
		h = s.Arrow(m, opts, damage)
	} else {
		h = entity.NewArrowWithDamage(opts, damage, m)
	}
	m.Tx().AddEntity(h)
	m.PlaySound("shoot")
}

// triangle is RandomSource.triangle(mode, deviation).
func triangle(f func() float64, mode, dev float64) float64 { return mode + dev*(f()-f()) }

// WalkTargetValue is Monster.getWalkTargetValue.
func (s *AbstractSkeleton) WalkTargetValue(m *javamob.Mob, pos cube.Pos) float64 {
	return javamob.MonsterWalkTargetValue(m, pos)
}

// EncodeNBT is Skeleton.addAdditionalSaveData (the freezing conversion).
func (s *AbstractSkeleton) EncodeNBT(data map[string]any) {
	conv := int32(-1)
	if s.converting {
		conv = int32(s.convertTime)
	}
	data["StrayConversionTime"] = conv
	data["FreezingTime"] = int32(s.freezeTime)
}

// DecodeNBT is Skeleton.readAdditionalSaveData.
func (s *AbstractSkeleton) DecodeNBT(m *javamob.Mob, data map[string]any) {
	if v, ok := data["FreezingTime"].(int32); ok {
		s.freezeTime = int(v)
	}
	if v, ok := data["StrayConversionTime"].(int32); ok && v != -1 {
		s.convertTime = int(v)
		s.setConverting(m, true)
	}
	s.ReassessWeaponGoal(m)
}
