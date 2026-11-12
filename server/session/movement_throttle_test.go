package session

import (
	"testing"

	"github.com/go-gl/mathgl/mgl64"
)

// TestMoveDistanceTiers counts the moves a viewer gets in 40 ticks of an entity moving every tick at
// 5, 12, 24 and 100 blocks: every tick, every 2, every 3 and every 4 ticks.
func TestMoveDistanceTiers(t *testing.T) {
	for _, c := range []struct {
		dist float32
		want int
	}{{5, 40}, {12, 20}, {24, 13}, {100, 10}} {
		s := batchSession()
		s.queueMove(move(3, c.dist))
		drainMoves(s)
		n := 0
		for tick := 0; tick < 40; tick++ {
			s.queueMove(move(3, c.dist+float32(tick)*0.01))
			s.FlushEntityMovement(mgl64.Vec3{})
			n += len(drainMoves(s))
		}
		if n != c.want {
			t.Errorf("%v blocks away: %d moves in 40 ticks, want %d", c.dist, n, c.want)
		}
	}
}

// TestMoveStoppedSentWhenDue: a far entity that moves once and stops gets that position on its next due
// tick, not never.
func TestMoveStoppedSentWhenDue(t *testing.T) {
	s := batchSession()
	s.queueMove(move(3, 100))
	drainMoves(s)
	s.FlushEntityMovement(mgl64.Vec3{})
	s.queueMove(move(3, 101))
	var got []float32
	for tick := 0; tick < 8; tick++ {
		s.FlushEntityMovement(mgl64.Vec3{})
		for _, m := range drainMoves(s) {
			got = append(got, m.Position[0])
		}
	}
	if len(got) != 1 || got[0] != 101 {
		t.Fatalf("stopped entity: moves %v, want its last position once", got)
	}
}

// TestMoveTeleportNotHeld: a teleport (dropMove) replaces the move kept and is not delayed.
func TestMoveTeleportNotHeld(t *testing.T) {
	s := batchSession()
	s.queueMove(move(3, 100))
	drainMoves(s)
	s.queueMove(move(3, 101))
	s.dropMove(3, false)
	for tick := 0; tick < 8; tick++ {
		s.FlushEntityMovement(mgl64.Vec3{})
	}
	if n := len(drainMoves(s)); n != 0 {
		t.Fatalf("move older than a teleport sent: %d", n)
	}
}
