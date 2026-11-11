//go:build crossfly_full

package hostile

import (
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/mob/javamob/path"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func init() {
	for _, elder := range []bool{false, true} {
		id, w, eye, fam := guardianID, 0.85, 0.425, "guardian"
		if elder {
			id, w, eye, fam = elderGuardianID, 1.9975, 0.99875, "elder_guardian"
		}
		javamob.Register(javamob.Type{
			ID: id, Category: javamob.CategoryMonster, Width: w, Height: w, EyeHeight: eye, Aquatic: true,
			NotInPeaceful: true, XPReward: 10,
			Families: []string{fam, "monster", "mob"},
			Attributes: func() map[javamob.Attribute]float64 {
				a := javamob.MonsterAttributes()
				a[javamob.AttrMaxHealth], a[javamob.AttrMovementSpeed], a[javamob.AttrAttackDamage] = GuardianAttributes(elder)
				return a
			},
			New: func() javamob.Behaviour { return &GuardianMob{elder: elder} },
		})
	}
}

// GuardianMob is Guardian and ElderGuardian on the base. The per-mob state
// is the Guardian of guardian.go.
type GuardianMob struct{ elder bool }

// RegisterGoals is Guardian.registerGoals (and the constructor's water
// malus, navigation and move control). MoveTowardsRestrictionGoal is left
// out: nothing in Crossfly gives a guardian a home (Java: none either unless
// a structure or command restricts it).
func (b *GuardianMob) RegisterGoals(m *javamob.Mob) {
	g := guardianOf(m.Mob)
	m.SetNavigation(path.NewWaterNavigation(m, false))
	m.SetPathfindingMalus(path.Water, 0)
	m.MoveControl().Custom = guardianMoveTick
	if b.elder {
		m.SetPersistenceRequired() // ElderGuardian's constructor
	}
	stroll := goal.NewRandomStroll(1)
	stroll.Interval = 80
	stroll.Control = javamob.FlagMove | javamob.FlagLook
	g.stroll = stroll
	m.Goals().Add(4, goalOf(&guardianAttackGoal{elder: b.elder}))
	m.Goals().Add(7, stroll)
	m.Goals().Add(8, goal.NewLookAtPlayer(8))
	lookGuardian := goal.NewLookAtPlayer(12)
	lookGuardian.Probability = 0.01
	lookGuardian.Match = func(e world.Entity) bool { id := entityID(e); return id == guardianID || id == elderGuardianID }
	m.Goals().Add(8, lookGuardian)
	m.Goals().Add(9, goal.NewRandomLookAround())
	t := &guardianTargetGoal{}
	t.NearestAttackableTarget = goal.NewNearestAttackableTarget(func(e world.Entity) bool {
		_, living := e.(entity.Living)
		return living
	}, true, false)
	t.Selector = func(e world.Entity, _ *world.Tx) bool { return t.cur != nil && guardianAttackSelector(t.cur, e) }
	m.Targets().Add(1, t)
}

// guardianTargetGoal is NearestAttackableTargetGoal with the
// GuardianAttackSelector, which needs the guardian.
type guardianTargetGoal struct {
	*goal.NearestAttackableTarget
	cur *mob.Mob
}

func (g *guardianTargetGoal) CanUse(m *javamob.Mob) bool {
	g.cur = m.Mob
	defer func() { g.cur = nil }()
	return g.NearestAttackableTarget.CanUse(m)
}

// AiStep is the server part of Guardian.aiStep before super.aiStep.
func (b *GuardianMob) AiStep(m *javamob.Mob) { GuardianAIStep(m.Mob) }

// Travel is Guardian.travel: travelInWater in water.
func (b *GuardianMob) Travel(m *javamob.Mob, input mgl64.Vec3) bool {
	if !m.InWater() {
		return false
	}
	GuardianTravelInWater(m.Mob, input)
	return true
}

// FilterDamage is the start of Guardian.hurtServer: thorns for a living
// attacker hitting it directly while the spikes are out, and the stroll
// trigger.
func (b *GuardianMob) FilterDamage(m *javamob.Mob, d *mob.Damage) {
	var direct world.Entity
	if a, ok := d.Source.(entity.AttackDamageSource); ok {
		direct = a.Attacker
	}
	_, thorns := d.Source.(thornsSource)
	_, magic := d.Source.(indirectMagicSource)
	GuardianHurt(m.Mob, direct, thorns || magic)
}

// CustomServerAiStep is ElderGuardian.customServerAiStep: the curse.
func (b *GuardianMob) CustomServerAiStep(m *javamob.Mob) {
	if b.elder {
		ElderGuardianServerAIStep(m.Mob)
	}
}
