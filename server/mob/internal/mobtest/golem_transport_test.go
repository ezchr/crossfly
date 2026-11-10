//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// golemBlockActionViewer records the block actions it sees.
type golemBlockActionViewer struct {
	world.NopViewer
	actions []world.BlockAction
	at      []cube.Pos
}

func (v *golemBlockActionViewer) ViewBlockAction(pos cube.Pos, a world.BlockAction) {
	v.actions = append(v.actions, a)
	v.at = append(v.at, pos)
}

func TestCopperGolemOpensChestWhilePuttingItemsIn(t *testing.T) {
	needs(t, true, true, "minecraft:behavior.transport_items")
	w := mobtest.New(t, mobtest.Options{})
	chestPos := cube.Pos{2, 1, 0}
	viewer := &golemBlockActionViewer{}
	var loader *world.Loader
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 6, block.Stone{})
		tx.SetBlock(chestPos, block.NewChest(), nil)
		loader = world.NewLoader(2, w.W, viewer)
		loader.Move(tx, mgl64.Vec3{0, 1, 0})
		loader.Load(tx, 25)
	})
	defer w.Do(func(tx *world.Tx) { loader.Close(tx) })
	golem := w.Spawn("minecraft:copper_golem", mgl64.Vec3{0.5, 1, 0.5})
	golem.With(func(m *mob.Mob) { m.SetHeldItems(item.NewStack(item.Diamond{}, 5), item.Stack{}) })

	state := func() string {
		var v any
		golem.With(func(m *mob.Mob) { v, _ = m.Property("minecraft:chest_interaction") })
		s, _ := v.(string)
		return s
	}
	_, ok := w.TickUntil(400, func(*world.Tx) bool { return state() == "put" })
	if !ok {
		t.Fatalf("golem never started putting its items in: %v", w.Dump(golem))
	}
	if len(viewer.actions) == 0 {
		t.Fatal("chest lid did not open")
	}
	if _, open := viewer.actions[len(viewer.actions)-1].(block.OpenAction); !open || viewer.at[len(viewer.at)-1] != chestPos {
		t.Fatalf("last block action %T at %v, want the chest opening", viewer.actions[len(viewer.actions)-1], viewer.at[len(viewer.at)-1])
	}
	opened := len(viewer.actions)
	end, ok := w.TickUntil(100, func(*world.Tx) bool { return state() == "none" })
	if !ok {
		t.Fatal("golem never stopped interacting")
	}
	// Java CopperGolemAi: open on the first tick, put the items in on the sixtieth.
	if end != 59 {
		t.Errorf("interaction lasted %d more ticks, want 59", end)
	}
	if len(viewer.actions) == opened {
		t.Fatal("chest lid did not close")
	}
	if _, closed := viewer.actions[len(viewer.actions)-1].(block.CloseAction); !closed {
		t.Fatalf("chest lid not closed: last action %T", viewer.actions[len(viewer.actions)-1])
	}
	w.Do(func(tx *world.Tx) {
		inv := tx.Block(chestPos).(block.Chest).Inventory(tx, chestPos)
		if !inv.ContainsItem(item.NewStack(item.Diamond{}, 5)) {
			t.Fatalf("chest holds %v, want 5 diamonds", inv.Items())
		}
	})
	golem.With(func(m *mob.Mob) {
		if main, _ := m.HeldItems(); !main.Empty() {
			t.Fatalf("golem still holds %v", main)
		}
	})
}
