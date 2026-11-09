//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestMonstersRemovedOnPeaceful: Monster::normalTick and
// Slime::preNormalTick remove monsters on peaceful; animals and piglins
// (whose constructor drops RemoveInPeacefulFlagComponent) stay.
func TestMonstersRemovedOnPeaceful(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 12, block.Stone{}) })
	ids := []string{"minecraft:zombie", "minecraft:creeper", "minecraft:slime", "minecraft:cow", "minecraft:piglin"}
	for i, id := range ids {
		w.Spawn(id, mgl64.Vec3{float64(i*4) - 8, 1, 0.5})
	}
	w.Tick(5)
	for _, id := range ids {
		if len(w.Mobs(id)) != 1 {
			t.Fatalf("%s gone on normal difficulty", id)
		}
	}
	w.Do(func(tx *world.Tx) { tx.World().SetDifficulty(world.DifficultyPeaceful) })
	w.Tick(2)
	for _, id := range ids {
		want := 0
		if id == "minecraft:cow" || id == "minecraft:piglin" {
			want = 1
		}
		if n := len(w.Mobs(id)); n != want {
			t.Errorf("%d %s on peaceful, want %d", n, id, want)
		}
	}
}
