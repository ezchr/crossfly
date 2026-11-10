//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/ride"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// llamaCarpet returns what the carpet slot (1) of a llama holds.
func llamaCarpet(t *testing.T, r *mobtest.Ref) item.Stack {
	t.Helper()
	var s item.Stack
	r.With(func(m *mob.Mob) {
		e, ok := mob.ComponentOf[*ride.Equippable](m)
		if !ok {
			t.Fatal("the llama has no minecraft:equippable")
		}
		s = e.Item(1)
	})
	return s
}

// llamaWithCarpet spawns a tamed llama and gives it a red carpet by hand (interact has_equipment "carpet", which
// is every colour as the BDS alias).
func llamaWithCarpet(t *testing.T) (*mobtest.World, *mobtest.Player, *mobtest.Ref) {
	t.Helper()
	needs(t, true, true, "minecraft:interact", "minecraft:equippable")
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	w.Do(func(tx *world.Tx) { w.Arena(tx, 6, 2) })
	p := w.AddPlayer("rider", mgl64.Vec3{2.5, 1, 0.5})
	l := w.Spawn("minecraft:llama", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_tame_adult"})
	w.Tick(1)
	red := item.NewStack(block.Carpet{Colour: item.ColourRed()}, 1)
	if _, ok := p.Interact(l, red); !ok {
		t.Fatalf("giving the carpet was not handled:\n%v", w.Dump(l))
	}
	if c := llamaCarpet(t, l); c.Empty() || c.Item() != red.Item() {
		t.Fatalf("carpet slot after giving a red carpet: %v", c)
	}
	return w, p, l
}

// TestLlamaCarpetByHand: a tamed llama takes a carpet of any colour by hand.
func TestLlamaCarpetByHand(t *testing.T) {
	llamaWithCarpet(t)
}

// TestLlamaCarpetShears: shears take the carpet off a llama again. This also needs the llama carpet slot of
// 3c7e241e (drop_item_slot "0" is the carpet slot 1).
func TestLlamaCarpetShears(t *testing.T) {
	w, p, l := llamaWithCarpet(t)
	w.Tick(1)
	if _, ok := p.Interact(l, item.NewStack(item.Shears{}, 1)); !ok {
		t.Fatalf("shearing was not handled:\n%v", w.Dump(l))
	}
	if c := llamaCarpet(t, l); !c.Empty() {
		t.Errorf("carpet slot after shearing: %v", c)
	}
	if n := w.Count(isItem("carpet")); n == 0 {
		t.Error("shearing dropped no carpet")
	}
}
