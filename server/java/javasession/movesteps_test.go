package javasession

import (
	"bytes"
	"testing"

	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/go-gl/mathgl/mgl64"
)

// TestMoveOverTicks: a move made over a number of ticks carries a one-step path of those ticks
// on 26.3 (as ServerEntity sends it: props bit 0 on ground, the step count from bit 1, then the
// step's ticks before its delta); a plain move stays a linear delta.
func TestMoveOverTicks(t *testing.T) {
	s := mobTestSession()
	if !s.ver.Native() {
		t.Skip("the test session is not 26.3")
	}
	m := newFakeMob("minecraft:cow")
	s.viewOtherEntity(m)
	s.take()
	id, _ := s.entityID(m)

	s.ViewEntityMovementOver(m, m.pos.Add(mgl64.Vec3{0.5, 0, 0}), m.rot, true, 2)
	want := []byte{byte(id), 1 | 1<<1, 2, 0x08, 0x00, 0, 0, 0, 0}
	if p := findPacket(s.take(), v777.ClientboundPlayMoveEntityPos); p == nil || !bytes.Equal(p, want) {
		t.Fatalf("stepped move: % x, want % x", p, want)
	}
	s.ViewEntityMovement(m, m.pos.Add(mgl64.Vec3{1, 0, 0}), m.rot, true)
	want = []byte{byte(id), 1, 0x08, 0x00, 0, 0, 0, 0}
	if p := findPacket(s.take(), v777.ClientboundPlayMoveEntityPos); p == nil || !bytes.Equal(p, want) {
		t.Fatalf("linear move: % x, want % x", p, want)
	}
}

func findPacket(out []outPacket, id int32) []byte {
	for _, p := range out {
		if p.id == id {
			return p.w.B
		}
	}
	return nil
}
