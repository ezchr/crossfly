package session

import (
	"testing"
	"time"

	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// blockFlushConn is a Conn whose Flush blocks, as on a peer that stopped reading.
type blockFlushConn struct {
	Conn
	written chan packet.Packet
	block   chan struct{}
}

func (c *blockFlushConn) WritePacket(pk packet.Packet) error { c.written <- pk; return nil }
func (c *blockFlushConn) Flush() error                       { <-c.block; return nil }

// TestDisconnectDoesNotWaitForFlush checks that Disconnect, often called on the world goroutine, returns
// although the flush blocks.
func TestDisconnectDoesNotWaitForFlush(t *testing.T) {
	c := &blockFlushConn{written: make(chan packet.Packet, 1), block: make(chan struct{})}
	defer close(c.block)
	s := &Session{conn: c}
	done := make(chan struct{})
	go func() {
		s.Disconnect("bye")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Disconnect blocked on a stuck flush")
	}
	if pk, ok := (<-c.written).(*packet.Disconnect); !ok || pk.Message != "bye" {
		t.Fatalf("written %#v, want the Disconnect packet", pk)
	}
	if !s.disconnectSent.Load() {
		t.Fatal("disconnectSent not set")
	}
}
