//go:build crossfly_full

package world_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// TestRedstoneMemoKeptOverPowerChange checks that a change of the power of dust alone keeps what was worked out
// about the blocks around it, and a change of its shape does not.
func TestRedstoneMemoKeptOverPowerChange(t *testing.T) {
	w := newPerfWorld(t)
	defer w.Close()
	wire, near := cube.Pos{0, 64, 0}, cube.Pos{1, 64, 0}
	perfDo(w, func(tx *world.Tx) {
		tx.SetBlock(wire, block.RedstoneWire{}, nil)
		_, _, seq := tx.RedstoneMemo(world.RedstoneMemoWireLinks, near)
		tx.SetRedstoneMemo(world.RedstoneMemoWireLinks, near, seq, 5)
		tx.SetBlock(wire, block.RedstoneWire{Power: 9}, nil)
		if v, ok, _ := tx.RedstoneMemo(world.RedstoneMemoWireLinks, near); !ok || v != 5 {
			t.Fatal("a change of the power of dust dropped the memo next to it")
		}
		tx.SetBlock(wire, block.RedstoneWire{Power: 9, Dot: true}, nil)
		if _, ok, _ := tx.RedstoneMemo(world.RedstoneMemoWireLinks, near); ok {
			t.Fatal("a change of dust to a dot kept the memo next to it")
		}
		_, _, seq = tx.RedstoneMemo(world.RedstoneMemoWireLinks, near)
		tx.SetRedstoneMemo(world.RedstoneMemoWireLinks, near, seq, 5)
		tx.SetBlock(wire, block.RedstoneLamp{}, nil)
		if _, ok, _ := tx.RedstoneMemo(world.RedstoneMemoWireLinks, near); ok {
			t.Fatal("replacing dust with a lamp kept the memo next to it")
		}
	})
}
