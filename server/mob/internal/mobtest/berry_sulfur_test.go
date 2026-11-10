//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// sulfurArchetype returns the minecraft:sulfur_cube_archetype property of a cube.
func sulfurArchetype(r *mobtest.Ref) any {
	var v any
	r.With(func(m *mob.Mob) { v, _ = m.Property("minecraft:sulfur_cube_archetype") })
	return v
}

// TestSulfurCubeArchetypeByHand: a medium sulfur cube given a block by hand (interact, has_equipment_tag of the
// Bedrock item tags sulfur_cube_archetype_*) holds it and takes the archetype of its tag (on_block_absorbed).
func TestSulfurCubeArchetypeByHand(t *testing.T) {
	needs(t, true, true, "minecraft:interact", "minecraft:on_equipment_changed")
	cases := []struct {
		give world.Item
		want string
	}{
		{block.Planks{Wood: block.OakWood()}, "bouncy"},
		{block.TNT{}, "explosive"},
		{block.Wool{Colour: item.ColourRed()}, "light"},
		{block.Magma{}, "hot"},
		{block.SoulSand{}, "high_resistance"},
		{block.Dirt{}, "regular"},
	}
	for _, c := range cases {
		w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
		w.Do(func(tx *world.Tx) { w.Arena(tx, 6, 2) })
		p := w.AddPlayer("kicker", mgl64.Vec3{2.5, 1, 0.5})
		cube := w.Spawn("minecraft:sulfur_cube", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_medium"})
		w.Tick(2)
		if _, ok := p.Interact(cube, item.NewStack(c.give, 1)); !ok {
			t.Errorf("giving %T was not handled:\n%v", c.give, w.Dump(cube))
			continue
		}
		w.Tick(2)
		if got := sulfurArchetype(cube); got != c.want {
			t.Errorf("given %T: archetype %v, want %s", c.give, got, c.want)
		}
		cube.With(func(m *mob.Mob) {
			if h, _ := m.HeldItems(); h.Empty() {
				t.Errorf("given %T: the cube holds nothing", c.give)
			}
		})
	}
}

// TestSulfurCubeRefusesOtherItems: an item without an archetype tag is not taken.
func TestSulfurCubeRefusesOtherItems(t *testing.T) {
	needs(t, true, true, "minecraft:interact")
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	w.Do(func(tx *world.Tx) { w.Arena(tx, 6, 2) })
	p := w.AddPlayer("kicker", mgl64.Vec3{2.5, 1, 0.5})
	cube := w.Spawn("minecraft:sulfur_cube", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_medium"})
	w.Tick(2)
	p.Interact(cube, item.NewStack(item.Stick{}, 1))
	w.Tick(2)
	cube.With(func(m *mob.Mob) {
		if h, _ := m.HeldItems(); !h.Empty() {
			t.Errorf("the cube took %v", h)
		}
	})
	if got := sulfurArchetype(cube); got != "none" {
		t.Errorf("archetype %v, want none", got)
	}
}
