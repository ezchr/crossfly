//go:build crossfly_full

package mobtest_test

import (
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// droppedItems counts the item entities of the world holding an item of the type of it.
func droppedItems(w *mobtest.World, it world.Item) int {
	return w.Count(func(e world.Entity) bool {
		ent, ok := e.(*entity.Ent)
		if !ok {
			return false
		}
		b, ok := ent.Behaviour().(*entity.ItemBehaviour)
		return ok && b.Item().Item() == it
	})
}

// TestSnifferLaysEgg: a pregnant sniffer (on_pregnant) drops a sniffer egg item (spawn_entity spawn_item).
func TestSnifferLaysEgg(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 6, block.Stone{}) })
	s := w.Spawn("minecraft:sniffer", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_adult"})
	s.With(func(m *mob.Mob) { m.Fire("on_pregnant", nil) })
	if _, ok := w.TickUntil(40, func(*world.Tx) bool { return droppedItems(w, block.SnifferEgg{}) == 1 }); !ok {
		t.Fatalf("no sniffer egg dropped: %v", w.Dump(s))
	}
}

// TestSnifferEggHatches: a max cracked sniffer egg hatches into a baby sniffer and goes.
func TestSnifferEggHatches(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{})
	pos := cube.Pos{0, 1, 0}
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 4, block.Dirt{})
		e := block.SnifferEgg{Cracks: 2}
		tx.SetBlock(pos, e, nil)
		e.ScheduledTick(pos, tx, rand.New(rand.NewPCG(1, 2)))
		if _, ok := tx.Block(pos).(block.Air); !ok {
			t.Errorf("a hatched sniffer egg left %v", tx.Block(pos))
		}
	})
	sniffers := w.Mobs("minecraft:sniffer")
	if len(sniffers) != 1 {
		t.Fatalf("%d sniffers hatched, want 1", len(sniffers))
	}
	sniffers[0].With(func(m *mob.Mob) {
		if !m.Baby() {
			t.Error("the sniffer egg hatched an adult")
		}
	})
}
