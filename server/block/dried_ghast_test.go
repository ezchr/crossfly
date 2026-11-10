package block_test

import (
	"testing"
	"time"
	_ "unsafe"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item/creative"
	"github.com/df-mc/dragonfly/server/world"
)

// TestDriedGhastStates checks that every dried ghast state is a Bedrock block state of the vanilla block
// palette (facing and rehydration level), and that the block is an item.
func TestDriedGhastStates(t *testing.T) {
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry}.New()
	defer w.Close()
	for _, f := range cube.Directions() {
		for h := int32(0); h <= 3; h++ {
			b, ok := world.BlockByName("minecraft:dried_ghast", map[string]any{
				"minecraft:cardinal_direction": f.String(), "rehydration_level": h,
			})
			if want := (block.DriedGhast{Facing: f, Hydration: int(h)}); !ok || b != want {
				t.Errorf("%v level %d: got %v (ok=%v), want %v", f, h, b, ok, want)
			}
		}
	}
	if it, ok := world.ItemByName("minecraft:dried_ghast", 0); !ok || it != (block.DriedGhast{}) {
		t.Errorf("dried ghast item: %v %v", it, ok)
	}
	driedGhastCreativeRegister()
	found := false
	for _, c := range creative.Items() {
		_, ok := c.Stack.Item().(block.DriedGhast)
		found = found || ok
	}
	if !found {
		t.Error("the dried ghast is not in the creative inventory")
	}
}

//go:linkname driedGhastCreativeRegister github.com/df-mc/dragonfly/server/item/creative.registerCreativeItems
func driedGhastCreativeRegister()

// driedGhastAt returns the dried ghast at pos, or false.
func driedGhastAt(w *world.World, pos cube.Pos) (d block.DriedGhast, ok bool) {
	w.Do(func(tx *world.Tx) { d, ok = tx.Block(pos).(block.DriedGhast) })
	return
}

// TestDriedGhastHydration: waterlogged, a dried ghast rehydrates one level every 6000 ticks; out of water it
// dries out a level every 6000 ticks down to fully dry. Without mobs a fully rehydrated dried ghast stays.
func TestDriedGhastHydration(t *testing.T) {
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry}.New()
	defer w.Close()
	wet, dry := cube.Pos{0, 1, 0}, cube.Pos{4, 1, 0}
	w.Do(func(tx *world.Tx) {
		for x := -1; x <= 5; x++ {
			tx.SetBlock(cube.Pos{x, 0, 0}, block.Stone{}, nil)
		}
		tx.SetBlock(wet, block.Water{Still: true, Depth: 8}, nil)
		tx.SetBlock(wet, block.DriedGhast{Facing: cube.East}, nil)
		tx.SetBlock(dry, block.DriedGhast{Facing: cube.North, Hydration: 2}, nil)
		tx.ScheduleBlockUpdate(wet, block.DriedGhast{Facing: cube.East}, 6000*50*time.Millisecond)
		tx.ScheduleBlockUpdate(dry, block.DriedGhast{Facing: cube.North, Hydration: 2}, 6000*50*time.Millisecond)
	})
	w.Do(func(tx *world.Tx) {
		if l, ok := tx.Liquid(wet); !ok || l != (block.Water{Still: true, Depth: 8}) {
			t.Fatalf("the dried ghast did not keep the water: %v", l)
		}
	})
	for i := 0; i < 5999; i++ {
		w.AdvanceTick()
	}
	if d, _ := driedGhastAt(w, wet); d.Hydration != 0 {
		t.Fatalf("rehydrated to %d before 6000 ticks", d.Hydration)
	}
	for step := 1; step <= 4; step++ {
		for i := 0; i < 6000; i++ {
			w.AdvanceTick()
		}
		wantWet, wantDry := min(step, 3), max(2-step, 0)
		if d, ok := driedGhastAt(w, wet); !ok || d.Hydration != wantWet || d.Facing != cube.East {
			t.Errorf("after %d steps the waterlogged dried ghast is %+v (ok=%v), want level %d", step, d, ok, wantWet)
		}
		if d, ok := driedGhastAt(w, dry); !ok || d.Hydration != wantDry {
			t.Errorf("after %d steps the dry dried ghast is %+v (ok=%v), want level %d", step, d, ok, wantDry)
		}
	}
}
