//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// itemsNear counts the items of type T lying within r blocks of pos.
func itemsNear[T world.Item](w *mobtest.World, pos mgl64.Vec3, r float64) int {
	n := 0
	w.Do(func(tx *world.Tx) {
		for e := range tx.Entities() {
			ent, ok := e.(*entity.Ent)
			if !ok || ent.Position().Sub(pos).Len() > r {
				continue
			}
			if ib, ok := ent.Behaviour().(*entity.ItemBehaviour); ok {
				if _, ok := ib.Item().Item().(T); ok {
					n += ib.Item().Count()
				}
			}
		}
	})
	return n
}

// TestArmadilloBrushed: brushing an adult armadillo drops an armadillo scute and takes 16 durability off the
// brush (armadillo.json interact: hurt_item 16, spawn_items armadillo_brush).
func TestArmadilloBrushed(t *testing.T) {
	mobtest.RequireDefinitions(t)
	mobtest.RequireFilters(t)
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
	pos := mgl64.Vec3{0.5, 1, 0.5}
	a := w.Spawn("minecraft:armadillo", pos, mob.SpawnOptions{Event: "minecraft:spawn_adult"})
	p := w.AddPlayer("brusher", mgl64.Vec3{2.5, 1, 0.5})
	w.Tick(1)

	left, ok := p.Interact(a, item.NewStack(item.Brush{}, 1))
	if !ok {
		t.Fatalf("armadillo was not brushed:\n%v", w.Dump(a))
	}
	if _, isBrush := left.Item().(item.Brush); !isBrush || left.Durability() != 48 {
		t.Errorf("brush after brushing: %v (durability %v), want a brush with 48", left, left.Durability())
	}
	w.Tick(2)
	if n := itemsNear[item.ArmadilloScute](w, pos, 4); n != 1 {
		t.Errorf("%v armadillo scutes after brushing, want 1", n)
	}
	// Other items do nothing.
	if _, ok := p.Interact(a, item.NewStack(item.Stick{}, 1)); ok {
		t.Errorf("armadillo took a stick")
	}
}
