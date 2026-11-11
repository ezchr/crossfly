package player

import (
	"context"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

type itemsViewer struct {
	world.NopViewer
	items int
}

func (v *itemsViewer) ViewEntityItems(world.Entity) { v.items++ }

// TestDurabilityNotShownToViewers: a held sword losing durability is not shown again to viewers; a
// different item, count or enchantment is.
func TestDurabilityNotShownToViewers(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	v := &itemsViewer{}
	_, _ = world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
		l := world.NewLoader(2, w, v)
		l.Move(tx, mgl64.Vec3{0.5, 0, 0.5})
		l.Load(tx, 25)
		pos := mgl64.Vec3{0.5, 0, 0.5}
		p := tx.AddEntity(world.EntitySpawnOpts{Position: pos}.New(Type, Config{
			Name: "Bot", UUID: uuid.New(), GameMode: world.GameModeSurvival, Health: 20, MaxHealth: 20, Position: pos,
		})).(*Player)
		sword := item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1)
		p.SetHeldItems(sword, item.Stack{})
		v.items = 0
		for i := 1; i <= 10; i++ {
			p.SetHeldItems(sword.Damage(i), item.Stack{})
		}
		if v.items != 0 {
			t.Fatalf("durability-only changes shown %d times", v.items)
		}
		p.SetHeldItems(item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1), item.Stack{})
		p.SetHeldItems(item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1), item.NewStack(item.Apple{}, 1))
		p.SetHeldItems(item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1), item.NewStack(item.Apple{}, 2))
		if v.items != 3 {
			t.Fatalf("item, off-hand and count changes shown %d times, want 3", v.items)
		}
		return struct{}{}, nil
	})
}
