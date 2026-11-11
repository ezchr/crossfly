//go:build crossfly_full

package hostile

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/jbrain"
	"github.com/df-mc/dragonfly/server/mob/jbrain/jmob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func init() {
	javamob.Register(javamob.Type{
		ID: creakingID, Category: javamob.CategoryMonster, Width: 0.9, Height: 2.7, EyeHeight: 2.3,
		Families: []string{"creaking", "monster", "mob"},
		Attributes: func() map[javamob.Attribute]float64 {
			a := javamob.MonsterAttributes()
			a[javamob.AttrMaxHealth], a[javamob.AttrMovementSpeed], a[javamob.AttrAttackDamage], a[javamob.AttrFollowRange], a[javamob.AttrStepHeight] = CreakingAttributes()
			return a
		},
		New: func() javamob.Behaviour { return &CreakingMob{} },
	})
}

// creakingProvider is Creaking.BRAIN_PROVIDER with CreakingAi's activities.
var creakingProvider = &jbrain.Provider[*javamob.Mob]{
	EntityType: "creaking",
	Sensors:    []*jbrain.SensorType[*javamob.Mob]{jmob.NearestLivingEntities, jmob.NearestPlayers},
	Activities: func(*javamob.Mob) []jbrain.ActivityData[*javamob.Mob] {
		canMove := func(m *javamob.Mob) bool { return creakingOf(m.Mob).CanMove() }
		return []jbrain.ActivityData[*javamob.Mob]{
			{Activity: jbrain.ActCore, Behaviours: jbrain.Seq[*javamob.Mob](0,
				creakingSwim(), jmob.LookAtTargetSink(45, 90), jmob.MoveToTargetSink(150, 250))},
			{Activity: jbrain.ActIdle, Behaviours: jbrain.Seq[*javamob.Mob](10,
				jmob.StartAttacking(func(_ *jbrain.Ctx, m *javamob.Mob) bool { return creakingOf(m.Mob).Active() }, nearestVisibleAttackablePlayer),
				jmob.SetEntityLookTargetSometimes(8, 30, 60, nil),
				jbrain.RunOne[*javamob.Mob](nil,
					jbrain.W[*javamob.Mob](jmob.RandomStroll(0.3), 2),
					jbrain.W[*javamob.Mob](jmob.SetWalkTargetFromLookTarget(0.3, 3), 2),
					jbrain.W[*javamob.Mob](jmob.DoNothing(30, 60), 1),
				),
			)},
			jbrain.ActivityErasing[*javamob.Mob](jbrain.ActFight, 10, jbrain.MemAttackTarget.ID(),
				jmob.SetWalkTargetFromAttackTargetIfTargetOutOfReach(func(*javamob.Mob) float32 { return 1 }),
				jmob.MeleeAttack(canMove, 40),
				jmob.StopAttackingIfTargetInvalid(func(c *jbrain.Ctx, t world.Entity) bool { return !attackTargetStillReachable(c, t) }, nil, false),
			),
		}
	},
}

// creakingSwim is the core Swim(0.8). Java runs it only while the creaking
// may move; a frozen creaking has no move control anyway (only floating up
// differs).
func creakingSwim() jmob.Control { return jmob.Swim(0.8) }

// nearestVisibleAttackablePlayer is the NEAREST_VISIBLE_ATTACKABLE_PLAYER
// memory.
func nearestVisibleAttackablePlayer(c *jbrain.Ctx, _ *javamob.Mob) (world.Entity, bool) {
	h, ok := jbrain.Get(c.Mem, jbrain.MemNearestVisibleAttackablePlayer)
	if !ok || h == nil {
		return nil, false
	}
	return h.Entity(c.Tx)
}

// attackTargetStillReachable is CreakingAi.isAttackTargetStillReachable: the
// target is one of the visible attackable players.
func attackTargetStillReachable(c *jbrain.Ctx, t world.Entity) bool {
	list, ok := jbrain.Get(c.Mem, jbrain.MemNearestVisibleAttackablePlayers)
	if !ok {
		return false
	}
	for _, h := range list {
		if h == t.H() {
			return true
		}
	}
	return false
}

// CreakingMob is a creaking without a heart (Crossfly has no creaking heart
// block yet, so creakings come from spawn eggs and commands): it moves only
// while no attackable player looks at it, activates on a player within 12
// blocks and dies to one hit (1 health). The heart-bound parts of creaking.go
// (invulnerability, tearing down) wait for the heart block.
type CreakingMob struct {
	brain *jmob.Brain
	c     *Creaking
}

// RegisterGoals makes the brain and the controls that only work while the
// creaking may move (CreakingLookControl, CreakingMoveControl,
// CreakingBodyRotationControl).
func (b *CreakingMob) RegisterGoals(m *javamob.Mob) {
	b.c = creakingOf(m.Mob)
	m.Navigation().SetCanFloat(true)
	b.brain = jmob.Attach(m, creakingProvider, nil)
	m.MoveControl().Custom = func(m *javamob.Mob, c *javamob.MoveControl) {
		if creakingOf(m.Mob).CanMove() {
			c.DefaultTick(m)
		}
	}
	m.LookControl().Custom = func(m *javamob.Mob, c *javamob.LookControl) {
		if creakingOf(m.Mob).CanMove() {
			c.DefaultTick(m)
		}
	}
	m.BodyRotationControl().Custom = func(m *javamob.Mob, c *javamob.BodyRotationControl) {
		if creakingOf(m.Mob).CanMove() {
			c.DefaultTick(m)
		}
	}
}

// AiStep is Creaking.aiStep before super.aiStep: checkCanMove over the
// nearest players.
func (b *CreakingMob) AiStep(m *javamob.Mob) {
	c := jmob.RunCtx(m)
	var players []creakingWatcher
	if list, ok := jbrain.Get(c.Mem, jbrain.MemNearestPlayers); ok {
		pos, scale := m.Position(), m.Height()/2.7
		gaze := creakingGazeHeights(pos[1], m.EyeY(), scale)
		for _, h := range list {
			e, ok := h.Entity(m.Tx())
			if !ok {
				continue
			}
			p, ok := e.(*player.Player)
			if !ok {
				continue
			}
			eye := p.Position().Add(mgl64.Vec3{0, p.EyeHeight()})
			w := creakingWatcher{
				attackable: attackablePlayer(p),
				disguised:  carvedPumpkin(p.Armour().Helmet()),
				distSq:     p.Position().Sub(pos).LenSqr(),
				h:          p.H(),
			}
			rot := p.Rotation()
			w.looking = lookingAt(pos, viewVector(rot.Yaw(), rot.Pitch()), eye, 0.5, gaze, func(to mgl64.Vec3) bool {
				return javamob.Clear(m.Tx(), eye, to)
			})
			players = append(players, w)
		}
	}
	CreakingAIStep(m.Mob, players,
		func(h *world.EntityHandle) { jbrain.Set(c.Mem, jbrain.MemAttackTarget, h); m.PlaySound("activate") },
		func() { c.Mem.Erase(jbrain.MemAttackTarget.ID()); m.PlaySound("deactivate") },
		m.StopInPlace,
		func() {},
	)
}

// attackablePlayer is Creaking.canAttack for a player: survival or
// adventure, alive.
func attackablePlayer(p *player.Player) bool {
	gm := p.GameMode()
	return !p.Dead() && gm.AllowsTakingDamage() && gm.Visible()
}

func carvedPumpkin(s item.Stack) bool {
	if s.Empty() {
		return false
	}
	name, _ := s.Item().EncodeItem()
	return name == "minecraft:carved_pumpkin"
}

// CustomServerAiStep is CreakingAi.updateActivity (the base ticks the brain
// right after).
func (b *CreakingMob) CustomServerAiStep(m *javamob.Mob) {
	if !creakingOf(m.Mob).CanMove() {
		b.brain.UseDefaultActivity()
		return
	}
	b.brain.SetActiveActivityToFirstValid(jbrain.ActFight, jbrain.ActIdle)
}

// DoHurtTarget is Creaking.doHurtTarget: the attack animation, then the base
// hit.
func (b *CreakingMob) DoHurtTarget(m *javamob.Mob, target world.Entity) bool {
	CreakingDoHurtTarget(m.Mob)
	return m.BaseDoHurtTarget(target)
}

// PostTick is Creaking.tick: a heart-bound creaking without its heart dies.
func (b *CreakingMob) PostTick(m *javamob.Mob) { CreakingTick(m.Mob) }

// EncodeNBT and DecodeNBT save the home position.
func (b *CreakingMob) EncodeNBT(data map[string]any)                 { b.c.EncodeNBT(data) }
func (b *CreakingMob) DecodeNBT(m *javamob.Mob, data map[string]any) { b.c.DecodeNBT(m.Mob, data) }
