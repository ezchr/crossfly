//go:build crossfly_full

package jnether

import (
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/mob/javamob/path"
	"github.com/go-gl/mathgl/mgl64"
)

// The blaze (Blaze, Paper 26.3): it floats down slowly (0.6 of its fall
// speed), climbs towards a target above its height offset (0.5 to 6.9,
// picked anew every 100 ticks), and attacks with BlazeAttackGoal: melee
// within 2 blocks, else a volley of three small fireballs (charge 60 ticks,
// 6 ticks between shots, 100 ticks rest) at targets in its follow range
// (48) it can see. Water hurts it; it is fire immune. Blaze farms work on
// the spawner (spawning) with the volleys and fall behaviour here.

func init() {
	javamob.Register(javamob.Type{
		ID: "minecraft:blaze", Category: javamob.CategoryMonster,
		Width: 0.6, Height: 1.8, EyeHeight: 1.8 * 0.85,
		FireImmune: true, NotInPeaceful: true,
		Attributes: func() map[javamob.Attribute]float64 {
			a := javamob.MonsterAttributes()
			a[javamob.AttrAttackDamage] = 6
			a[javamob.AttrMovementSpeed] = float64(float32(0.23))
			a[javamob.AttrFollowRange] = 48
			return a
		},
		Families: []string{"blaze", "monster", "mob"},
		XPReward: 10,
		New:      func() javamob.Behaviour { return &Blaze{heightOffset: 0.5} },
	})
}

// Blaze is the Blaze class.
type Blaze struct {
	heightOffset   float64
	nextOffsetTick int
	charged        bool
}

// RegisterGoals is Blaze.registerGoals.
func (b *Blaze) RegisterGoals(m *javamob.Mob) {
	m.SetPathfindingMalus(path.Water, -1)
	m.SetPathfindingMalus(path.Lava, 8)
	m.SetPathfindingMalus(path.FireInNeighbour, 0)
	m.SetPathfindingMalus(path.Fire, 0)
	m.Goals().Add(4, &blazeAttack{b: b})
	// MoveTowardsRestrictionGoal(5) only matters with a home (spawner
	// mobs have none in Java either).
	m.Goals().Add(7, goal.NewWaterAvoidingRandomStroll(1, 0))
	m.Goals().Add(8, goal.NewLookAtPlayer(8))
	m.Goals().Add(8, goal.NewRandomLookAround())
	m.Targets().Add(1, goal.NewHurtByTarget().SetAlertOthers())
	m.Targets().Add(2, goal.NewNearestAttackableTarget(nil, true, false))
}

// AiStep is Blaze.aiStep before super: a slow fall.
func (b *Blaze) AiStep(m *javamob.Mob) {
	if v := m.Velocity(); !m.OnGround() && v[1] < 0 {
		m.SetVelocity(mgl64.Vec3{v[0], v[1] * 0.6, v[2]})
	}
}

// SensitiveToWater is Blaze.isSensitiveToWater.
func (b *Blaze) SensitiveToWater(*javamob.Mob) bool { return true }

// CustomServerAiStep is Blaze.customServerAiStep: a new height offset
// (triangle(0.5, 6.891)) every 100 ticks; with a target whose eyes are
// higher than the blaze's eyes + the offset, rise towards 0.3 a tick.
func (b *Blaze) CustomServerAiStep(m *javamob.Mob) {
	b.nextOffsetTick--
	if b.nextOffsetTick <= 0 {
		b.nextOffsetTick = 100
		r := m.Rand()
		b.heightOffset = 0.5 + 6.891*(r.Float64()-r.Float64())
	}
	t, ok := m.Target()
	if !ok {
		return
	}
	if t.Position()[1]+eyeHeight(t) > m.EyeY()+b.heightOffset && canAttackDefault(t) {
		v := m.Velocity()
		m.SetVelocity(v.Add(mgl64.Vec3{0, (0.3 - v[1]) * 0.3, 0}))
	}
}

// setCharged is Blaze.setCharged: the blaze burns (DATA_FLAGS_ID 1) while
// a volley charges. Both clients draw it from Meta.Charged / the on-fire
// flag.
func (b *Blaze) setCharged(m *javamob.Mob, v bool) {
	b.charged = v
	if m.Meta().Charged != v {
		m.UpdateMeta(func(meta *mob.Meta) { meta.Charged = v })
	}
}

// blazeAttack is Blaze.BlazeAttackGoal.
type blazeAttack struct {
	javamob.GoalBase
	b                            *Blaze
	attackStep, attackTime, seen int
}

func (g *blazeAttack) Flags() javamob.Flags          { return javamob.FlagMove | javamob.FlagLook }
func (g *blazeAttack) RequiresUpdateEveryTick() bool { return true }

func (g *blazeAttack) CanUse(m *javamob.Mob) bool {
	t, ok := m.Target()
	return ok && alive(t) && canAttackDefault(t)
}

func (g *blazeAttack) Start(*javamob.Mob) { g.attackStep = 0 }
func (g *blazeAttack) Stop(m *javamob.Mob) {
	g.b.setCharged(m, false)
	g.seen = 0
}

func (g *blazeAttack) Tick(m *javamob.Mob) {
	g.attackTime--
	t, ok := m.Target()
	if !ok {
		return
	}
	los := m.Sensing().HasLineOfSight(m, t)
	if los {
		g.seen = 0
	} else {
		g.seen++
	}
	tp, pos := t.Position(), m.Position()
	dist := tp.Sub(pos).LenSqr()
	follow := m.FollowRange()
	switch {
	case dist < 4:
		if !los {
			return
		}
		if g.attackTime <= 0 {
			g.attackTime = 20
			m.DoHurtTarget(t)
		}
		m.MoveControl().SetWantedPosition(tp[0], tp[1], tp[2], 1)
	case dist < follow*follow && los:
		xd := tp[0] - pos[0]
		yd := tp[1] + t.H().Type().BBox(t).Height()*0.5 - (pos[1] + m.Height()*0.5)
		zd := tp[2] - pos[2]
		if g.attackTime <= 0 {
			g.attackStep++
			switch {
			case g.attackStep == 1:
				g.attackTime = 60
				g.b.setCharged(m, true)
			case g.attackStep <= 4:
				g.attackTime = 6
			default:
				g.attackTime = 100
				g.attackStep = 0
				g.b.setCharged(m, false)
			}
			if g.attackStep > 1 {
				sqd := sqrt(sqrt(dist)) * 0.5
				m.Tx().PlaySound(pos, entitySound{event: "shoot", id: "minecraft:blaze"})
				r := m.Rand()
				dir := mgl64.Vec3{xd + 2.297*sqd*(r.Float64()-r.Float64()), yd, zd + 2.297*sqd*(r.Float64()-r.Float64())}
				SpawnHurting(m.Tx(), SmallFireballType, HurtingOpts{
					Owner: m, Pos: mgl64.Vec3{pos[0], pos[1] + m.Height()*0.5 + 0.5, pos[2]}, Dir: normalize(dir),
					Incendiary: mobGriefing(m.Tx()),
				})
			}
		}
		m.LookControl().SetLookAtEntity(t, 10, 10)
	case g.seen < 5:
		m.MoveControl().SetWantedPosition(tp[0], tp[1], tp[2], 1)
	}
}
