//go:build crossfly_full

package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestCopperBulbTogglesOnRisingPower(t *testing.T) {
	w := world.Config{Dim: world.End, Synchronous: true, SettleSynchronousTransactions: true}.New()
	at, src := cube.Pos{0, 64, 0}, cube.Pos{1, 64, 0}
	bulb := func(tx *world.Tx) CopperBulb { return tx.Block(at).(CopperBulb) }
	runWorld(w, func(tx *world.Tx) { tx.SetBlock(at, CopperBulb{}, nil) })
	for i, want := range []bool{true, true, false, false, true} {
		runWorld(w, func(tx *world.Tx) {
			if i%2 == 0 {
				tx.SetBlock(src, RedstoneBlock{}, nil)
			} else {
				tx.SetBlock(src, nil, nil)
			}
		})
		w.AdvanceTick()
		runWorld(w, func(tx *world.Tx) {
			b := bulb(tx)
			if b.Lit != want || b.Powered != (i%2 == 0) {
				t.Fatalf("step %d: lit %v powered %v, want lit %v powered %v", i, b.Lit, b.Powered, want, i%2 == 0)
			}
			if v, _ := redstoneAnalogOutput(at, b, tx); (v == 15) != b.Lit {
				t.Errorf("step %d: comparator reads %d with lit %v", i, v, b.Lit)
			}
		})
	}
}
