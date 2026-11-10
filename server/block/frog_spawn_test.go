//go:build crossfly_full

package block_test

import (
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestFrogSpawnRegistered checks that frogspawn is a block and an item, in the creative inventory.
func TestFrogSpawnRegistered(t *testing.T) {
	eggWorld(t)
	if b, ok := world.BlockByName("minecraft:frog_spawn", nil); !ok || b != (block.FrogSpawn{}) {
		t.Errorf("frogspawn block: %v %v", b, ok)
	}
	if it, ok := world.ItemByName("minecraft:frog_spawn", 0); !ok || it != (block.FrogSpawn{}) {
		t.Errorf("frogspawn item: %v %v", it, ok)
	}
	if !eggInCreative(block.FrogSpawn{}) {
		t.Error("frogspawn is not in the creative inventory")
	}
	if d := (block.FrogSpawn{}).BreakInfo().Drops(item.ToolNone{}, nil); len(d) != 0 {
		t.Errorf("frogspawn drops %v", d)
	}
}

// TestFrogSpawnPlacing: frogspawn goes on the surface of a water source clicked, like a lily pad, and starts
// hatching; not on flowing water or on land.
func TestFrogSpawnPlacing(t *testing.T) {
	w := eggWorld(t)
	still, flowing, stone := cube.Pos{0, 0, 0}, cube.Pos{2, 0, 0}, cube.Pos{4, 0, 0}
	eggDo(t, w, func(tx *world.Tx) {
		tx.SetBlock(still, block.Water{Still: true, Depth: 8}, nil)
		tx.SetBlock(flowing, block.Water{Depth: 5}, nil)
		tx.SetBlock(stone, block.Stone{}, nil)
		u := &eggUser{held: item.NewStack(block.FrogSpawn{}, 4), tx: tx}
		use := func(p cube.Pos) bool {
			return block.FrogSpawn{}.UseOnBlock(p, cube.FaceUp, mgl64.Vec3{}, tx, u, &item.UseContext{})
		}
		if use(stone) || use(flowing) {
			t.Error("frogspawn went on stone or flowing water")
		}
		above := still.Side(cube.FaceUp)
		if !use(still) || tx.Block(above) != (block.FrogSpawn{}) {
			t.Fatalf("frogspawn on water: %v above, %v in the water", tx.Block(above), tx.Block(still))
		}
		if _, ok := tx.Block(still).(block.Water); !ok {
			t.Errorf("placing frogspawn replaced the water with %v", tx.Block(still))
		}
		if !tx.BlockUpdateScheduled(above, block.FrogSpawn{}) {
			t.Error("placed frogspawn has no hatching tick")
		}
	})
}

// TestFrogSpawnLosesWater: frogspawn breaks when the water under it goes, and at its hatching tick without
// water; with water it hatches (without mobs only the block goes).
func TestFrogSpawnLosesWater(t *testing.T) {
	w := eggWorld(t)
	water, pos := cube.Pos{0, 0, 0}, cube.Pos{0, 1, 0}
	r := rand.New(rand.NewPCG(5, 6))
	eggDo(t, w, func(tx *world.Tx) {
		tx.SetBlock(water, block.Water{Still: true, Depth: 8}, nil)
		tx.SetBlock(pos, block.FrogSpawn{}, nil)
		block.FrogSpawn{}.NeighbourUpdateTick(pos, water, tx)
		if tx.Block(pos) != (block.FrogSpawn{}) {
			t.Fatalf("frogspawn on water broke: %v", tx.Block(pos))
		}
		block.FrogSpawn{}.ScheduledTick(pos, tx, r)
		if _, ok := tx.Block(pos).(block.Air); !ok {
			t.Errorf("frogspawn left %v after hatching", tx.Block(pos))
		}

		tx.SetBlock(pos, block.FrogSpawn{}, nil)
		tx.SetBlock(water, block.Stone{}, nil)
		block.FrogSpawn{}.NeighbourUpdateTick(pos, water, tx)
		if _, ok := tx.Block(pos).(block.Air); !ok {
			t.Errorf("frogspawn without water stayed: %v", tx.Block(pos))
		}
		tx.SetBlock(pos, block.FrogSpawn{}, nil)
		block.FrogSpawn{}.ScheduledTick(pos, tx, r)
		if _, ok := tx.Block(pos).(block.Air); !ok {
			t.Errorf("frogspawn without water at its tick: %v", tx.Block(pos))
		}
	})
}
