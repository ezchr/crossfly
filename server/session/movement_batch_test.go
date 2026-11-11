package session

import (
	"testing"

	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func batchSession() *Session {
	return &Session{packets: make(chan packet.Packet, 1024), closeBackground: make(chan struct{})}
}

// drainMoves returns the MoveActorAbsolute packets written so far.
func drainMoves(s *Session) []*packet.MoveActorAbsolute {
	var out []*packet.MoveActorAbsolute
	for {
		select {
		case pk := <-s.packets:
			if m, ok := pk.(*packet.MoveActorAbsolute); ok {
				out = append(out, m)
			}
		default:
			return out
		}
	}
}

func move(id uint64, x float32) *packet.MoveActorAbsolute {
	return &packet.MoveActorAbsolute{EntityRuntimeID: id, Position: mgl32.Vec3{x, 0, 0}}
}

// TestMovesOncePerTick: an entity moved three times in a tick reaches the viewer once, at its latest
// position.
func TestMovesOncePerTick(t *testing.T) {
	s := batchSession()
	s.queueMove(move(5, 0)) // the first move after the entity was shown goes at once
	if n := len(drainMoves(s)); n != 1 {
		t.Fatalf("first move: %d packets, want 1", n)
	}
	for tick := 0; tick < 20; tick++ {
		s.queueMove(move(5, 1))
		s.queueMove(move(5, 1))
		s.queueMove(move(5, float32(tick+2)))
		s.FlushEntityMovement(mgl64.Vec3{})
		got := drainMoves(s)
		if len(got) != 1 || got[0].Position[0] != float32(tick+2) {
			t.Fatalf("tick %d: %d packets %v, want one at the latest position", tick, len(got), got)
		}
	}
	s.FlushEntityMovement(mgl64.Vec3{})
	if n := len(drainMoves(s)); n != 0 {
		t.Fatalf("tick without moves: %d packets", n)
	}
}

// TestMoveDroppedOnRemove: a move kept for an entity removed before the tick is not sent, and its next
// move after being shown again goes at once.
func TestMoveDroppedOnRemove(t *testing.T) {
	s := batchSession()
	s.queueMove(move(7, 0))
	drainMoves(s)
	s.queueMove(move(7, 1))
	s.dropMove(7, true)
	s.FlushEntityMovement(mgl64.Vec3{})
	if n := len(drainMoves(s)); n != 0 {
		t.Fatalf("removed entity: %d moves sent", n)
	}
	s.queueMove(move(7, 2))
	if n := len(drainMoves(s)); n != 1 {
		t.Fatalf("shown again: %d moves at once, want 1", n)
	}
}
