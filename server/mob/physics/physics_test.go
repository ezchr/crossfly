//go:build crossfly_full

package physics

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// The ground speed of a walking mob is speed x 0.98 x speed per tick of
// acceleration against 0.546 friction: 0.98 s^2 / (1 - 0.546) blocks a tick.
func TestWalkingSpeed(t *testing.T) {
	run := testWorld(t)
	for _, speed := range []float64{0.2, 0.23, 0.25, 0.3} {
		run(func(tx *world.Tx) {
			floor(tx, 40)
			m := spawn(t, tx, "test:walker", mgl64.Vec3{-30.5, 1, 0.5}, 0.6, 1.8, speed)
			walk(m, towardsPosX)
			tick(tx, m, 40)
			x0 := m.Pos()[0]
			tick(tx, m, 20)
			got := (m.Pos()[0] - x0) / 20
			want := 0.98 * speed * speed / (1 - 0.546)
			t.Logf("speed %.2f: %.4f blocks/tick (%.2f blocks/s), want %.4f", speed, got, got*20, want)
			if math.Abs(got-want) > want*0.01 {
				t.Errorf("speed %v: moved %.4f a tick, want %.4f", speed, got, want)
			}
			if math.Abs(m.Pos()[1]-1) > 1e-9 || !m.OnGround() {
				t.Errorf("speed %v: left the ground: y %v", speed, m.Pos()[1])
			}
			m.Close()
		})
	}
}

// A mob walks up a slab (0.5 < max auto step 0.5625) but not up a full
// block (it would have to jump).
func TestStepUp(t *testing.T) {
	run := testWorld(t)
	run(func(tx *world.Tx) {
		floor(tx, 10)
		for x := 3; x <= 9; x++ {
			for z := -3; z <= 3; z++ {
				tx.SetBlock(cube.Pos{x, 1, z}, block.Slab{Block: block.Stone{}}, nil)
			}
		}
		m := spawn(t, tx, "test:walker", mgl64.Vec3{0.5, 1, 0.5}, 0.6, 1.8, 0.25)
		walk(m, towardsPosX)
		tick(tx, m, 40)
		if p := m.Pos(); math.Abs(p[1]-1.5) > 1e-9 || p[0] < 4 {
			t.Errorf("slab: ended at %v, want on the slab (y 1.5, x > 4)", p)
		}
		m.Close()
	})
	run(func(tx *world.Tx) {
		floor(tx, 10)
		for z := -3; z <= 3; z++ {
			tx.SetBlock(cube.Pos{3, 1, z}, block.Stone{}, nil)
		}
		m := spawn(t, tx, "test:walker", mgl64.Vec3{0.5, 1, 0.5}, 0.6, 1.8, 0.25)
		walk(m, towardsPosX)
		tick(tx, m, 40)
		if p := m.Pos(); math.Abs(p[1]-1) > 1e-9 || math.Abs(p[0]-2.7) > 1e-6 {
			t.Errorf("full block: ended at %v, want stopped at the wall (x 2.7, y 1)", p)
		}
		if !m.Physics().HorizontalCollision {
			t.Errorf("full block: no horizontal collision")
		}
		m.Close()
	})
	// A jump (0.42) clears a full block.
	run(func(tx *world.Tx) {
		floor(tx, 10)
		for z := -3; z <= 3; z++ {
			tx.SetBlock(cube.Pos{3, 1, z}, block.Stone{}, nil)
		}
		m := spawn(t, tx, "test:walker", mgl64.Vec3{0.5, 1, 0.5}, 0.6, 1.8, 0.25)
		walk(m, towardsPosX)
		for i := 0; i < 40; i++ {
			m.Input().Jump = m.Physics().HorizontalCollision
			tick(tx, m, 1)
		}
		if p := m.Pos(); math.Abs(p[1]-2) > 1e-9 || p[0] < 4 {
			t.Errorf("jump: ended at %v, want on top of the block (y 2)", p)
		}
		m.Close()
	})
}

// fall drops a mob from height above a stone floor (or lander) and returns
// the fall distance it landed with and the damage taken.
func fall(t *testing.T, tx *world.Tx, height float64, under world.Block) (dist, dmg float64) {
	floor(tx, 3)
	if under != nil {
		tx.SetBlock(cube.Pos{0, 0, 0}, under, nil)
	}
	m := spawn(t, tx, "test:walker", mgl64.Vec3{0.5, 1 + height, 0.5}, 0.6, 1.8, 0.25)
	defer m.Close()
	for i := 0; i < 200 && !m.OnGround(); i++ {
		dist = m.Physics().FallDistance
		tick(tx, m, 1)
	}
	if !m.OnGround() {
		fail(t, "fell %v: never landed", height)
	}
	if m.Physics().FallDistance != 0 {
		t.Errorf("fall distance not reset on landing: %v", m.Physics().FallDistance)
	}
	return dist, 20 - m.Health()
}

// Fall damage is ceil(distance - 3); a hay bale takes 80% of the distance
// away.
func TestFallDamage(t *testing.T) {
	run := testWorld(t)
	for _, c := range []struct {
		height   float64
		min, max float64
	}{{2, 0, 0}, {3, 0, 0}, {4, 1, 1}, {10, 6, 7}, {20, 16, 17}} {
		run(func(tx *world.Tx) {
			dist, dmg := fall(t, tx, c.height, nil)
			want := math.Max(math.Ceil(dist-3), 0)
			t.Logf("drop %v: fall distance %.3f, damage %v", c.height, dist, dmg)
			if dmg != want || dmg < c.min || dmg > c.max {
				t.Errorf("drop %v (distance %.3f): damage %v, want %v in [%v, %v]", c.height, dist, dmg, want, c.min, c.max)
			}
		})
	}
	run(func(tx *world.Tx) {
		if dist, dmg := fall(t, tx, 10, block.HayBale{}); dmg != 0 {
			t.Errorf("hay bale: drop 10 (distance %.3f): damage %v, want 0", dist, dmg)
		}
	})
}

// A falling mob's velocity is v' = (v - 0.08) * 0.98, so after n ticks
// -3.92 * (1 - 0.98^n), approaching 3.92 blocks a tick.
func TestTerminalVelocity(t *testing.T) {
	run := testWorld(t)
	run(func(tx *world.Tx) {
		m := spawn(t, tx, "test:walker", mgl64.Vec3{0.5, 319, 0.5}, 0.6, 1.8, 0.25)
		tick(tx, m, 1)
		if v := m.Vel()[1]; math.Abs(v-(-0.0784)) > 1e-9 {
			t.Errorf("first tick: vy %v, want -0.0784", v)
		}
		tick(tx, m, 119)
		if v, want := m.Vel()[1], -3.92*(1-math.Pow(0.98, 120)); math.Abs(v-want) > 1e-6 {
			t.Errorf("vy after 120 ticks %v, want %v", v, want)
		}
		m.Close()
	})
}

// pool makes a water pool of the given depth over a stone floor.
func pool(tx *world.Tx, depth int) {
	floor(tx, 4)
	for x := -4; x <= 4; x++ {
		for z := -4; z <= 4; z++ {
			for y := 1; y <= depth; y++ {
				tx.SetBlock(cube.Pos{x, y, z}, block.Water{Depth: 8, Still: true}, nil)
			}
		}
	}
}

// Without jumping a mob sinks (drag 0.8 then water gravity 0.02: at most 0.1
// a tick); jumping (behavior.float) lifts it to the surface where it bobs.
func TestSwimAndSink(t *testing.T) {
	run := testWorld(t)
	run(func(tx *world.Tx) {
		pool(tx, 6)
		m := spawn(t, tx, "test:walker", mgl64.Vec3{0.5, 4, 0.5}, 0.9, 1.3, 0.25)
		tick(tx, m, 1)
		if !m.Physics().InWater {
			fail(t, "not in water")
		}
		minVel := 0.0
		for i := 0; i < 100; i++ {
			tick(tx, m, 1)
			minVel = math.Min(minVel, m.Vel()[1])
		}
		if p := m.Pos(); math.Abs(p[1]-1) > 1e-9 || !m.OnGround() {
			t.Errorf("sinking: ended at y %v, want on the floor (1)", p[1])
		}
		if minVel < -0.1-1e-9 {
			t.Errorf("sinking faster than 0.1 a tick: %v", minVel)
		}
		if !m.Physics().Underwater {
			t.Errorf("eyes at %v not under water", m.EyePosition())
		}
		for i := 0; i < 200; i++ {
			m.Input().Jump = true
			tick(tx, m, 1)
		}
		y := m.Pos()[1]
		t.Logf("floating: feet at %.3f (surface 6.889)", y)
		if y < 5.5 || y > 6.9 {
			t.Errorf("floating: feet at %v, want bobbing near the surface (5.5..6.9)", y)
		}
		m.Close()
	})
	// A mob whose navigation cannot sink floats in place under water.
	run(func(tx *world.Tx) {
		pool(tx, 6)
		m := spawn(t, tx, "test:fish", mgl64.Vec3{0.5, 3, 0.5}, 0.5, 0.3, 0.1)
		tick(tx, m, 100)
		if y := m.Pos()[1]; math.Abs(y-3) > 1e-6 {
			t.Errorf("fish: drifted to y %v, want 3", y)
		}
		m.Close()
	})
}

// A spider (navigation.climb) walking into a wall climbs it at about 0.2 a
// tick and shows as climbing; a mob with can_climb climbs a ladder, one
// without it does not.
func TestClimbing(t *testing.T) {
	run := testWorld(t)
	run(func(tx *world.Tx) {
		floor(tx, 12)
		for x := 3; x <= 12; x++ {
			for y := 1; y <= 5; y++ {
				for z := -3; z <= 3; z++ {
					tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, nil)
				}
			}
		}
		m := spawn(t, tx, "test:spider", mgl64.Vec3{0.5, 1, 0.5}, 1.4, 0.9, 0.3)
		walk(m, towardsPosX)
		climbing := false
		for i := 0; i < 80; i++ {
			tick(tx, m, 1)
			climbing = climbing || m.Meta().Climbing
		}
		if !climbing {
			t.Error("spider never showed as climbing")
		}
		if p := m.Pos(); math.Abs(p[1]-6) > 1e-9 || p[0] < 3 {
			t.Errorf("spider: ended at %v, want on top of the wall (y 6)", p)
		}
		if m.Meta().Climbing {
			t.Error("spider still climbing on top of the wall")
		}
		m.Close()
	})
	ladder := func(tx *world.Tx) {
		floor(tx, 8)
		for y := 1; y <= 6; y++ {
			tx.SetBlock(cube.Pos{3, y, 0}, block.Stone{}, nil)
			tx.SetBlock(cube.Pos{2, y, 0}, block.Ladder{Facing: cube.West}, nil)
		}
	}
	run(func(tx *world.Tx) {
		ladder(tx)
		m := spawn(t, tx, "test:climber", mgl64.Vec3{0.5, 1, 0.5}, 0.6, 1.8, 0.25)
		walk(m, towardsPosX)
		tick(tx, m, 30)
		if y := m.Pos()[1]; y < 3 {
			t.Errorf("can_climb: at y %v after 30 ticks on a ladder, want > 3", y)
		}
		m.Close()
	})
	run(func(tx *world.Tx) {
		ladder(tx)
		m := spawn(t, tx, "test:walker", mgl64.Vec3{0.5, 1, 0.5}, 0.6, 1.8, 0.25)
		walk(m, towardsPosX)
		tick(tx, m, 30)
		if y := m.Pos()[1]; y != 1 {
			t.Errorf("no can_climb: climbed to y %v", y)
		}
		m.Close()
	})
}

// knockDistance knocks a standing mob with Dragonfly's melee values and
// returns how far it slid and how high it went.
func knockDistance(t *testing.T, tx *world.Tx, resistance float64) (dist, height float64) {
	floor(tx, 12)
	m := spawn(t, tx, "test:walker", mgl64.Vec3{0.5, 1, 0.5}, 0.6, 1.8, 0.25)
	defer m.Close()
	m.Attributes().SetBase(mob.AttrKnockbackResistance, resistance)
	tick(tx, m, 2)
	start := m.Pos()
	m.KnockBack(start.Sub(mgl64.Vec3{1, 0, 0}), 0.45, 0.3608)
	for i := 0; i < 60; i++ {
		tick(tx, m, 1)
		height = math.Max(height, m.Pos()[1]-start[1])
	}
	return m.Pos()[0] - start[0], height
}

func TestKnockBack(t *testing.T) {
	run := testWorld(t)
	var d0, d5 float64
	run(func(tx *world.Tx) {
		var h float64
		d0, h = knockDistance(t, tx, 0)
		t.Logf("resistance 0: %.3f blocks, %.3f high", d0, h)
		if d0 < 2 || d0 > 3.5 || h < 0.5 || h > 0.8 {
			t.Errorf("resistance 0: knocked %.3f blocks and %.3f high, want 2..3.5 and 0.5..0.8", d0, h)
		}
	})
	run(func(tx *world.Tx) {
		var h float64
		d5, h = knockDistance(t, tx, 0.5)
		t.Logf("resistance 0.5: %.3f blocks, %.3f high", d5, h)
		if d5 >= d0*0.6 || d5 < d0*0.3 {
			t.Errorf("resistance 0.5: knocked %.3f blocks, want about a third to a half of %.3f", d5, d0)
		}
	})
	run(func(tx *world.Tx) {
		if d, h := knockDistance(t, tx, 1); d != 0 || h != 0 {
			t.Errorf("resistance 1: moved %v blocks and %v high", d, h)
		}
	})
}

// Two pushable mobs in one spot push each other apart; a mob that is not
// pushable is not moved but still pushes.
func TestPush(t *testing.T) {
	run := testWorld(t)
	run(func(tx *world.Tx) {
		floor(tx, 6)
		a := spawn(t, tx, "test:walker", mgl64.Vec3{0.45, 1, 0.5}, 0.6, 1.8, 0.25)
		b := spawn(t, tx, "test:walker", mgl64.Vec3{0.55, 1, 0.5}, 0.6, 1.8, 0.25)
		for i := 0; i < 40; i++ {
			tick(tx, a, 1)
			tick(tx, b, 1)
		}
		if d := b.Pos()[0] - a.Pos()[0]; d < 0.6 {
			t.Errorf("pushable pair %.3f apart after 40 ticks, want >= 0.6", d)
		}
		if math.Abs(a.Pos()[0]-0.5+(b.Pos()[0]-0.5)) > 0.1 {
			t.Errorf("push not symmetric: %v, %v", a.Pos(), b.Pos())
		}
		a.Close()
		b.Close()
	})
	run(func(tx *world.Tx) {
		floor(tx, 6)
		a := spawn(t, tx, "test:walker", mgl64.Vec3{0.45, 1, 0.5}, 0.6, 1.8, 0.25)
		w := spawn(t, tx, "test:wall", mgl64.Vec3{0.55, 1, 0.5}, 0.6, 1.8, 0.25)
		for i := 0; i < 40; i++ {
			tick(tx, a, 1)
			tick(tx, w, 1)
		}
		if x := w.Pos()[0]; x != 0.55 {
			t.Errorf("unpushable mob moved to x %v", x)
		}
		if d := w.Pos()[0] - a.Pos()[0]; d < 0.6 {
			t.Errorf("unpushable mob did not push the other away: %.3f apart", d)
		}
		a.Close()
		w.Close()
	})
}

// Cobwebs slow mobs (Dragonfly's Cobweb.EntityInside), except those with
// block_movement_slowdown_immunity for minecraft:web.
func TestCobwebImmunity(t *testing.T) {
	run := testWorld(t)
	walkThrough := func(tx *world.Tx, id string) float64 {
		floor(tx, 10)
		for x := 1; x <= 6; x++ {
			for y := 1; y <= 2; y++ {
				for z := -2; z <= 2; z++ {
					tx.SetBlock(cube.Pos{x, y, z}, block.Cobweb{}, nil)
				}
			}
		}
		m := spawn(t, tx, id, mgl64.Vec3{-0.5, 1, 0.5}, 0.6, 0.9, 0.3)
		defer m.Close()
		walk(m, towardsPosX)
		tick(tx, m, 40)
		return m.Pos()[0]
	}
	var slow, immune float64
	run(func(tx *world.Tx) { slow = walkThrough(tx, "test:walker") })
	run(func(tx *world.Tx) { immune = walkThrough(tx, "test:spider") })
	t.Logf("40 ticks through cobwebs: %.2f blocks normally, %.2f immune", slow+0.5, immune+0.5)
	if immune < slow+2 {
		t.Errorf("immune mob got to x %.2f, normal one to %.2f", immune, slow)
	}
}
