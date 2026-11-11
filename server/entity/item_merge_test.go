package entity

import (
	"context"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type mergeViewer struct {
	world.NopViewer
	shown, hidden int
	sizes         []int
}

func (v *mergeViewer) ViewEntity(world.Entity) { v.shown++ }
func (v *mergeViewer) HideEntity(world.Entity) { v.hidden++ }
func (v *mergeViewer) ViewEntityAction(_ world.Entity, a world.EntityAction) {
	if s, ok := a.(StackSizeAction); ok {
		v.sizes = append(v.sizes, s.Count)
	}
}

// TestItemMergeInPlace: two stacks merging keep the larger entity, which grows; viewers see one removal
// and a stack size update, no new entity.
func TestItemMergeInPlace(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	v := &mergeViewer{}
	_, _ = world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
		l := world.NewLoader(2, w, v)
		l.Move(tx, mgl64.Vec3{0.5, 0, 0.5})
		l.Load(tx, 25)
		small := tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 0, 0.5}}, item.NewStack(item.Apple{}, 3))).(*Ent)
		big := tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{0.8, 0, 0.5}}, item.NewStack(item.Apple{}, 10))).(*Ent)
		v.shown, v.hidden = 0, 0
		if !small.Behaviour().(*ItemBehaviour).merge(small, big, tx) {
			t.Fatal("no merge")
		}
		if v.shown != 0 || v.hidden != 1 || len(v.sizes) != 1 || v.sizes[0] != 13 {
			t.Fatalf("shown %d, hidden %d, sizes %v; want 0, 1, [13]", v.shown, v.hidden, v.sizes)
		}
		if n := big.Behaviour().(*ItemBehaviour).Item().Count(); n != 13 {
			t.Fatalf("kept stack has %d", n)
		}
		// Overflow: 60 + 13 keeps 64 in the larger and 9 in the other, both in place.
		full := tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 0, 0.8}}, item.NewStack(item.Apple{}, 60))).(*Ent)
		v.shown, v.hidden, v.sizes = 0, 0, nil
		big.Behaviour().(*ItemBehaviour).merge(big, full, tx)
		if v.shown != 0 || v.hidden != 0 || len(v.sizes) != 2 || v.sizes[0] != 64 || v.sizes[1] != 9 {
			t.Fatalf("overflow: shown %d, hidden %d, sizes %v; want 0, 0, [64 9]", v.shown, v.hidden, v.sizes)
		}
		return struct{}{}, nil
	})
}
