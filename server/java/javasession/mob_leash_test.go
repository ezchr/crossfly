package javasession

import (
	"encoding/hex"
	"testing"

	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// leashedFakeMob is a fake mob with Mob.ActorData (the LEASH_HOLDER entry).
type leashedFakeMob struct {
	*fakeMob
	data map[uint32]any
}

func (m *leashedFakeMob) ActorData(key uint32) (any, bool) { v, ok := m.data[key]; return v, ok }

// TestLeashLink: a leashed mob gets set_entity_link to its holder; a holder the client does not have yet
// gets it when it comes into view, and again after it left and came back; unleashing sends a link to 0.
func TestLeashLink(t *testing.T) {
	s := mobTestSession()
	m := &leashedFakeMob{fakeMob: newFakeMob("minecraft:cow"), data: map[uint32]any{}}
	knot := newFakeMob("minecraft:leash_knot")
	s.entityIDs[m.H()] = 9
	link := func(mob, holder string) string { return mob + holder }

	check := func(what string, want ...string) {
		t.Helper()
		out := s.take()
		if len(out) != len(want) {
			t.Fatalf("%v: %d packets, want %d: %s", what, len(out), len(want), dump(out))
		}
		for i, p := range out {
			if p.id != v777.ClientboundPlaySetEntityLink || hex.EncodeToString(p.w.B) != want[i] {
				t.Fatalf("%v: %s", what, dump(out))
			}
		}
	}
	s.viewLeash(m, 9)
	check("not leashed")

	m.data[protocol.EntityDataKeyLeashHolder] = knot.H()
	s.viewLeash(m, 9)
	check("holder not in view")
	s.entityIDs[knot.H()] = 12
	s.viewLeashHolder(knot, 12)
	check("holder came into view", link("00000009", "0000000c"))
	s.viewLeash(m, 9)
	check("state again")

	s.forgetLeash(knot.H(), 12)
	delete(s.entityIDs, knot.H())
	s.entityIDs[knot.H()] = 14
	s.viewLeashHolder(knot, 14)
	check("holder back", link("00000009", "0000000e"))

	m.data[protocol.EntityDataKeyLeashHolder] = int64(-1)
	s.viewLeash(m, 9)
	check("unleashed", link("00000009", "00000000"))
	s.viewLeash(m, 9)
	check("unleashed again")

	s.ent = newFakeMob("minecraft:player").H()
	m.data[protocol.EntityDataKeyLeashHolder] = s.ent
	s.viewLeash(m, 9)
	check("leashed to the viewer", link("00000009", "00000001"))
}
