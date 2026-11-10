package session

import (
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

type stallConn struct {
	block  chan struct{}
	closed atomic.Bool
}

func (c *stallConn) Flush() error { <-c.block; return nil }
func (c *stallConn) Close() error {
	if c.closed.CompareAndSwap(false, true) {
		close(c.block)
	}
	return nil
}

type okConn struct{ flushes, closes atomic.Int32 }

func (c *okConn) Flush() error { c.flushes.Add(1); return nil }
func (c *okConn) Close() error { c.closes.Add(1); return nil }

func TestWatchStallClosesStalledConn(t *testing.T) {
	s := &Session{closeBackground: make(chan struct{})}
	s.conf.Log = slog.Default()
	c := &stallConn{block: make(chan struct{})}
	done := make(chan struct{})
	go func() { s.watchStallEvery(c, 10*time.Millisecond, 50*time.Millisecond); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watchStall did not give up on a stalled connection")
	}
	if !c.closed.Load() {
		t.Fatal("stalled connection was not closed")
	}
}

func TestWatchStallLeavesHealthyConn(t *testing.T) {
	s := &Session{closeBackground: make(chan struct{})}
	s.conf.Log = slog.Default()
	c := &okConn{}
	done := make(chan struct{})
	go func() { s.watchStallEvery(c, 5*time.Millisecond, 50*time.Millisecond); close(done) }()
	time.Sleep(100 * time.Millisecond)
	close(s.closeBackground)
	<-done
	if c.closes.Load() != 0 || c.flushes.Load() == 0 {
		t.Fatalf("healthy connection: closes=%d flushes=%d", c.closes.Load(), c.flushes.Load())
	}
}
