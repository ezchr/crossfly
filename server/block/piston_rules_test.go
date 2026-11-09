//go:build crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"testing"
)

func TestPistonLeafAndSignRules(t *testing.T) {
	for _, b := range []world.Block{Leaves{Persistent: true}, Sign{}} {
		w := world.Config{Dim: world.End, Synchronous: true}.New()
		runWorld(w, func(tx *world.Tx) {
			tx.SetBlock(cube.Pos{1, 64, 0}, b, nil)
			r, ok := resolvePiston(tx, cube.Pos{0, 64, 0}, cube.FaceEast, true)
			if _, sign := b.(Sign); sign {
				if ok {
					t.Error("Java signs block piston movement")
				}
				return
			}
			if !ok || len(r.destroy) != 1 || len(r.push) != 0 {
				t.Errorf("Java pops %T: success=%t push=%v destroy=%v", b, ok, r.push, r.destroy)
			}
		})
		w.Close()
	}
}
