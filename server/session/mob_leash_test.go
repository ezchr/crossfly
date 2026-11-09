package session

import (
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// leashedFakeMob is a fake mob with ExtraActorData (the LEASH_HOLDER entry, as server/mob sets it).
type leashedFakeMob struct {
	*fakeMob
	data map[uint32]any
}

func (m *leashedFakeMob) ExtraActorData() map[uint32]any { return m.data }

// TestLeashHolderView: a leashed mob shown before its holder (a fence knot) gets the holder's runtime ID in a
// SetActorData once the holder is shown, and again when the holder is shown anew; once unleashed it does not.
func TestLeashHolderView(t *testing.T) {
	s := newTestSession()
	knot := newFakeMob(t)
	m := &leashedFakeMob{fakeMob: newFakeMob(t), data: map[uint32]any{protocol.EntityDataKeyLeashHolder: knot.H()}}

	s.ViewEntity(m)
	add := only[*packet.AddActor](t, s)
	if v := add.EntityMetadata[protocol.EntityDataKeyLeashHolder]; v != int64(0) {
		t.Fatalf("holder not shown yet: data %v", v)
	}
	holderData := func(what string) int64 {
		t.Helper()
		for _, pk := range written(s) {
			if sd, ok := pk.(*packet.SetActorData); ok && sd.EntityRuntimeID == add.EntityRuntimeID {
				return sd.EntityMetadata[protocol.EntityDataKeyLeashHolder].(int64)
			}
		}
		return -2
	}
	s.ViewEntity(knot)
	kid := int64(s.entityRuntimeID(knot))
	if v := holderData("knot shown"); v != kid {
		t.Fatalf("holder data %v after the knot was shown, want %v", v, kid)
	}
	s.HideEntity(knot)
	s.ViewEntity(knot)
	kid = int64(s.entityRuntimeID(knot))
	if v := holderData("knot shown again"); v != kid {
		t.Fatalf("holder data %v after the knot was shown again, want %v", v, kid)
	}
	m.data[protocol.EntityDataKeyLeashHolder] = int64(-1)
	s.ViewEntityState(m)
	written(s)
	s.HideEntity(knot)
	s.ViewEntity(knot)
	if v := holderData("unleashed"); v != -2 {
		t.Fatalf("holder data sent for an unleashed mob: %v", v)
	}
}
