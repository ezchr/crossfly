package server

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
)

// syncBuffer is a bytes.Buffer safe for a logger and a test to share.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// A panic while a connection logs in (here in the Status callback) drops that connection only:
// it is logged with the packet, the pending slot is given back and the listener keeps accepting.
func TestLoginPanicDropsOnlyTheConnection(t *testing.T) {
	var logs syncBuffer
	l := listen(t, Config{
		CompressionThreshold: -1,
		MaxPendingPerIP:      1,
		Log:                  slog.New(slog.NewTextHandler(&logs, nil)),
		Status:               func() Status { panic("status callback failed") },
	})
	for i := range 3 {
		c := dial(t, l)
		handshake(c, 1)
		c.WritePacket(v777.ServerboundStatusStatusRequest, nil)
		c.Flush()
		if !closedSoon(c, 5*time.Second) {
			t.Fatalf("connection %d not closed", i)
		}
	}
	out := logs.String()
	if !strings.Contains(out, "panic during java login") || !strings.Contains(out, "status callback failed") ||
		!strings.Contains(out, "packet=0") {
		t.Errorf("log: %s", out)
	}
}
