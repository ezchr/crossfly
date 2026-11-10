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

// TestSnifferEggStates checks that every sniffer egg state is a Bedrock block state of the vanilla palette, that
// the block is an item in the creative inventory, and that it drops itself.
func TestSnifferEggStates(t *testing.T) {
	eggWorld(t)
	for c, n := range []string{"no_cracks", "cracked", "max_cracked"} {
		b, ok := world.BlockByName("minecraft:sniffer_egg", map[string]any{"cracked_state": n})
		if want := (block.SnifferEgg{Cracks: c}); !ok || b != want {
			t.Errorf("%v: got %v (ok=%v), want %v", n, b, ok, want)
		}
	}
	if it, ok := world.ItemByName("minecraft:sniffer_egg", 0); !ok || it != (block.SnifferEgg{}) {
		t.Errorf("sniffer egg item: %v %v", it, ok)
	}
	if !eggInCreative(block.SnifferEgg{}) {
		t.Error("the sniffer egg is not in the creative inventory")
	}
	if d := (block.SnifferEgg{Cracks: 2}).BreakInfo().Drops(item.ToolNone{}, nil); len(d) != 1 || d[0].Item() != (block.SnifferEgg{}) {
		t.Errorf("a sniffer egg drops %v, want itself", d)
	}
}

// TestSnifferEggCracking: each step a sniffer egg cracks a level and queues its next step; cracked twice it
// hatches (without mobs only the block goes).
func TestSnifferEggCracking(t *testing.T) {
	w := eggWorld(t)
	pos := cube.Pos{0, 1, 0}
	r := rand.New(rand.NewPCG(1, 2))
	eggDo(t, w, func(tx *world.Tx) {
		tx.SetBlock(pos.Side(cube.FaceDown), block.Dirt{}, nil)
		tx.SetBlock(pos, block.SnifferEgg{}, nil)
		for c := 1; c <= 2; c++ {
			tx.Block(pos).(block.SnifferEgg).ScheduledTick(pos, tx, r)
			if want := (block.SnifferEgg{Cracks: c}); tx.Block(pos) != want {
				t.Fatalf("step %d: %v, want %v", c, tx.Block(pos), want)
			}
			if !tx.BlockUpdateScheduled(pos, tx.Block(pos)) {
				t.Errorf("step %d: no next step queued", c)
			}
		}
		tx.Block(pos).(block.SnifferEgg).ScheduledTick(pos, tx, r)
		if _, ok := tx.Block(pos).(block.Air); !ok {
			t.Errorf("a hatched sniffer egg left %v", tx.Block(pos))
		}
	})
}

// TestSnifferEggMoss: a sniffer egg takes 7200-8799 ticks a step, half that on a moss block.
func TestSnifferEggMoss(t *testing.T) {
	w := eggWorld(t)
	moss, dirt := cube.Pos{0, 0, 0}, cube.Pos{3, 0, 0}
	eggDo(t, w, func(tx *world.Tx) {
		m, ok := world.BlockByName("minecraft:moss_block", nil)
		if !ok {
			t.Fatal("no moss block state")
		}
		tx.SetBlock(moss, m, nil)
		tx.SetBlock(dirt, block.Dirt{}, nil)
		for _, p := range []cube.Pos{moss, dirt} {
			u := &eggUser{held: item.NewStack(block.SnifferEgg{}, 1), tx: tx}
			if !(block.SnifferEgg{}).UseOnBlock(p, cube.FaceUp, mgl64.Vec3{}, tx, u, &item.UseContext{}) {
				t.Fatalf("placing a sniffer egg on %v failed", tx.Block(p))
			}
		}
	})
	at := func(p cube.Pos) (e block.SnifferEgg) {
		eggDo(t, w, func(tx *world.Tx) { e, _ = tx.Block(p.Side(cube.FaceUp)).(block.SnifferEgg) })
		return
	}
	for i := 0; i < 3599; i++ {
		w.AdvanceTick()
	}
	if at(moss).Cracks != 0 {
		t.Fatal("the egg on moss cracked before 3600 ticks")
	}
	for i := 0; i < 800; i++ {
		w.AdvanceTick()
	}
	if at(moss).Cracks != 1 || at(dirt).Cracks != 0 {
		t.Fatalf("after 4399 ticks: %v on moss, %v on dirt; want only the one on moss cracked", at(moss), at(dirt))
	}
	for i := 0; i < 4400; i++ {
		w.AdvanceTick()
	}
	if at(dirt).Cracks != 1 {
		t.Errorf("after 8799 ticks the egg on dirt has not cracked: %v", at(dirt))
	}
}
