//go:build crossfly_full

package jnether

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
)

// ZombieAttackTurtleEgg is Zombie.ZombieAttackTurtleEggGoal (a
// RemoveBlockGoal on a MoveToBlockGoal): with mobGriefing, every 10-20 s
// look for turtle eggs with air above within 24 blocks and 3 up or down,
// walk onto them, stomp for 60 ticks and destroy them. Zombie.registerGoals
// adds it (priority 4, speed 1, vertical 3; Paper's
// zombies-target-turtle-eggs is on), so zombified piglins go for eggs too:
// gold farms lure them with turtle eggs.
type ZombieAttackTurtleEgg struct {
	javamob.GoalBase
	speed                     float64
	searchRange, verticalSize int

	nextStartTick int
	tryTicks      int
	maxStayTicks  int
	pos           cube.Pos
	reached       bool
	sinceReached  int
}

// NewZombieAttackTurtleEgg returns ZombieAttackTurtleEggGoal(mob, speed,
// verticalSearchRange).
func NewZombieAttackTurtleEgg(speed float64, vertical int) *ZombieAttackTurtleEgg {
	return &ZombieAttackTurtleEgg{
		GoalBase: javamob.GoalBase{Control: javamob.FlagMove | javamob.FlagJump},
		speed:    speed, searchRange: 24, verticalSize: vertical,
	}
}

func (g *ZombieAttackTurtleEgg) RequiresUpdateEveryTick() bool { return true }

// CanUse is RemoveBlockGoal.canUse.
func (g *ZombieAttackTurtleEgg) CanUse(m *javamob.Mob) bool {
	if !mobGriefing(m.Tx()) {
		return false
	}
	if g.nextStartTick > 0 {
		g.nextStartTick--
		return false
	}
	if g.findNearestBlock(m) {
		g.nextStartTick = javamob.ReducedTickDelay(20)
		return true
	}
	g.nextStartTick = javamob.ReducedTickDelay(200 + m.Rand().IntN(200))
	return false
}

// CanContinueToUse is MoveToBlockGoal.canContinueToUse.
func (g *ZombieAttackTurtleEgg) CanContinueToUse(m *javamob.Mob) bool {
	return g.tryTicks >= -g.maxStayTicks && g.tryTicks <= 1200 && validEggTarget(m.Tx(), g.pos)
}

// Start is MoveToBlockGoal.start + RemoveBlockGoal.start.
func (g *ZombieAttackTurtleEgg) Start(m *javamob.Mob) {
	m.Navigation().MoveTo(m, mgl64.Vec3{float64(g.pos[0]) + 0.5, float64(g.pos[1]) + 1, float64(g.pos[2]) + 0.5}, 1, g.speed)
	g.tryTicks = 0
	r := m.Rand()
	g.maxStayTicks = r.IntN(r.IntN(1200)+1200) + 1200
	g.sinceReached = 0
}

// Stop is RemoveBlockGoal.stop, which also sets the fall distance to 1
// (the base has no setter for it; the stomping hops are 0.3 high).
func (g *ZombieAttackTurtleEgg) Stop(*javamob.Mob) {}

// Tick is MoveToBlockGoal.tick + RemoveBlockGoal.tick.
func (g *ZombieAttackTurtleEgg) Tick(m *javamob.Mob) {
	target := g.pos.Side(cube.FaceUp)
	if target.Vec3Centre().Sub(m.Position()).LenSqr() >= 1.14*1.14 {
		g.reached = false
		g.tryTicks++
		if g.tryTicks%40 == 0 {
			m.Navigation().MoveTo(m, mgl64.Vec3{float64(target[0]) + 0.5, float64(target[1]), float64(target[2]) + 0.5}, 1, g.speed)
		}
	} else {
		g.reached = true
		g.tryTicks--
	}
	tx := m.Tx()
	eat, ok := eggNear(tx, cube.PosFromVec3(m.Position()))
	if !g.reached || !ok {
		return
	}
	v := m.Velocity()
	if g.sinceReached > 0 {
		m.SetVelocity(mgl64.Vec3{v[0], 0.3, v[2]})
	}
	if g.sinceReached%2 == 0 {
		v = m.Velocity()
		m.SetVelocity(mgl64.Vec3{v[0], -0.3, v[2]})
		if g.sinceReached%6 == 0 {
			tx.PlaySound(eat.Vec3Centre(), entitySound{event: "destroy_egg", id: "minecraft:zombie"})
		}
	}
	if g.sinceReached > 60 {
		tx.SetBlock(eat, nil, nil)
		tx.AddParticle(eat.Vec3Centre(), particle.Named{Bedrock: "minecraft:egg_destroy_emitter", Java: "poof"})
		tx.PlaySound(eat.Vec3Centre(), entitySound{event: "block.turtle_egg.break"})
	}
	g.sinceReached++
}

// eggNear is RemoveBlockGoal.getPosWithBlock: the egg at pos, below, beside
// or two below.
func eggNear(tx *world.Tx, pos cube.Pos) (cube.Pos, bool) {
	for _, p := range []cube.Pos{pos, pos.Side(cube.FaceDown), pos.Side(cube.FaceWest), pos.Side(cube.FaceEast),
		pos.Side(cube.FaceNorth), pos.Side(cube.FaceSouth), pos.Side(cube.FaceDown).Side(cube.FaceDown)} {
		if _, ok := tx.Block(p).(block.TurtleEgg); ok {
			return p, true
		}
	}
	return cube.Pos{}, false
}

// validEggTarget is RemoveBlockGoal.isValidTarget: an egg with two air
// blocks above.
func validEggTarget(tx *world.Tx, pos cube.Pos) bool {
	if _, ok := tx.Block(pos).(block.TurtleEgg); !ok {
		return false
	}
	_, a1 := tx.Block(pos.Side(cube.FaceUp)).(block.Air)
	_, a2 := tx.Block(pos.Add(cube.Pos{0, 2, 0})).(block.Air)
	return a1 && a2
}

// findNearestBlock is MoveToBlockGoal.findNearestBlock: layers 0, 1, -1,
// 2, -2 ... down to the vertical range, rings outwards, inside the home.
func (g *ZombieAttackTurtleEgg) findNearestBlock(m *javamob.Mob) bool {
	tx := m.Tx()
	mp := cube.PosFromVec3(m.Position())
	for y := 0; y <= g.verticalSize; {
		for r := 0; r < g.searchRange; r++ {
			for x := 0; x <= r; {
				z := 0
				if x < r && x > -r {
					z = r
				}
				for z <= r {
					p := mp.Add(cube.Pos{x, y - 1, z})
					if m.WithinHome(p) && validEggTarget(tx, p) {
						g.pos = p
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
