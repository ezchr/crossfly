//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// droppedNames returns the item names of the item entities in the world.
func droppedNames(w *mobtest.World) map[string]int {
	names := map[string]int{}
	w.Count(func(e world.Entity) bool {
		if ent, ok := e.(*entity.Ent); ok {
			if b, ok := ent.Behaviour().(*entity.ItemBehaviour); ok {
				n, _ := b.Item().Item().EncodeItem()
				names[n] += b.Item().Count()
			}
		}
		return false
	})
	return names
}

// TestFrogEatsSlime: a frog eats a small slime or magma cube, and the loot table rolled for the frog and its
// prey (frog.json: entity_killed tests the prey, has_variant the frog) drops a slime ball, or the froglight of
// the frog's variant.
func TestFrogEatsSlime(t *testing.T) {
	mobtest.RequireDefinitions(t)
	for _, c := range []struct {
		name, frogEvent, prey, want string
	}{
		{"slime", "spawn_temperate", "minecraft:slime", "minecraft:slime_ball"},
		{"temperate", "spawn_temperate", "minecraft:magma_cube", "minecraft:ochre_froglight"},
		{"cold", "spawn_cold", "minecraft:magma_cube", "minecraft:verdant_froglight"},
		{"warm", "spawn_warm", "minecraft:magma_cube", "minecraft:pearlescent_froglight"},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := mobtest.New(t, mobtest.Options{})
			w.Do(func(tx *world.Tx) { w.Arena(tx, 6, 3) })
			fr := w.Spawn("minecraft:frog", mgl64.Vec3{-2.5, 1, 0.5}, mob.SpawnOptions{Event: c.frogEvent})
			prey := w.Spawn(c.prey, mgl64.Vec3{2.5, 1, 0.5}, mob.SpawnOptions{Event: "spawn_small"})
			if _, ok := w.TickUntil(600, func(*world.Tx) bool { return prey.Gone() }); !ok {
				t.Fatalf("the frog did not eat the %v: %v", c.prey, w.Dump(fr))
			}
			got := droppedNames(w)
			if got[c.want] != 1 || len(got) != 1 {
				t.Errorf("eating a %v dropped %v, want one %v", c.prey, got, c.want)
			}
		})
	}
}
