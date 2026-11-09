package server

import (
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

// ghostSession is an external session whose client is gone without the server knowing: it never
// closes by itself, only when the server disconnects it.
type ghostSession struct {
	*session.Session // session.Nop for everything a player sends
	mu               sync.Mutex
	onClose          func(*world.Tx, session.Controllable)
	handle           *world.EntityHandle
	disconnected     string
	closed           bool
}

func (g *ghostSession) Spawn(session.Controllable, *world.Tx) {}
func (g *ghostSession) SetCloseHandler(f func(*world.Tx, session.Controllable)) {
	g.onClose = f
}
func (g *ghostSession) SetHandle(h *world.EntityHandle, _ skin.Skin) { g.handle = h }
func (g *ghostSession) Disconnect(msg string) {
	g.mu.Lock()
	g.disconnected = msg
	g.mu.Unlock()
}
func (g *ghostSession) CloseConnection() {}
func (g *ghostSession) Close(tx *world.Tx, c session.Controllable) {
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return
	}
	g.closed = true
	g.mu.Unlock()
	g.onClose(tx, c)
	if tx != nil {
		tx.RemoveEntity(c)
		_ = g.handle.Close()
	}
}

// idleListener accepts nobody until it is closed: it keeps the server open for external joins.
type idleListener struct {
	once   sync.Once
	closed chan struct{}
}

func (l *idleListener) Accept() (session.Conn, error) {
	<-l.closed
	return nil, net.ErrClosed
}
func (l *idleListener) Disconnect(session.Conn, string) error { return nil }
func (l *idleListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func takeOverServer(t *testing.T) *Server {
	t.Helper()
	uc := DefaultConfig()
	uc.World.Folder = t.TempDir()
	uc.Players.Folder = t.TempDir()
	conf, err := uc.Config(slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	idle := &idleListener{closed: make(chan struct{})}
	conf.Listeners = []func(Config) (Listener, error){func(Config) (Listener, error) { return idle, nil }}
	srv := conf.New()
	srv.Listen()
	go func() {
		for range srv.Accept() {
		}
	}()
	t.Cleanup(func() { _ = srv.Close() })
	return srv
}

func joinGhost(t *testing.T, srv *Server, id uuid.UUID) *ghostSession {
	t.Helper()
	pc, w := srv.LoadPlayer(id)
	pc.Name, pc.UUID = "Ghost", id
	g := &ghostSession{Session: session.Nop}
	if err := srv.AddPlayer(g, pc, w); err != nil {
		t.Fatalf("join: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		h, ok := srv.Player(id)
		if ok {
			if _, err := player.Call(t.Context(), h, func(*world.Tx, *player.Player) (struct{}, error) { return struct{}{}, nil }); err == nil {
				return g
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("player never spawned")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A new login of a player whose old session is still online (its connection died unnoticed)
// replaces that session instead of being refused with "Already logged in".
func TestTakeOverReplacesOnlineSession(t *testing.T) {
	srv := takeOverServer(t)
	id := uuid.New()
	old := joinGhost(t, srv, id)

	if err := srv.TakeOver(id); err != nil {
		t.Fatalf("TakeOver: %v", err)
	}
	if _, ok := srv.Player(id); ok {
		t.Fatal("old session still online after TakeOver")
	}
	old.mu.Lock()
	msg, closed := old.disconnected, old.closed
	old.mu.Unlock()
	if msg != DuplicateLoginMessage || !closed {
		t.Fatalf("old session: disconnected with %q, closed %v", msg, closed)
	}
	// The player can join again right away.
	joinGhost(t, srv, id)
	if n := srv.PlayerCount(); n != 1 {
		t.Fatalf("%d players online, want 1", n)
	}
}

// TakeOver returns at once for a player that is not online.
func TestTakeOverOffline(t *testing.T) {
	srv := takeOverServer(t)
	start := time.Now()
	if err := srv.TakeOver(uuid.New()); err != nil {
		t.Fatalf("TakeOver: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("TakeOver waited for a player that is not online")
	}
}
