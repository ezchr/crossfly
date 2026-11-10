//go:build crossfly_full

package jpets

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/janimal"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// RegisterGoals is Cat.registerGoals (and the constructor's
// reassessTameGoals).
func (c *Cat) RegisterGoals(m *javamob.Mob) {
	gs, ts := m.Goals(), m.Targets()
	c.tempt = &catTempt{TemptGoal: janimal.NewTemptGoal[petHost](0.6, func(s item.Stack) bool { return IsCatFood(itemName(s)) }, true), c: c}
	gs.Add(1, goal.NewFloat(m))
	gs.Add(1, c.panicGoal(1.5))
	gs.Add(2, &sitWhenOrdered{t: &c.Tamable})
	gs.Add(3, &catRelaxOnOwner{c: c})
	gs.Add(4, c.tempt)
	gs.Add(5, &moveToBlock{speed: 1.1, searchRange: 8, verticalRange: 6, verticalStart: -2, flags: javamob.FlagJump | javamob.FlagMove,
		fixedStart: 40, valid: catCanLieOn, hooks: catLieHooks(c)})
	gs.Add(6, newFollowOwner(&c.Tamable, 1, 10, 5))
	gs.Add(7, &moveToBlock{speed: 0.8, searchRange: 8, verticalRange: 1, flags: javamob.FlagJump | javamob.FlagMove,
		valid: catCanSitOn, hooks: catSitHooks(c)})
	gs.Add(8, goal.NewLeapAtTarget(0.3))
	gs.Add(9, &ocelotAttack{})
	gs.Add(10, wrapGoal(&janimal.BreedGoal[petHost]{Speed: 0.8, CanMate: func(a, b petHost) bool {
		return catCanMate(a.m.Behaviour().(*Cat), b.m.Behaviour().(*Cat))
	}}))
	gs.Add(11, goal.NewWaterAvoidingRandomStroll(0.8, float32(1.0000001e-5)))
	gs.Add(12, goal.NewLookAtPlayer(10))
	rabbits := goal.NewNearestAttackableTarget(func(e world.Entity) bool { return entityID(e) == "minecraft:rabbit" }, false, false)
	ts.Add(1, &nonTameRandomTarget{t: &c.Tamable, NearestAttackableTarget: rabbits})
	turtles := goal.NewNearestAttackableTarget(isBabyTurtleOnLand, false, false)
	ts.Add(1, &nonTameRandomTarget{t: &c.Tamable, NearestAttackableTarget: turtles})
	// CatAvoidEntityGoal(Player, 16, 0.8, 1.33, NO_CREATIVE_OR_SPECTATOR).
	c.avoidPlayers = &catAvoidPlayers{c: c, AvoidEntity: goal.NewAvoidEntity(func(e world.Entity) bool {
		p, ok := e.(*player.Player)
		return ok && !p.GameMode().CreativeInventory() && p.GameMode() != world.GameModeSpectator
	}, 16, 0.8, 1.33)}
	c.reassessTameGoals(m)
}

func (c *Cat) panicGoal(speed float64) javamob.Goal {
	c.panic = &janimal.PanicGoal[petHost]{Speed: speed}
	return &tamablePanic{t: &c.Tamable, g: wrapGoal(c.panic)}
}

func (c *Cat) animal() *janimal.Animal { return &c.Animal }
func (c *Cat) panicking() bool         { return c.panic != nil && c.panic.Running() }

// breed is Cat.getBreedOffspring + spawnChildFromBreeding.
func (c *Cat) breed(m, partner *javamob.Mob) bool {
	baby, err := javamob.Spawn(m.Tx(), "minecraft:cat", m.Position(), javamob.SpawnBreeding, javamob.SpawnOptions{})
	if err != nil {
		return false
	}
	bc := baby.Behaviour().(*Cat)
	c.babyLook(m, partner.Behaviour().(*Cat), baby, bc)
	bc.Animal.SetBaby(true)
	baby.SetBaby(true)
	return true
}

// catAvoidPlayers is Cat.CatAvoidEntityGoal: only while wild.
type catAvoidPlayers struct {
	*goal.AvoidEntity
	c *Cat
}

func (g *catAvoidPlayers) CanUse(m *javamob.Mob) bool {
	return !g.c.Tame && g.AvoidEntity.CanUse(m)
}
func (g *catAvoidPlayers) CanContinueToUse(m *javamob.Mob) bool {
	return !g.c.Tame && g.AvoidEntity.CanContinueToUse(m)
}

// catTempt is Cat.CatTemptGoal: TemptGoal(0.6, cat food, canScare) for wild
// cats; a player the cat "picked" (1 in 600 goal ticks, forgotten 1 in 500)
// does not scare it.
type catTempt struct {
	*janimal.TemptGoal[petHost]
	c        *Cat
	selected *world.EntityHandle
}

func (g *catTempt) Flags() javamob.Flags { return javamob.Flags(g.TemptGoal.Flags()) }

func (g *catTempt) CanUse(m *javamob.Mob) bool {
	g.TemptGoal.CanScare = g.selected == nil || g.TemptGoal.Player() == nil || g.TemptGoal.Player().H() != g.selected
	return g.TemptGoal.CanUse(hostOf(m)) && !g.c.Tame
}
func (g *catTempt) CanContinueToUse(m *javamob.Mob) bool {
	g.TemptGoal.CanScare = g.selected == nil || g.TemptGoal.Player() == nil || g.TemptGoal.Player().H() != g.selected
	return g.TemptGoal.CanContinueToUse(hostOf(m))
}
func (g *catTempt) Start(m *javamob.Mob) { g.TemptGoal.Start(hostOf(m)) }
func (g *catTempt) Stop(m *javamob.Mob)  { g.TemptGoal.Stop(hostOf(m)) }
func (g *catTempt) Tick(m *javamob.Mob) {
	g.TemptGoal.Tick(hostOf(m))
	if g.selected == nil && m.Rand().IntN(javamob.ReducedTickDelay(600)) == 0 {
		if p := g.TemptGoal.Player(); p != nil {
			g.selected = p.H()
		}
	} else if m.Rand().IntN(javamob.ReducedTickDelay(500)) == 0 {
		g.selected = nil
	}
}

// tamablePanic is TamableAnimal.TamableAnimalPanicGoal: while panicking a pet
// far from its owner teleports to it.
type tamablePanic struct {
	t *Tamable
	g javamob.Goal
}

func (p *tamablePanic) Flags() javamob.Flags       { return p.g.Flags() }
func (p *tamablePanic) CanUse(m *javamob.Mob) bool { return p.g.CanUse(m) }
func (p *tamablePanic) CanContinueToUse(m *javamob.Mob) bool {
	return p.g.(javamob.GoalContinuer).CanContinueToUse(m)
}
func (p *tamablePanic) Start(m *javamob.Mob) { p.g.(javamob.GoalStarter).Start(m) }
func (p *tamablePanic) Stop(m *javamob.Mob)  { p.g.(javamob.GoalStopper).Stop(m) }
func (p *tamablePanic) Tick(m *javamob.Mob) {
	if !p.t.UnableToMoveToOwner(m) && p.t.ShouldTryTeleportToOwner(m) {
		p.t.TryToTeleportToOwner(m)
	}
	p.g.(javamob.GoalTicker).Tick(m)
}

// ocelotAttack is OcelotAttackGoal (MOVE, LOOK, every tick): creep up on the
// target, sprint the last 4 blocks, bite every 20 ticks.
type ocelotAttack struct {
	target     *world.EntityHandle
	attackTime int
}

func (g *ocelotAttack) Flags() javamob.Flags          { return javamob.FlagMove | javamob.FlagLook }
func (g *ocelotAttack) RequiresUpdateEveryTick() bool { return true }

func (g *ocelotAttack) CanUse(m *javamob.Mob) bool {
	t, ok := m.Target()
	if !ok {
		return false
	}
	g.target = t.H()
	return true
}

func (g *ocelotAttack) CanContinueToUse(m *javamob.Mob) bool {
	t, ok := g.target.Entity(m.Tx())
	return ok && !dead(t) && distSq(m.Position(), t.Position()) <= 225 && (!m.Navigation().Done() || g.CanUse(m))
}

func (g *ocelotAttack) Stop(m *javamob.Mob) {
	g.target = nil
	m.Navigation().Stop()
}

func (g *ocelotAttack) Tick(m *javamob.Mob) {
	t, ok := g.target.Entity(m.Tx())
	if !ok {
		return
	}
	m.LookControl().SetLookAtEntity(t, 30, 30)
	w := float64(float32(m.Width()) * 2)
	melee := w * w
	d := distSq(m.Position(), t.Position())
	speed := 0.8
	if d > melee && d < 16 {
		speed = 1.33
	} else if d < 225 {
		speed = 0.6
	}
	m.Navigation().MoveToEntity(m, t, speed)
	g.attackTime = max(g.attackTime-1, 0)
	if d <= melee && g.attackTime <= 0 {
		g.attackTime = 20
		m.DoHurtTarget(t)
	}
}

// ---- MoveToBlockGoal and the two cat goals built on it ----

// moveToBlockHooks are the overrides of a MoveToBlockGoal subclass.
type moveToBlockHooks struct {
	canUse      func(m *javamob.Mob) bool
	start, stop func(m *javamob.Mob)
	tick        func(m *javamob.Mob, reached bool)
}

// moveToBlock is MoveToBlockGoal: every 10-20 s (or fixedStart ticks) look
// for the nearest valid block within searchRange (verticalStart..range up and
// down, outward in Java's order), walk on top of it, stay there up to a
// minute or so.
type moveToBlock struct {
	speed                      float64
	searchRange, verticalRange int
	verticalStart              int
	flags                      javamob.Flags
	fixedStart                 int
	valid                      func(tx *world.Tx, pos cube.Pos) bool
	hooks                      moveToBlockHooks

	nextStartTick, tryTicks, maxStayTicks int
	blockPos                              cube.Pos
	reached                               bool
}

func (g *moveToBlock) Flags() javamob.Flags          { return g.flags }
func (g *moveToBlock) RequiresUpdateEveryTick() bool { return true }

func (g *moveToBlock) CanUse(m *javamob.Mob) bool {
	if g.hooks.canUse != nil && !g.hooks.canUse(m) {
		return false
	}
	if g.nextStartTick > 0 {
		g.nextStartTick--
		return false
	}
	if g.fixedStart > 0 {
		g.nextStartTick = g.fixedStart
	} else {
		g.nextStartTick = javamob.ReducedTickDelay(200 + m.Rand().IntN(200))
	}
	return g.findNearestBlock(m)
}

func (g *moveToBlock) CanContinueToUse(m *javamob.Mob) bool {
	return g.tryTicks >= -g.maxStayTicks && g.tryTicks <= 1200 && g.valid(m.Tx(), g.blockPos)
}

func (g *moveToBlock) Start(m *javamob.Mob) {
	m.Navigation().MoveTo(m, mgl64.Vec3{float64(g.blockPos[0]) + 0.5, float64(g.blockPos[1] + 1), float64(g.blockPos[2]) + 0.5}, 0, g.speed)
	g.tryTicks = 0
	r := m.Rand()
	g.maxStayTicks = r.IntN(r.IntN(1200)+1200) + 1200
	if g.hooks.start != nil {
		g.hooks.start(m)
	}
}

func (g *moveToBlock) Stop(m *javamob.Mob) {
	if g.hooks.stop != nil {
		g.hooks.stop(m)
	}
}

func (g *moveToBlock) Tick(m *javamob.Mob) {
	target := g.blockPos.Side(cube.FaceUp)
	centre := mgl64.Vec3{float64(target[0]) + 0.5, float64(target[1]) + 0.5, float64(target[2]) + 0.5}
	if centre.Sub(m.Position()).LenSqr() >= 1 {
		g.reached = false
		g.tryTicks++
		if g.tryTicks%40 == 0 {
			m.Navigation().MoveTo(m, mgl64.Vec3{float64(target[0]) + 0.5, float64(target[1]), float64(target[2]) + 0.5}, 0, g.speed)
		}
	} else {
		g.reached = true
		g.tryTicks--
	}
	if g.hooks.tick != nil {
		g.hooks.tick(m, g.reached)
	}
}

// findNearestBlock is MoveToBlockGoal.findNearestBlock.
func (g *moveToBlock) findNearestBlock(m *javamob.Mob) bool {
	mp := cube.PosFromVec3(m.Position())
	tx := m.Tx()
	for y := g.verticalStart; y <= g.verticalRange; {
		for r := 0; r < g.searchRange; r++ {
			for x := 0; x <= r; {
				z := 0
				if x < r && x > -r {
					z = r
				}
				for z <= r {
					pos := mp.Add(cube.Pos{x, y - 1, z})
					if m.WithinHome(pos) && g.valid(tx, pos) {
						g.blockPos = pos
						return true
					}
					if z > 0 {
						z = -z
					} else {
						z = 1 - z
					}
				}
				if x > 0 {
					x = -x
				} else {
					x = 1 - x
				}
			}
		}
		if y > 0 {
			y = -y
		} else {
			y = 1 - y
		}
	}
	return false
}

// catCanLieOn is CatLieOnBlockGoal.isValidTarget: a bed with air above.
func catCanLieOn(tx *world.Tx, pos cube.Pos) bool {
	if !isAirAt(tx, pos.Side(cube.FaceUp)) {
		return false
	}
	_, ok := tx.Block(pos).(block.Bed)
	return ok
}

// catCanSitOn is CatSitOnBlockGoal.isValidTarget: a chest nobody has open,
// a lit furnace or the foot of a bed, with air above.
func catCanSitOn(tx *world.Tx, pos cube.Pos) bool {
	if !isAirAt(tx, pos.Side(cube.FaceUp)) {
		return false
	}
	switch b := tx.Block(pos).(type) {
	case block.Chest:
		return b.Viewers() < 1
	case block.Furnace:
		return b.Lit
	case block.Bed:
		return !b.Head
	}
	return false
}

func isAirAt(tx *world.Tx, pos cube.Pos) bool {
	_, ok := tx.Block(pos).(block.Air)
	return ok
}

// catLieHooks are CatLieOnBlockGoal's overrides.
func catLieHooks(c *Cat) moveToBlockHooks {
	return moveToBlockHooks{
		canUse: func(*javamob.Mob) bool { return c.Tame && !c.OrderedToSit && !c.lying },
		start:  func(m *javamob.Mob) { c.SetInSittingPose(m, false) },
		stop:   func(m *javamob.Mob) { c.setLying(m, false) },
		tick: func(m *javamob.Mob, reached bool) {
			c.SetInSittingPose(m, false)
			if !reached {
				c.setLying(m, false)
			} else if !c.lying {
				c.setLying(m, true)
			}
		},
	}
}

// catSitHooks are CatSitOnBlockGoal's overrides.
func catSitHooks(c *Cat) moveToBlockHooks {
	return moveToBlockHooks{
		canUse: func(*javamob.Mob) bool { return c.Tame && !c.OrderedToSit },
		start:  func(m *javamob.Mob) { c.SetInSittingPose(m, false) },
		stop:   func(m *javamob.Mob) { c.SetInSittingPose(m, false) },
		tick:   func(m *javamob.Mob, reached bool) { c.SetInSittingPose(m, reached) },
	}
}

func dead(e world.Entity) bool {
	l, ok := e.(interface{ Dead() bool })
	return ok && l.Dead()
}

var _ = player.Player{}
