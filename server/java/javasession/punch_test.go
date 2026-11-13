package javasession

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/java/protocol/v776"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/java/protocol/version"
	"github.com/df-mc/dragonfly/server/java/protocol/wire"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

type punchCounter struct {
	player.NopHandler
	n *atomic.Int32
}

func (h punchCounter) HandlePunchAir(*player.Context) { h.n.Add(1) }

// punchSession returns a session of client version ver whose survival player holds a block, and a
// count of the punch-air events of the player.
func punchSession(t *testing.T, ver *version.Version) (*Session, *atomic.Int32) {
	t.Helper()
	w := world.Config{Entities: entity.DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	s := mobTestSession()
	s.ver = ver
	s.log = slog.New(slog.DiscardHandler)
	n := new(atomic.Int32)
	_, _ = world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
		pos := mgl64.Vec3{0.5, 10, 0.5}
		h := world.EntitySpawnOpts{Position: pos}.New(player.Type, player.Config{
			Name: "Clicker", UUID: uuid.New(), GameMode: world.GameModeSurvival, Health: 20, MaxHealth: 20, Position: pos,
		})
		p := tx.AddEntity(h).(*player.Player)
		p.Handle(punchCounter{n: n})
		_ = p.Inventory().SetItem(0, item.NewStack(item.Snowball{}, 16))
		s.ent = p.H()
		return struct{}{}, nil
	})
	return s, n
}

// feed handles a packet with the client's own id, as the read loop does.
func feed(t *testing.T, s *Session, id int32, w *wire.Writer) {
	t.Helper()
	var body []byte
	if w != nil {
		body = w.B
	}
	var err error
	if nid := s.ver.ServerboundPlay(id); nid >= 0 {
		err = s.handle(nid, body)
	} else {
		err = s.handleLegacy(id, body)
	}
	if err != nil {
		t.Fatalf("packet %#x: %v", id, err)
	}
}

func useItemOnBody() *wire.Writer {
	w := &wire.Writer{}
	w.VarInt(0) // main hand
	w.Position(0, 8, 0)
	w.VarInt(1) // up
	w.Float32(0.5)
	w.Float32(1)
	w.Float32(0.5)
	w.Bool(false)
	w.Bool(false)
	w.VarInt(1)
	return w
}

func useItemBody() *wire.Writer {
	w := &wire.Writer{}
	w.VarInt(0)
	w.VarInt(2)
	w.Float32(0)
	w.Float32(0)
	return w
}

// TestJavaRightClickPunchIsNoHit: a 26.3 client follows the swing of a right click (use_item_on,
// use_item, interact) with punch, which was handled as a swing at the air: the attack sound and
// the punch-air event, heard and seen as a hit. Only a punch on its own is one.
func TestJavaRightClickPunchIsNoHit(t *testing.T) {
	s, n := punchSession(t, version.Newest)
	feed(t, s, v777.ServerboundPlayPunch, nil)
	if n.Load() != 1 {
		t.Fatalf("a swing at the air: %d punch-air events, want 1", n.Load())
	}
	feed(t, s, v777.ServerboundPlayClientTickEnd, nil)

	for name, body := range map[string]func() *wire.Writer{"use_item_on": useItemOnBody, "use_item": useItemBody} {
		n.Store(0)
		id := v777.ServerboundPlayUseItemOn
		if name == "use_item" {
			id = v777.ServerboundPlayUseItem
		}
		feed(t, s, id, body())
		feed(t, s, v777.ServerboundPlayPunch, nil)
		feed(t, s, v777.ServerboundPlayClientTickEnd, nil)
		if n.Load() != 0 {
			t.Errorf("%s then punch: %d punch-air events, want 0", name, n.Load())
		}
	}

	i := &wire.Writer{}
	i.VarInt(12345) // no such entity
	i.VarInt(0)
	i.LpVec3(0, 0, 0)
	i.Bool(false)
	n.Store(0)
	feed(t, s, v777.ServerboundPlayInteract, i)
	feed(t, s, v777.ServerboundPlayPunch, nil)
	if n.Load() != 0 {
		t.Errorf("interact then punch: %d punch-air events, want 0", n.Load())
	}

	// The next client tick, a left click in the air is one again.
	feed(t, s, v777.ServerboundPlayClientTickEnd, nil)
	feed(t, s, v777.ServerboundPlayPunch, nil)
	if n.Load() != 1 {
		t.Fatalf("a swing at the air after a right click: %d punch-air events, want 1", n.Load())
	}
}

// TestJavaRightClickSwing262: 26.2 has swing instead of punch, for every swing: a right click is
// never a punch at the air there.
func TestJavaRightClickSwing262(t *testing.T) {
	s, n := punchSession(t, version.V776)
	feed(t, s, v776.ServerboundPlayUseItemOn, useItemOnBody())
	sw := &wire.Writer{}
	sw.VarInt(0)
	feed(t, s, v776.ServerboundPlaySwing, sw)
	feed(t, s, v776.ServerboundPlayUseItem, useItemBody())
	feed(t, s, v776.ServerboundPlaySwing, sw)
	feed(t, s, v776.ServerboundPlayClientTickEnd, nil)
	if n.Load() != 0 {
		t.Fatalf("26.2 right clicks: %d punch-air events, want 0", n.Load())
	}
}
