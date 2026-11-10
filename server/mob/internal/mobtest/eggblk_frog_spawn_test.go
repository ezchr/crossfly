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
	"github.com/go-gl/mathgl/mgl64"
)

// TestFrogLaysFrogSpawn: a pregnant frog (become_pregnant) goes to a pond and lays frogspawn on the water,
// which starts hatching; the frog is no longer pregnant.
func TestFrogLaysFrogSpawn(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Arena(tx, 8, 3)
		w.Water(tx, cube.Pos{3, 0, -2}, cube.Pos{6, 0, 2})
	})
	fr := w.Spawn("minecraft:frog", mgl64.Vec3{-3.5, 1, 0.5})
	fr.With(func(m *mob.Mob) { m.Fire("become_pregnant", nil) })
	var at cube.Pos
	if _, ok := w.TickUntil(1200, func(tx *world.Tx) bool {
		for p := range cube.Range3D(cube.Pos{-8, 0, -8}, cube.Pos{8, 3, 8}) {
			if _, ok := tx.Block(p).(block.FrogSpawn); ok {
				at = p
				return true
			}
		}
		return false
	}); !ok {
		t.Fatalf("the frog laid no frogspawn: %v", w.Dump(fr))
	}
	w.Do(func(tx *world.Tx) {
		if l, ok := tx.Liquid(at.Side(cube.FaceDown)); !ok || l.LiquidDepth() != 8 {
			t.Errorf("frogspawn at %v is not on a water source", at)
		}
		if !tx.BlockUpdateScheduled(at, block.FrogSpawn{}) {
			t.Error("laid frogspawn has no hatching tick")
		}
	})
	w.Tick(2)
	fr.With(func(m *mob.Mob) {
		if m.HasComponent("minecraft:behavior.lay_egg") {
			t.Error("the frog is still pregnant after laying")
		}
	})
}

// TestFrogSpawnHatches: frogspawn on water hatches into 2-6 persistent tadpoles and goes.
func TestFrogSpawnHatches(t *testing.T) {
	mobtest.RequireDefinitions(t)
	seen := map[int]bool{}
	for seed := uint64(1); seed <= 40; seed++ {
		w := mobtest.New(t, mobtest.Options{})
		pos := cube.Pos{0, 1, 0}
		w.Do(func(tx *world.Tx) {
			w.Floor(tx, -1, 3, block.Stone{})
			w.Water(tx, cube.Pos{-2, 0, -2}, cube.Pos{2, 0, 2})
			tx.SetBlock(pos, block.FrogSpawn{}, nil)
			block.FrogSpawn{}.ScheduledTick(pos, tx, rand.New(rand.NewPCG(seed, 7)))
			if _, ok := tx.Block(pos).(block.Air); !ok {
				t.Errorf("hatched frogspawn left %v", tx.Block(pos))
			}
		})
		tadpoles := w.Mobs("minecraft:tadpole")
		if n := len(tadpoles); n < 2 || n > 6 {
			t.Fatalf("%d tadpoles hatched, want 2-6", n)
		}
		seen[len(tadpoles)] = true
		tadpoles[0].With(func(m *mob.Mob) {
			if !m.Persistent() {
				t.Error("hatched tadpole is not persistent")
			}
		})
		w.Close()
	}
	if len(seen) != 5 {
		t.Errorf("tadpole counts: %v, want all of 2-6", seen)
	}
}
