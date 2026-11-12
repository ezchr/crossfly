package javasession

import (
	"bytes"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/java/protocol/wire"
)

// lockedBuffer is a bytes.Buffer a logger and a test can share.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// A panic on readLoop's reader goroutine (here: no protocol version to look the packet up in)
// ends this client's session, logged with the player and the packet, instead of the process.
func TestReaderPanicDropsOnlyTheClient(t *testing.T) {
	var logs lockedBuffer
	client, srv := net.Pipe()
	defer client.Close()
	s := itemsTestSession()
	s.log = slog.New(slog.NewTextHandler(&logs, nil)).With("player", "Tester")
	s.conn = wire.NewConn(srv)
	s.ver = nil // ServerboundPlay panics on it, in the reader goroutine
	ended := make(chan struct{})
	go func() {
		s.readLoop()
		close(ended)
	}()
	go client.Write([]byte{2, 0x2a, 0}) // packet 0x2a, one byte of body
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("readLoop did not end")
	}
	out := logs.String()
	if !strings.Contains(out, "panic reading packet") || !strings.Contains(out, "player=Tester") || !strings.Contains(out, "id=42") {
		t.Errorf("log: %s", out)
	}
	select {
	case <-s.closed:
	default:
		t.Error("connection not closed")
	}
}
