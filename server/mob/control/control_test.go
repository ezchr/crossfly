//go:build crossfly_full

package control

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func moveControl(t *testing.T, m *mob.Mob) *MoveControl {
	t.Helper()
	c, ok := m.MoveControl().(*MoveControl)
	if !ok {
		fail(t, "move control is %T", m.MoveControl())
	}
	return c
}

// The move control ramps the speed up by 10% a tick and then walks at the
// speed of the movement attribute: 0.98 s^2 / (1 - 0.546) blocks a tick on
// stone.
func TestWalkSpeed(t *testing.T) {
	run := testWorld(t)
	for _, speed := range []float64{0.2, 0.25, 0.3} {
		run(func(tx *world.Tx) {
			floor(tx, 40)
			m := spawn(t, tx, "test:cow", mgl64.Vec3{-30.5, 1, 0.5}, 0.9, 1.3, speed)
			c := moveControl(t, m)
			c.SetWantedPosition(mgl64.Vec3{39.5, 1, 0.5}, 1)
			tick(tx, m, 1)
			if got := c.CurrentSpeed(); math.Abs(got-0.1*speed) > 1e-9 {
				t.Errorf("speed %v: first tick at %v, want %v", speed, got, 0.1*speed)
			}
			tick(tx, m, 9)
			if got := c.CurrentSpeed(); math.Abs(got-speed) > 1e-9 {
				t.Errorf("speed %v: after 10 ticks at %v, want full speed", speed, got)
			}
			tick(tx, m, 30)
			x0 := m.Pos()[0]
			tick(tx, m, 20)
			got := (m.Pos()[0] - x0) / 20
			want := 0.98 * speed * speed / (1 - 0.546)
			t.Logf("movement %.2f: %.4f blocks/tick", speed, got)
			if math.Abs(got-want) > want*0.01 {
				t.Errorf("movement %v: %.4f blocks a tick, want %.4f", speed, got, want)
			}
			// The first ticks turn the mob from yaw 0 while it starts to
			// walk, so it aims at the target from slightly off the line.
			if math.Abs(m.Pos()[2]-0.5) > 0.01 {
				t.Errorf("movement %v: drifted sideways to z %v", speed, m.Pos()[2])
			}
			m.Close()
		})
	}
}

// The facing turns at most max_turn (30) a tick at full speed, the body at
// most 25 a tick while moving, and the head stays within 75 of the body.
func TestTurnRates(t *testing.T) {
	run := testWorld(t)
	run(func(tx *world.Tx) {
		floor(tx, 40)
		m := spawn(t, tx, "test:cow", mgl64.Vec3{0.5, 1, 0.5}, 0.9, 1.3, 0.25)
		c := moveControl(t, m)
		c.SetWantedPosition(mgl64.Vec3{39.5, 1, 0.5}, 1)
		tick(tx, m, 30)
		if y := WrapDegrees(m.Yaw()); math.Abs(y-(-90)) > 0.01 {
			fail(t, "walking +X with yaw %v, want -90", y)
		}
		c.SetWantedPosition(m.Pos().Add(mgl64.Vec3{-40, 0, 0.01}), 1)
		turned := -1
		for i := 1; i <= 20; i++ {
			yaw, body := m.Yaw(), m.BodyYaw()
			tick(tx, m, 1)
			if d := math.Abs(WrapDegrees(m.Yaw() - yaw)); d > 30+1e-9 {
				t.Errorf("tick %d: facing turned %v", i, d)
			}
			if d := math.Abs(WrapDegrees(m.BodyYaw() - body)); d > 25+1e-9 {
				t.Errorf("tick %d: body turned %v", i, d)
			}
			if d := math.Abs(WrapDegrees(m.HeadYaw() - m.BodyYaw())); d > HeadRange+1e-9 {
				t.Errorf("tick %d: head %v from the body", i, d)
			}
			if turned < 0 && math.Abs(WrapDegrees(m.Yaw()-90)) < 0.1 {
				turned = i
			}
		}
		if turned != 6 {
			t.Errorf("turning around took %d ticks, want 6 (180 / 30)", turned)
		}
		m.Close()
	})
}

// A mob with jump.static jumps onto a full block in its way; one without a
// jump control cannot, but still walks up slabs.
func TestJumpAndStep(t *testing.T) {
	run := testWorld(t)
	stepUp := func(tx *world.Tx, b world.Block) {
		floor(tx, 12)
		for x := 3; x <= 12; x++ {
			for z := -3; z <= 3; z++ {
				tx.SetBlock(cube.Pos{x, 1, z}, b, nil)
			}
		}
	}
	walkTo := func(tx *world.Tx, id string, top float64) mgl64.Vec3 {
		m := spawn(t, tx, id, mgl64.Vec3{0.5, 1, 0.5}, 0.9, 1.3, 0.25)
		defer m.Close()
		// Like navigation, aim at the first block of the step until on it:
		// the jump is only asked for when the higher target is close.
		for i := 0; i < 100; i++ {
			target := mgl64.Vec3{3.5, top, 0.5}
			if m.Pos()[0] > 3.2 {
				target = mgl64.Vec3{8.5, top, 0.5}
			}
			moveControl(t, m).SetWantedPosition(target, 1)
			tick(tx, m, 1)
		}
		return m.Pos()
	}
	run(func(tx *world.Tx) {
		stepUp(tx, block.Stone{})
		if p := walkTo(tx, "test:cow", 2); math.Abs(p[1]-2) > 1e-9 || math.Abs(p[0]-8.5) > 0.6 {
			t.Errorf("jump.static: ended at %v, want on the block near x 8.5", p)
		}
	})
	run(func(tx *world.Tx) {
		stepUp(tx, block.Stone{})
		if p := walkTo(tx, "test:legless", 2); p[1] != 1 {
			t.Errorf("no jump control: got up to y %v", p[1])
		}
	})
	run(func(tx *world.Tx) {
		stepUp(tx, block.Slab{Block: block.Stone{}})
		if p := walkTo(tx, "test:legless", 1.5); math.Abs(p[1]-1.5) > 1e-9 || math.Abs(p[0]-8.5) > 0.6 {
			t.Errorf("slab without jumping: ended at %v, want on the slabs near x 8.5", p)
		}
	})
}

// The look control turns the head at most yawMax a tick and never past 75
// degrees from the body; a standing body follows a head that stays still
// (after 10 ticks, closing in over the next 10), so the head ends up on the
// target. Without a target the head returns to the body at 10 a tick.
func TestLookControl(t *testing.T) {
	run := testWorld(t)
	run(func(tx *world.Tx) {
		floor(tx, 10)
		m := spawn(t, tx, "test:cow", mgl64.Vec3{0.5, 1, 0.5}, 0.9, 1.3, 0.25)
		tick(tx, m, 2)
		// Yaw 0 faces +Z; a point at +X is at yaw -90, above the eyes.
		target := m.EyePosition().Add(mgl64.Vec3{5, 2, 0})
		for i := 1; i <= 80; i++ {
			head := m.HeadYaw()
			m.LookControl().SetLookAt(target, 10, 40)
			tick(tx, m, 1)
			if d := math.Abs(WrapDegrees(m.HeadYaw() - head)); d > 10+1e-9 {
				t.Errorf("tick %d: head turned %v", i, d)
			}
			if d := math.Abs(WrapDegrees(m.HeadYaw() - m.BodyYaw())); d > HeadRange+1e-9 {
				t.Errorf("tick %d: head %v from the body", i, d)
			}
		}
		if d := WrapDegrees(m.HeadYaw() + 90); math.Abs(d) > 1e-6 {
			t.Errorf("head at %v, want -90", m.HeadYaw())
		}
		if d := WrapDegrees(m.BodyYaw() - m.HeadYaw()); math.Abs(d) > 1e-6 {
			t.Errorf("body at %v did not follow the head at %v", m.BodyYaw(), m.HeadYaw())
		}
		wantPitch := -mgl64.RadToDeg(math.Atan2(2, 5))
		if math.Abs(m.Pitch()-wantPitch) > 1e-6 {
			t.Errorf("pitch %v, want %v", m.Pitch(), wantPitch)
		}
		// Look away from the body, then let go: the head comes back at 10 a
		// tick.
		m.SetBodyYaw(0)
		m.SetHeadYaw(60)
		for i := 0; i < 6; i++ {
			before := m.HeadYaw()
			tick(tx, m, 1)
			if d := before - m.HeadYaw(); math.Abs(d-10) > 1e-9 && m.HeadYaw() != m.BodyYaw() {
				t.Errorf("idle head moved %v, want 10", d)
			}
		}
		if math.Abs(m.Pitch()) > 1e-9 {
			t.Errorf("idle pitch %v, want back at 0", m.Pitch())
		}
		m.Close()
	})
}

// A strafing mob moves sideways and backwards at a quarter of its speed.
func TestStrafe(t *testing.T) {
	run := testWorld(t)
	run(func(tx *world.Tx) {
		floor(tx, 20)
		m := spawn(t, tx, "test:cow", mgl64.Vec3{0.5, 1, 0.5}, 0.9, 1.3, 0.25)
		tick(tx, m, 2)
		start := m.Pos()
		for i := 0; i < 20; i++ {
			moveControl(t, m).Strafe(-0.5, 0.5)
			tick(tx, m, 1)
		}
		d := m.Pos().Sub(start)
		// Yaw 0: forward is +Z, strafe positive is +X.
		if d[2] >= -0.1 || d[0] <= 0.1 {
			t.Errorf("strafing back and to the side moved %v", d)
		}
		if m.Yaw() != 0 {
			t.Errorf("strafing turned the mob to %v", m.Yaw())
		}
		m.Close()
	})
}

// movement.fly flies to the wanted position without gravity.
func TestFly(t *testing.T) {
	run := testWorld(t)
	run(func(tx *world.Tx) {
		floor(tx, 20)
		m := spawn(t, tx, "test:parrot", mgl64.Vec3{0.5, 1, 0.5}, 0.5, 0.9, 0.2)
		m.Attributes().SetBase(mob.AttrFlyingSpeed, 0.4)
		target := mgl64.Vec3{10.5, 8, 0.5}
		for i := 0; i < 200; i++ {
			moveControl(t, m).SetWantedPosition(target, 1)
			tick(tx, m, 1)
		}
		if d := m.Pos().Sub(target).Len(); d > 1 {
			t.Errorf("flying mob at %v, %.2f from %v", m.Pos(), d, target)
		}
		m.Close()
	})
}

// movement.generic swims towards a target under water.
func TestSwim(t *testing.T) {
	run := testWorld(t)
	run(func(tx *world.Tx) {
		floor(tx, 20)
		for x := -20; x <= 20; x++ {
			for z := -3; z <= 3; z++ {
				for y := 1; y <= 8; y++ {
					tx.SetBlock(cube.Pos{x, y, z}, block.Water{Depth: 8, Still: true}, nil)
				}
			}
		}
		m := spawn(t, tx, "test:drowned", mgl64.Vec3{0.5, 6, 0.5}, 0.6, 1.9, 0.23)
		m.Attributes().SetBase(mob.AttrUnderwaterMovement, 0.06)
		start := m.Pos()
		target := mgl64.Vec3{12.5, 2, 0.5}
		for i := 0; i < 200; i++ {
			moveControl(t, m).SetWantedPosition(target, 1)
			tick(tx, m, 1)
		}
		if p := m.Pos(); p.Sub(target).Len() > start.Sub(target).Len()/2 {
			t.Errorf("swimmer at %v, not much closer to %v from %v", p, target, start)
		}
		m.Close()
	})
}
