//go:build crossfly_full

package hostile

import (
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// The water mobs and the phantom on the Java mob base.
func init() {
	for _, glow := range []bool{false, true} {
		id, cat, fam := squidID, javamob.CategoryWaterCreature, "squid"
		if glow {
			id, cat, fam = glowSquidID, javamob.CategoryUndergroundWaterCreature, "glow_squid"
		}
		javamob.Register(javamob.Type{
			ID: id, Category: cat, Width: 0.8, Height: 0.8, EyeHeight: 0.4, Aquatic: true, XPReward: 1,
			Families: []string{fam, "squid", "mob"},
			Attributes: func() map[javamob.Attribute]float64 {
				a := javamob.LivingAttributes()
				a[javamob.AttrMaxHealth] = SquidMaxHealth
				return a
			},
			New: func() javamob.Behaviour { return &SquidMob{air: 300} },
		})
	}
}

// SquidMob is Squid and GlowSquid on the base: the goals, aiStep and travel
// of squid.go, and WaterAnimal's air out of water.
type SquidMob struct{ air int }

// RegisterGoals is Squid.registerGoals.
func (s *SquidMob) RegisterGoals(m *javamob.Mob) {
	m.Goals().Add(0, goalOf(squidRandomMovementGoal{}))
	m.Goals().Add(1, goalOf(&squidFleeGoal{}))
}

// Tick is the air part of WaterAnimal.baseTick.
func (s *SquidMob) Tick(m *javamob.Mob) {
	if !m.Dead() && waterAnimalAir(&s.air, m.InWater()) {
		m.Hurt(2, DryOutDamageSource{})
	}
}

// PostTick is the rest of Squid.aiStep (after LivingEntity.aiStep) and
// GlowSquid.aiStep.
func (s *SquidMob) PostTick(m *javamob.Mob) {
	SquidAIStep(m.Mob)
	if m.JavaType().ID == glowSquidID {
		GlowSquidAIStep(m.Mob)
	}
}

// Travel is Squid.travel: move(SELF, getDeltaMovement()).
func (s *SquidMob) Travel(m *javamob.Mob, _ mgl64.Vec3) bool {
	SquidTravel(m.Mob)
	return true
}

// OnHurt is the end of Squid/GlowSquid.hurtServer: ink when hurt by a mob.
func (s *SquidMob) OnHurt(m *javamob.Mob, _ float64, _ world.DamageSource) {
	_, byMob := m.LastHurtByMob()
	SquidHurt(m.Mob, byMob, nil)
}

// DryOutDamageSource is DamageTypes.DRY_OUT (Paper cause DRYOUT): a water
// animal out of water too long.
type DryOutDamageSource struct{}

func (DryOutDamageSource) ReducedByArmour() bool     { return false }
func (DryOutDamageSource) ReducedByResistance() bool { return true }
func (DryOutDamageSource) Fire() bool                { return false }
func (DryOutDamageSource) IgnoreTotem() bool         { return false }

// waterAnimalXP is WaterAnimal.getBaseExperienceReward: 1 + random(3).
func waterAnimalXP(m *javamob.Mob) int { return 1 + m.Rand().IntN(3) }

// XPReward is WaterAnimal.getBaseExperienceReward.
func (s *SquidMob) XPReward(m *javamob.Mob, _ int) int { return waterAnimalXP(m) }

// XPReward is WaterAnimal.getBaseExperienceReward.
func (b *FishMob) XPReward(m *javamob.Mob, _ int) int { return waterAnimalXP(m) }
