//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// harnessed spawns an adult happy ghast high over a floor and harnesses it, then lets the player get on.
func harnessed(t *testing.T) (*mobtest.World, *mobtest.Ref, *mobtest.Player) {
	t.Helper()
	w, g, p := adultGhast(t, mgl64.Vec3{0.5, 6, 0.5})
	p.SetGameMode(world.GameModeCreative)
	if _, ok := p.Interact(g, item.NewStack(item.Harness{Colour: item.ColourWhite()}, 1)); !ok {
		t.Fatal("happy ghast did not take the harness")
	}
	p.SetGameMode(world.GameModeSurvival)
	if _, ok := p.Interact(g, item.Stack{}); !ok {
		t.Fatal("player did not get on the harnessed happy ghast")
	}
	p.With(func(pl *player.Player) {
		if pl.RidingEntityHandle() != g.H() {
			t.Fatal("player does not ride the happy ghast")
		}
	})
	return w, g, p
}

// steer sends the rider's input for n ticks: move (strafe, forward) looking at yaw/pitch, jumping if jump.
func steer(w *mobtest.World, g *mobtest.Ref, p *mobtest.Player, move mgl64.Vec2, yaw, pitch float64, jump bool, n int) {
	for i := 0; i < n; i++ {
		p.With(func(pl *player.Player) {
			r := pl.Rotation()
			pl.Move(mgl64.Vec3{}, yaw-r.Yaw(), pitch-r.Pitch())
			m, _ := g.Mob(pl.Tx())
			rideable, _ := entity.RideableOf(m)
			rideable.MoveInput(move, float32(yaw), float32(pitch))
			if jump {
				if j, ok := rideable.(entity.VehicleJumper); ok {
					j.RiderJump(pl, 1)
				}
			}
		})
		w.Tick(1)
	}
}

// TestHappyGhastRideForward: the rider flies the happy ghast where it looks, at the flying speed of
// adult_mobile (0.016 x 0.98 / (1 - 0.91): about 0.17 blocks a tick, as Java's 3.9 x 0.05 ridden input), and
// up when looking up.
func TestHappyGhastRideForward(t *testing.T) {
	w, g, p := harnessed(t)
	start := g.Pos()
	// Yaw 0 looks south (+z); no pitch.
	steer(w, g, p, mgl64.Vec2{0, 1}, 0, 0, false, 60)
	mid := g.Pos()
	if dz := mid[2] - start[2]; dz < 5 {
		t.Fatalf("happy ghast flew %.2f blocks south in 60 ticks:\n%v", dz, w.Dump(g))
	}
	if dy := mid[1] - start[1]; dy > 0.5 || dy < -0.5 {
		t.Errorf("level flight changed height by %.2f", dy)
	}
	var speed float64
	g.With(func(m *mob.Mob) { speed = m.Vel().Len() })
	if speed < 0.15 || speed > 0.19 {
		t.Errorf("ridden speed %.3f blocks/tick, want about 0.17", speed)
	}
	// Looking 45 degrees up climbs.
	steer(w, g, p, mgl64.Vec2{0, 1}, 0, -45, false, 40)
	if dy := g.Pos()[1] - mid[1]; dy < 2 {
		t.Errorf("looking up climbed %.2f blocks in 40 ticks", dy)
	}
}

// TestHappyGhastRideJumpRises: holding jump lifts the ridden happy ghast (vertical_movement_action).
func TestHappyGhastRideJumpRises(t *testing.T) {
	w, g, p := harnessed(t)
	start := g.Pos()
	steer(w, g, p, mgl64.Vec2{}, 0, 0, true, 20)
	if dy := g.Pos()[1] - start[1]; dy < 2 {
		t.Errorf("jumping lifted the happy ghast %.2f blocks in 20 ticks:\n%v", dy, w.Dump(g))
	}
}

// TestHappyGhastFourRiders: up to four players ride a harnessed happy ghast (seat_count 4); the first
// steers; a fifth cannot get on; a rider that dismounts lands on top of it (dismount_mode on_top_center).
func TestHappyGhastFourRiders(t *testing.T) {
	w, g, first := harnessed(t)
	riders := []*mobtest.Player{first}
	for i := 0; i < 3; i++ {
		p := w.AddPlayer("rider"+string(rune('b'+i)), mgl64.Vec3{3.5, 1, 0.5 + float64(i)})
		if _, ok := p.Interact(g, item.Stack{}); !ok {
			t.Fatalf("rider %d did not get on", i+2)
		}
		riders = append(riders, p)
	}
	fifth := w.AddPlayer("fifth", mgl64.Vec3{3.5, 1, 4.5})
	if _, ok := fifth.Interact(g, item.Stack{}); ok {
		t.Error("a fifth player got on")
	}
	g.With(func(m *mob.Mob) {
		if n := m.RiderCount(); n != 4 {
			t.Errorf("%d riders, want 4", n)
		}
		if c, ok := m.ControllingPassenger(); !ok || c.H() != first.H() {
			t.Error("the first rider does not steer")
		}
		seats := m.SeatPositions()
		if len(seats) != 4 || seats[3] == seats[0] {
			t.Errorf("seats %v", seats)
		}
	})
	w.Tick(2)
	riders[1].With(func(pl *player.Player) {
		pl.DismountEntity(pl.Tx())
		m, _ := g.Mob(pl.Tx())
		if top := m.Pos()[1] + m.BBox().Height(); pl.Position()[1] < top-0.01 {
			t.Errorf("dismounted rider at y %.2f, below the top of the happy ghast %.2f", pl.Position()[1], top)
		}
	})
}
