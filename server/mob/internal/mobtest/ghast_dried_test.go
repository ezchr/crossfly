//go:build crossfly_full

package mobtest_test

import (
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
)

// TestDriedGhastBecomesGhastling: a fully rehydrated, waterlogged dried ghast turns into a ghastling (a baby
// happy ghast) at its next step, whose home is the block; the water stays.
func TestDriedGhastBecomesGhastling(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{})
	pos := cube.Pos{0, 1, 0}
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 4, block.Stone{})
		tx.SetBlock(pos, block.Water{Still: true, Depth: 8}, nil)
		d := block.DriedGhast{Facing: cube.South, Hydration: 3}
		tx.SetBlock(pos, d, nil)
		d.ScheduledTick(pos, tx, rand.New(rand.NewPCG(1, 2)))
		if b := tx.Block(pos); b != (block.Water{Still: true, Depth: 8}) {
			t.Errorf("the dried ghast left %v behind, want its water", b)
		}
	})
	found := false
	w.Do(func(tx *world.Tx) {
		for e := range tx.Entities() {
			m, ok := mob.Of(e)
			if !ok || m.Identifier() != "minecraft:happy_ghast" {
				continue
			}
			found = true
			if !m.Baby() {
				t.Error("the dried ghast became an adult happy ghast")
			}
			if home, ok := m.Home(); !ok || home != pos {
				t.Errorf("ghastling home %v (%v), want %v", home, ok, pos)
			}
		}
	})
	if !found {
		t.Fatal("no ghastling came out of the dried ghast")
	}
}
