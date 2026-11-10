//go:build crossfly_full

package mobtest_test

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// golemDispenseShears dispenses shears from a dispenser at (0, 1, 0) facing east and returns what is left of them.
func golemDispenseShears(t *testing.T, w *mobtest.World) item.Stack {
	t.Helper()
	var left item.Stack
	w.Do(func(tx *world.Tx) {
		d := block.NewDispenser()
		d.Facing = cube.FaceEast
		pos := cube.Pos{0, 1, 0}
		_ = d.Inventory(tx, pos).SetItem(0, item.NewStack(item.Shears{}, 1))
		d.ScheduledTick(pos, tx, rand.New(rand.NewPCG(5, 6)))
		left, _ = d.Inventory(tx, pos).Item(0)
	})
	return left
}

func golemCountItems(w *mobtest.World, name string) int {
	return w.Count(func(e world.Entity) bool {
		ent, ok := e.(*entity.Ent)
		if !ok {
			return false
		}
		i, ok := ent.Behaviour().(*entity.ItemBehaviour)
		if !ok {
			return false
		}
		n, _ := i.Item().Item().EncodeItem()
		return strings.HasSuffix(n, name)
	})
}

func golemShearWorld(t *testing.T, id string, opts ...mob.SpawnOptions) (*mobtest.World, *mobtest.Ref) {
	t.Helper()
	w := mobtest.New(t, mobtest.Options{Seed: 3})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 4, block.Stone{}) })
	r := w.Spawn(id, mgl64.Vec3{1.5, 1, 0.5}, opts...)
	w.Tick(1)
	return w, r
}

func TestDispenserShearsSheep(t *testing.T) {
	needs(t, true, true, "minecraft:interact", "minecraft:is_sheared")
	w, sheep := golemShearWorld(t, "minecraft:sheep", mob.SpawnOptions{Event: "minecraft:entity_spawned"})
	sheep.With(func(m *mob.Mob) {
		if m.Baby() {
			m.Fire("minecraft:ageable_grow_up", nil)
		}
		if m.Baby() || m.Sheared() {
			t.Fatalf("sheep baby %v sheared %v before shearing", m.Baby(), m.Sheared())
		}
	})
	left := golemDispenseShears(t, w)
	sheep.With(func(m *mob.Mob) {
		if !m.Sheared() {
			t.Fatal("sheep not sheared")
		}
	})
	if d := left.MaxDurability() - left.Durability(); d != 1 {
		t.Fatalf("shears took %d damage, want 1", d)
	}
	if n := golemCountItems(w, "_wool"); n == 0 {
		t.Fatal("no wool dropped")
	}
	// A sheared sheep is not sheared again: the shears stay as they are.
	left = golemDispenseShears(t, w)
	if d := left.MaxDurability() - left.Durability(); d != 0 {
		t.Fatalf("shears damaged shearing a sheared sheep: %d", d)
	}
}

func TestDispenserLeavesBabySheepAndEmptyBlock(t *testing.T) {
	needs(t, true, true, "minecraft:interact", "minecraft:is_sheared")
	w, sheep := golemShearWorld(t, "minecraft:sheep", mob.SpawnOptions{Event: "minecraft:entity_born"})
	left := golemDispenseShears(t, w)
	sheep.With(func(m *mob.Mob) {
		if !m.Baby() {
			t.Skip("sheep not born a baby")
		}
		if m.Sheared() {
			t.Fatal("baby sheep sheared")
		}
	})
	if d := left.MaxDurability() - left.Durability(); d != 0 || left.Empty() {
		t.Fatalf("shears %v damaged by %d with only a baby in front", left, d)
	}
	// Nothing in front: not a failure, the shears stay in the dispenser.
	empty := mobtest.New(t, mobtest.Options{})
	if left := golemDispenseShears(t, empty); left.Empty() || left.Durability() != left.MaxDurability() {
		t.Fatalf("shears %v after dispensing into nothing", left)
	}
	if golemCountItems(empty, "minecraft:shears") != 0 {
		t.Fatal("shears dropped out of the dispenser")
	}
}

func TestDispenserShearsSnowGolemAndMooshroom(t *testing.T) {
	needs(t, true, true, "minecraft:interact", "minecraft:is_sheared")
	w, golem := golemShearWorld(t, "minecraft:snow_golem")
	golemDispenseShears(t, w)
	golem.With(func(m *mob.Mob) {
		if !m.Sheared() {
			t.Fatal("snow golem not sheared")
		}
	})
	if golemCountItems(w, "minecraft:carved_pumpkin") != 1 {
		t.Fatal("snow golem dropped no carved pumpkin")
	}

	w, cow := golemShearWorld(t, "minecraft:mooshroom", mob.SpawnOptions{Event: "minecraft:entity_spawned"})
	cow.With(func(m *mob.Mob) {
		if m.Baby() {
			m.Fire("minecraft:ageable_grow_up", nil)
		}
		if m.Baby() || m.Variant() != 0 {
			t.Fatalf("mooshroom baby %v variant %v before shearing", m.Baby(), m.Variant())
		}
	})
	golemDispenseShears(t, w)
	cow.With(func(m *mob.Mob) {
		if !m.HasComponent("minecraft:transformation") {
			t.Fatal("sheared mooshroom is not turning into a cow")
		}
	})
	// Mushrooms are no items in Crossfly yet; once they are, the shear loot drops them.
	if _, ok := world.ItemByName("minecraft:red_mushroom", 0); ok && golemCountItems(w, "minecraft:red_mushroom") == 0 {
		t.Fatal("red mooshroom dropped no red mushrooms")
	}
}
