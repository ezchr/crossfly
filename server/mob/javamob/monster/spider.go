//go:build crossfly_full

package monster

import (
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/mob/javamob/path"
	"github.com/df-mc/dragonfly/server/world"
)

func init() {
	javamob.Register(javamob.Type{
		ID: "minecraft:spider", Category: javamob.CategoryMonster, Width: 1.4, Height: 0.9, EyeHeight: 0.65,
		NotInPeaceful: true, Arthropod: true, XPReward: 5,
		Families: []string{"spider", "arthropod", "monster", "mob"},
		Attributes: func() map[javamob.Attribute]float64 {
			a := javamob.MonsterAttributes()
			a[javamob.AttrMaxHealth] = 16
			a[javamob.AttrMovementSpeed] = float64(float32(0.3))
			return a
		},
		New: func() javamob.Behaviour { return &Spider{} },
	})
}

// ArmadilloScared reports whether an armadillo is rolled up (Armadillo.isScared); spiders only run from armadillos
// that are not. The package of the armadillo sets it.
var ArmadilloScared = func(world.Entity) bool { return false }

// Spider is Java's Spider: it climbs walls (WallClimberNavigation, climbing while it bumps into one), leaps at its
// target and stops chasing in the light.
type Spider struct{}

// RegisterGoals is Spider.registerGoals and createNavigation.
func (s *Spider) RegisterGoals(m *javamob.Mob) {
	m.SetNavigation(path.NewNavigation(path.WallClimber, m))
	m.Goals().Add(1, goal.NewFloat(m))
	armadillo := goal.NewAvoidEntity(func(e world.Entity) bool {
		return e.H().Type().EncodeEntity() == "minecraft:armadillo" && !ArmadilloScared(e)
	}, 6, 1, 1.2)
	m.Goals().Add(2, armadillo)
	m.Goals().Add(3, goal.NewLeapAtTarget(0.4))
	m.Goals().Add(4, newSpiderAttack())
	m.Goals().Add(5, goal.NewWaterAvoidingRandomStroll(0.8, 0.001))
	m.Goals().Add(6, goal.NewLookAtPlayer(8))
	m.Goals().Add(6, goal.NewRandomLookAround())
	m.Targets().Add(1, goal.NewHurtByTarget())
	m.Targets().Add(2, &spiderTarget{goal.NewNearestAttackableTarget(nil, true, false)})
	m.Targets().Add(3, &spiderTarget{goal.NewNearestAttackableTarget(goal.IsType("minecraft:iron_golem"), true, false)})
}

// PostTick is Spider.tick after super.tick: climbing while bumping into a wall.
func (s *Spider) PostTick(m *javamob.Mob) {
	if v := m.HorizontalCollision(); m.Meta().Climbing != v {
		m.UpdateMeta(func(meta *mob.Meta) { meta.Climbing = v })
	}
}

// OnClimbable is Spider.onClimbable: isClimbing.
func (s *Spider) OnClimbable(m *javamob.Mob) bool { return m.Meta().Climbing }

// IgnoresCobwebs is Spider.makeStuckInBlock: cobwebs do not slow spiders.
func (s *Spider) IgnoresCobwebs() bool { return true }

// FinalizeSpawn is Spider.finalizeSpawn: on hard, now and then a lasting speed, strength, regeneration or
// invisibility. (Spider jockeys are still to come.)
func (s *Spider) FinalizeSpawn(m *javamob.Mob, _ javamob.SpawnReason) {
	r := m.Rand()
	if m.DifficultyID() != 3 || !(r.Float32() < 0.1*float32(m.Difficulty().Special)) {
		return
	}
	var t effect.LastingType
	switch sel := r.IntN(5); {
	case sel <= 1:
		t = effect.Speed
	case sel <= 2:
		t = effect.Strength
	case sel <= 3:
		t = effect.Regeneration
	default:
		t = effect.Invisibility
	}
	m.AddEffect(effect.New(t, 1, time.Duration(1<<62)))
}

// WalkTargetValue is Monster.getWalkTargetValue.
func (s *Spider) WalkTargetValue(m *javamob.Mob, pos cube.Pos) float64 {
	return javamob.MonsterWalkTargetValue(m, pos)
}

// newSpiderAttack is SpiderAttackGoal: a melee attack that follows the target out of sight, and drops it now and
// then in the light.
func newSpiderAttack() *spiderAttack {
	return &spiderAttack{goal.NewMeleeAttack(1, true)}
}

type spiderAttack struct{ *goal.MeleeAttack }

func (g *spiderAttack) CanUse(m *javamob.Mob) bool {
	return g.MeleeAttack.CanUse(m) && len(m.Passengers()) == 0
}

func (g *spiderAttack) CanContinueToUse(m *javamob.Mob) bool {
	if m.LightLevelDependentMagicValue() >= 0.5 && m.Rand().IntN(100) == 0 {
		m.SetTarget(nil)
		return false
	}
	return g.MeleeAttack.CanContinueToUse(m)
}

// spiderTarget is SpiderTargetGoal: no new target in the light.
type spiderTarget struct{ *goal.NearestAttackableTarget }

func (g *spiderTarget) CanUse(m *javamob.Mob) bool {
	if m.LightLevelDependentMagicValue() >= 0.5 {
		return false
	}
	return g.NearestAttackableTarget.CanUse(m)
}
