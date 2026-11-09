//go:build crossfly_full

package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func TestLecternPageTurnPulse(t *testing.T) {
	w := world.Config{Dim: world.End, Synchronous: true}.New()
	at := cube.Pos{0, 64, 0}
	runWorld(w, func(tx *world.Tx) {
		book := item.NewStack(item.BookAndQuill{Pages: []string{"a", "b", "c"}}, 1)
		tx.SetBlock(at, Lectern{Book: book}, nil)
		if err := tx.Block(at).(Lectern).TurnPage(at, tx, 1); err != nil {
			t.Fatal(err)
		}
		l := tx.Block(at).(Lectern)
		if !l.Powered || l.RedstonePower(at, tx, cube.FaceNorth) != 15 || l.RedstoneStrongPower(at, tx, cube.FaceDown) != 15 {
			t.Fatalf("page turn did not start the pulse: %+v", l)
		}
		if l.RedstoneStrongPower(at, tx, cube.FaceUp) != 0 {
			t.Error("lectern strongly powers the block above")
		}
	})
	w.AdvanceTick()
	runWorld(w, func(tx *world.Tx) {
		if !tx.Block(at).(Lectern).Powered {
			t.Error("pulse ended after one tick, want two")
		}
	})
	w.AdvanceTick()
	runWorld(w, func(tx *world.Tx) {
		if tx.Block(at).(Lectern).Powered {
			t.Error("pulse still on after two ticks")
		}
	})
}
