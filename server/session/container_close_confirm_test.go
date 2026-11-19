package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// closeUser is a Controllable whose temporary slots are empty.
type closeUser struct{ Controllable }

func (closeUser) MoveItemsToInventory() {}

// closesWritten counts the ContainerClose packets in pks and returns the last one.
func closesWritten(pks []packet.Packet) (n int, last *packet.ContainerClose) {
	for _, pk := range pks {
		if c, ok := pk.(*packet.ContainerClose); ok {
			n, last = n+1, c
		}
	}
	return
}

// The client's confirmation of a close the server made gets no reply; a close the client asks for
// gets its reply, as before.
func TestContainerCloseConfirmNoReply(t *testing.T) {
	anvilSessionTest(t, func(tx *world.Tx, s *Session, _ *ItemStackRequestHandler) {
		h := &ContainerCloseHandler{}
		u := closeUser{}
		s.openedWindowID.Store(5)
		written(s)

		// The server closes window 5 (as a menu library does), then the client confirms it.
		s.writePacket(&packet.ContainerClose{WindowID: 5, ServerSide: true})
		written(s)
		if err := h.Handle(&packet.ContainerClose{WindowID: 5}, s, tx, u); err != nil {
			t.Fatal(err)
		}
		if n, _ := closesWritten(written(s)); n != 0 {
			t.Fatalf("%d ContainerClose replies to the confirmation of a server close, want 0", n)
		}

		// The client closes window 5 itself: it gets its reply.
		s.openedWindowID.Store(5)
		s.containerOpened.Store(true)
		if err := h.Handle(&packet.ContainerClose{WindowID: 5}, s, tx, u); err != nil {
			t.Fatal(err)
		}
		n, last := closesWritten(written(s))
		if n != 1 || last.WindowID != 5 || last.ServerSide {
			t.Fatalf("client close answered with %d packets, last %+v", n, last)
		}

		// A server close the client never confirmed does not swallow the reply to a later client
		// close once a new window opened.
		s.writePacket(&packet.ContainerClose{WindowID: 0, ServerSide: true})
		s.writePacket(&packet.ContainerOpen{WindowID: 0})
		written(s)
		if err := h.Handle(&packet.ContainerClose{WindowID: 0}, s, tx, u); err != nil {
			t.Fatal(err)
		}
		if n, _ := closesWritten(written(s)); n != 1 {
			t.Fatalf("client inventory close after a new open answered with %d packets, want 1", n)
		}
	})
}
