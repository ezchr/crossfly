//go:build crossfly_full

package javasession

import (
	"github.com/df-mc/dragonfly/server/block"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"testing"
)

func TestDispenserAndDropperUseThreeByThreeMenu(t *testing.T) {
	for _, b := range []block.Dispenser{block.NewDispenser(), block.NewDropper()} {
		m, _, ok := menuFor(b)
		if !ok || m.kind != menuDispenser || m.size != 9 || m.typ != v777.BuiltinID("minecraft:menu", "minecraft:generic_3x3") {
			t.Fatalf("dropper=%t: incorrect menu %+v", b.Dropper, m)
		}
	}
}
