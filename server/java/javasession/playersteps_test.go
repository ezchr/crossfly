package javasession

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/go-gl/mathgl/mgl64"
)

// TestMoveTicks: a move spans the ticks since the last one, at least 1 and at most the interval.
func TestMoveTicks(t *testing.T) {
	t0 := time.Unix(100, 0)
	for _, c := range []struct {
		since time.Duration
		want  int
	}{
		{0, 1}, {10 * time.Millisecond, 1}, {50 * time.Millisecond, 1}, {70 * time.Millisecond, 1},
		{80 * time.Millisecond, 2}, {100 * time.Millisecond, 2}, {time.Second, 2},
	} {
		if got := moveTicks(t0, t0.Add(c.since), 2); got != c.want {
			t.Errorf("after %v: %d ticks, want %d", c.since, got, c.want)
		}
	}
	if got := moveTicks(time.Time{}, t0, 2); got != 2 {
		t.Errorf("first move: %d ticks, want 2", got)
	}
}

// TestPlayerMovesEveryTickSpanOneTick: moves of an entity shown as a player that come every tick
// carry a one-tick step, so the 26.3 client plays them in real time instead of over 2 ticks each
// (which made its queue of steps grow and others lag behind); the first move spans the interval.
func TestPlayerMovesEveryTickSpanOneTick(t *testing.T) {
	s := mobTestSession()
	if !s.ver.Native() {
		t.Skip("the test session is not 26.3")
	}
	now := time.Unix(100, 0)
	old := moveClock
	moveClock = func() time.Time { return now }
	defer func() { moveClock = old }()

	const id = 77
	pos := mgl64.Vec3{0, 64, 0}
	s.setTrack(id, pos, cube.Rotation{})
	s.take()
	step := func() byte {
		t.Helper()
		pos = pos.Add(mgl64.Vec3{0.25, 0, 0})
		s.syncEntitySteps(id, pos, cube.Rotation{}, 0, true, -2)
		p := findPacket(s.take(), v777.ClientboundPlayMoveEntityPos)
		if p == nil || len(p) < 3 || p[1] != 1|1<<1 {
			t.Fatalf("not a stepped move: % x", p)
		}
		return p[2]
	}
	if n := step(); n != 2 {
		t.Fatalf("first move spans %d ticks, want 2", n)
	}
	for i := 0; i < 5; i++ {
		now = now.Add(50 * time.Millisecond)
		if n := step(); n != 1 {
			t.Fatalf("move %d a tick later spans %d ticks, want 1", i, n)
		}
	}
	now = now.Add(2 * time.Second) // stood still
	if n := step(); n != 2 {
		t.Fatalf("move after a pause spans %d ticks, want 2", n)
	}
}
