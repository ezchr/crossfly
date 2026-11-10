//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func init() {
	// Harnesses and the dried ghast are items/blocks of the mobs: registered before the first world.
	block.RegisterMobBlocks()
	item.RegisterMobItems()
}

// adultGhast spawns an adult happy ghast at pos over a stone floor and a survival player next to it.
func adultGhast(t *testing.T, pos mgl64.Vec3) (*mobtest.World, *mobtest.Ref, *mobtest.Player) {
	t.Helper()
	mobtest.RequireDefinitions(t)
	mobtest.RequireFilters(t)
	w := mobtest.New(t, mobtest.Options{Time: 6000})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 12, block.Stone{}) })
	g := w.Spawn("minecraft:happy_ghast", pos, mob.SpawnOptions{Event: "minecraft:spawn_adult"})
	p := w.AddPlayer("rider", mgl64.Vec3{pos[0] + 3, 1, pos[2]})
	w.Tick(1)
	return w, g, p
}

// TestHappyGhastHarness: a harness used on an adult happy ghast is worn in its body slot and makes it
// rideable (on_harnessed); shears take it off again, dropping the harness and wearing the shears.
func TestHappyGhastHarness(t *testing.T) {
	w, g, p := adultGhast(t, mgl64.Vec3{0.5, 1, 0.5})
	red := item.Harness{Colour: item.ColourRed()}
	// A creative player keeps the harness (use_item takes nothing in creative).
	p.SetGameMode(world.GameModeCreative)
	left, ok := p.Interact(g, item.NewStack(red, 1))
	if !ok {
		t.Fatal("happy ghast did not take the harness")
	}
	if left.Item() != red {
		t.Errorf("creative player was left with %v", left)
	}
	g.With(func(m *mob.Mob) {
		if b := m.BodyArmour(); b.Item() != red {
			t.Errorf("body slot holds %v, want the red harness", b)
		}
		if !m.HasComponent("minecraft:rideable") {
			t.Errorf("harnessed happy ghast is not rideable: %v", m.Components())
		}
	})
	p.SetGameMode(world.GameModeSurvival)

	shears := item.NewStack(item.Shears{}, 1)
	left, ok = p.Interact(g, shears)
	if !ok {
		t.Fatal("shears did nothing on the harnessed happy ghast")
	}
	if left.Durability() != shears.Durability()-1 {
		t.Errorf("shears durability %d, want %d", left.Durability(), shears.Durability()-1)
	}
	g.With(func(m *mob.Mob) {
		if b := m.BodyArmour(); !b.Empty() {
			t.Errorf("sheared happy ghast still wears %v", b)
		}
		if m.HasComponent("minecraft:rideable") {
			t.Error("sheared happy ghast is still rideable")
		}
	})
	w.Tick(1)
	if !harnessDropped(w, red) {
		t.Error("the sheared harness was not dropped")
	}
}

// TestHappyGhastHarnessSurvival: a survival player's harness is used up and worn. The happy ghast is the only
// mob whose interaction both uses up the item (use_item) and equips it (equip_item_slot), as BDS
// InteractComponent::_tryEquipItem does with the item before it is used up.
func TestHappyGhastHarnessSurvival(t *testing.T) {
	_, g, p := adultGhast(t, mgl64.Vec3{0.5, 1, 0.5})
	left, ok := p.Interact(g, item.NewStack(item.Harness{Colour: item.ColourLime()}, 1))
	if !ok {
		t.Fatal("happy ghast did not take the harness")
	}
	if !left.Empty() {
		t.Errorf("survival player kept %v", left)
	}
	g.With(func(m *mob.Mob) {
		if b := m.BodyArmour(); b.Item() != (item.Harness{Colour: item.ColourLime()}) {
			t.Errorf("body slot holds %v, want the lime harness", b)
		}
	})
}

// harnessDropped reports whether an item entity of h lies in the world.
func harnessDropped(w *mobtest.World, h item.Harness) bool {
	dropped := false
	w.Do(func(tx *world.Tx) {
		for e := range tx.Entities() {
			if it, ok := e.(*entity.Ent); ok {
				if s, ok := it.Behaviour().(*entity.ItemBehaviour); ok && s.Item().Item() == h {
					dropped = true
				}
			}
		}
	})
	return dropped
}
