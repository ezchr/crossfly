package session

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// A panic while one player's packet is handled must end that player's session only. Before, the world
// recovered the panic of the task but world.CallRef raised it again on the session's goroutine, where
// nothing recovered it, and the whole process exited.

// panickingHandler panics on every packet, standing in for any handler bug a client can trigger.
type panickingHandler struct{}

func (panickingHandler) Handle(packet.Packet, *Session, *world.Tx, Controllable) error {
	panic("handler bug")
}

type panicTestOwnerConfig struct{}

func (panicTestOwnerConfig) Apply(*world.EntityData) {}

type panicTestOwnerType struct{}

func (panicTestOwnerType) Open(tx *world.Tx, h *world.EntityHandle, _ *world.EntityData) world.Entity {
	return panicTestOwner{h: h, tx: tx}
}
func (panicTestOwnerType) EncodeEntity() string                        { return "test:owner" }
func (panicTestOwnerType) BBox(world.Entity) cube.BBox                 { return cube.BBox{} }
func (panicTestOwnerType) DecodeNBT(map[string]any, *world.EntityData) {}
func (panicTestOwnerType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

// panicTestOwner is the session's Controllable. Only what the session's close path calls is
// implemented; it removes itself from the world when closed.
type panicTestOwner struct {
	Controllable
	h  *world.EntityHandle
	tx *world.Tx
}

func (e panicTestOwner) H() *world.EntityHandle { return e.h }
func (panicTestOwner) Position() mgl64.Vec3     { return mgl64.Vec3{} }
func (panicTestOwner) Rotation() cube.Rotation  { return cube.Rotation{} }
func (e panicTestOwner) Close() error {
	if ent, ok := e.h.Entity(e.tx); ok {
		e.tx.RemoveEntity(ent)
	}
	return e.h.Close()
}

// panicTestConn is a Conn that delivers the packets given and records what the server writes.
type panicTestConn struct {
	in     chan packet.Packet
	mu     sync.Mutex
	out    []packet.Packet
	closed chan struct{}
	once   sync.Once
}

func (c *panicTestConn) Close() error { c.once.Do(func() { close(c.closed) }); return nil }
func (c *panicTestConn) IdentityData() login.IdentityData {
	return login.IdentityData{DisplayName: "Tester"}
}
func (c *panicTestConn) ClientData() login.ClientData { return login.ClientData{} }
func (c *panicTestConn) ClientCacheEnabled() bool     { return false }
func (c *panicTestConn) ChunkRadius() int             { return 8 }
func (c *panicTestConn) Latency() time.Duration       { return 0 }
func (c *panicTestConn) Flush() error                 { return nil }
func (c *panicTestConn) RemoteAddr() net.Addr         { return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)} }
func (c *panicTestConn) StartGameContext(context.Context, minecraft.GameData) error {
	return nil
}
func (c *panicTestConn) ReadPacket() (packet.Packet, error) {
	select {
	case pk := <-c.in:
		return pk, nil
	case <-c.closed:
		return nil, io.EOF
	}
}
func (c *panicTestConn) WritePacket(pk packet.Packet) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.out = append(c.out, pk)
	return nil
}

func newPanicTestSession(t *testing.T, log *slog.Logger) (*Session, *panicTestConn, *world.World) {
	t.Helper()
	w := world.Config{Log: slog.New(slog.DiscardHandler)}.New()
	t.Cleanup(func() { _ = w.Close() })
	h := world.NewEntity(panicTestOwnerType{}, panicTestOwnerConfig{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.Do(func(tx *world.Tx) { tx.AddEntity(h) }).Wait(ctx); err != nil {
		t.Fatal(err)
	}
	conn := &panicTestConn{in: make(chan packet.Packet, 4), closed: make(chan struct{})}
	s := &Session{
		ent:             h,
		conn:            conn,
		conf:            Config{Log: log.With("name", "Tester")},
		closeBackground: make(chan struct{}),
		handlers:        map[uint32]packetHandler{packet.IDMobEquipment: panickingHandler{}},
	}
	return s, conn, w
}

func TestWithControllableReturnsPanics(t *testing.T) {
	s, _, w := newPanicTestSession(t, slog.New(slog.DiscardHandler))
	ctx := context.Background()

	// Handled through handlePacketRecovered: recovered on the world owner, with the packet type.
	err := s.withControllable(ctx, func(tx *world.Tx, c Controllable) error {
		return s.handlePacketRecovered(&packet.MobEquipment{}, tx, c)
	})
	p, ok := errors.AsType[*sessionPanic](err)
	if !ok || p.packet != "*packet.MobEquipment" || p.value != "handler bug" || len(p.stack) == 0 {
		t.Fatalf("handlePacketRecovered: got %#v", err)
	}

	// Any other panic on the owner comes back from withControllable as an error, not a panic.
	err = s.withControllable(ctx, func(tx *world.Tx, c Controllable) error {
		return s.handlePacket(&packet.MobEquipment{}, tx, c)
	})
	if p, ok := errors.AsType[*sessionPanic](err); !ok || p.value != "handler bug" {
		t.Fatalf("withControllable: got %#v", err)
	}

	// The world carries on.
	ran := false
	if err := w.Do(func(*world.Tx) { ran = true }).Wait(ctx); err != nil || !ran {
		t.Fatalf("world stopped running tasks: %v", err)
	}
}

func TestPacketPanicDisconnectsOnlyThePlayer(t *testing.T) {
	var logs bytes.Buffer
	var logMu sync.Mutex
	log := slog.New(slog.NewTextHandler(&lockedWriter{w: &logs, mu: &logMu}, nil))
	s, conn, w := newPanicTestSession(t, log)

	conn.in <- &packet.MobEquipment{}
	done := make(chan struct{})
	go func() {
		s.handlePackets()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handlePackets did not return after a panicking packet")
	}

	conn.mu.Lock()
	var disconnect *packet.Disconnect
	for _, pk := range conn.out {
		if d, ok := pk.(*packet.Disconnect); ok {
			disconnect = d
		}
	}
	conn.mu.Unlock()
	if disconnect == nil || disconnect.Message == "" {
		t.Fatalf("player not sent a disconnect message: %#v", conn.out)
	}
	logMu.Lock()
	out := logs.String()
	logMu.Unlock()
	for _, want := range []string{"level=ERROR", "process packet: panic, disconnecting the player", "name=Tester", "packet=*packet.MobEquipment", `panic="handler bug"`} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %q:\n%s", want, out)
		}
	}
	// The player's entity was closed, and the world still runs.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.Do(func(*world.Tx) {}).Wait(ctx); err != nil {
		t.Fatalf("world stopped: %v", err)
	}
	if !s.ent.Closed() {
		t.Error("player entity not closed")
	}
}

type lockedWriter struct {
	w  io.Writer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(b)
}
