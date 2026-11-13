package javasession

import (
	"context"

	"bytes"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/item"
	jitem "github.com/df-mc/dragonfly/server/java/protocol/item"
	"github.com/df-mc/dragonfly/server/java/protocol/wire"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// TestJavaCrossbowChargeAndShoot: a Java client charges a crossbow with use_item, lets go of the
// key (RELEASE_USE_ITEM) after the charge time and shoots with the next use_item. The release used
// to go to ReleaseItem, which does nothing for a crossbow, so it was never loaded and every
// use_item started a new charge.
func TestJavaCrossbowChargeAndShoot(t *testing.T) {
	crossbowWorld(t, func(tx *world.Tx, s *Session, p *player.Player) {
		_ = p.Inventory().SetItem(0, item.NewStack(item.Crossbow{}, 1))
		_, _ = p.Inventory().AddItem(item.NewStack(item.Arrow{}, 8))
		p.SetHeldSlot(0)

		p.UseItem() // use_item: starts charging
		if !p.UsingItem() {
			t.Fatal("use_item with arrows did not start charging")
		}
		time.Sleep(1300 * time.Millisecond)
		s.releaseUseItem(p) // RELEASE_USE_ITEM
		held, _ := p.HeldItems()
		cb, _ := held.Item().(item.Crossbow)
		if cb.Item.Empty() {
			t.Fatal("crossbow not loaded after RELEASE_USE_ITEM")
		}
		if p.UsingItem() {
			t.Fatal("still using the crossbow after the release")
		}
		if n := countArrows(p); n != 7 {
			t.Fatalf("arrows after loading: %d, want 7", n)
		}

		// The loaded crossbow goes to the client with charged_projectiles holding one arrow.
		var js jitem.Stack
		javaStack(held, &js)
		if !js.Has(jitem.CompChargedProjectiles) {
			t.Fatal("loaded crossbow sent without charged_projectiles")
		}
		var arrow jitem.Stack
		javaStack(item.NewStack(item.Arrow{}, 1), &arrow)
		var want wire.Writer
		want.VarInt(1)
		arrow.Encode(&want)
		for _, r := range js.Raw {
			if r.Type == jitem.CompChargedProjectiles && !bytes.Equal(r.Data, want.B) {
				t.Fatalf("charged_projectiles % x, want % x", r.Data, want.B)
			}
		}

		p.UseItem() // second use_item: shoots
		held, _ = p.HeldItems()
		if cb, _ := held.Item().(item.Crossbow); !cb.Item.Empty() {
			t.Fatal("loaded crossbow did not shoot on use_item")
		}
		if p.UsingItem() {
			t.Fatal("shooting started a new charge")
		}
		javaStack(held, &js)
		if js.Has(jitem.CompChargedProjectiles) {
			t.Fatal("empty crossbow sent with charged_projectiles")
		}
	})
}

// TestJavaCrossbowEarlyRelease: letting go before the charge time leaves the crossbow empty and
// the arrows where they were, as on vanilla.
func TestJavaCrossbowEarlyRelease(t *testing.T) {
	crossbowWorld(t, func(tx *world.Tx, s *Session, p *player.Player) {
		_ = p.Inventory().SetItem(0, item.NewStack(item.Crossbow{}, 1))
		_, _ = p.Inventory().AddItem(item.NewStack(item.Arrow{}, 8))
		p.SetHeldSlot(0)
		p.UseItem()
		s.releaseUseItem(p)
		held, _ := p.HeldItems()
		if cb, _ := held.Item().(item.Crossbow); !cb.Item.Empty() {
			t.Fatal("crossbow loaded by an early release")
		}
		if p.UsingItem() || countArrows(p) != 8 {
			t.Fatalf("using %v, arrows %d after an early release", p.UsingItem(), countArrows(p))
		}
	})
}

func countArrows(p *player.Player) (n int) {
	for _, it := range p.Inventory().Items() {
		if _, ok := it.Item().(item.Arrow); ok {
			n += it.Count()
		}
	}
	return n
}

// crossbowWorld runs f with a test session whose survival player stands in a world that can spawn
// arrows.
func crossbowWorld(t *testing.T, f func(tx *world.Tx, s *Session, p *player.Player)) {
	t.Helper()
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	_, _ = world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
		pos := mgl64.Vec3{0.5, 10, 0.5}
		h := world.EntitySpawnOpts{Position: pos}.New(player.Type, player.Config{
			Name: "Archer", UUID: uuid.New(), GameMode: world.GameModeSurvival, Health: 20, MaxHealth: 20, Position: pos,
		})
		p := tx.AddEntity(h).(*player.Player)
		s := mobTestSession()
		s.ent = p.H()
		f(tx, s, p)
		return struct{}{}, nil
	})
}
