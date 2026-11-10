//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// hiveAt returns the beehive at pos.
func hiveAt(w *mobtest.World, pos cube.Pos) (h block.Beehive) {
	w.Do(func(tx *world.Tx) { h, _ = tx.Block(pos).(block.Beehive) })
	return
}

// bees returns the bees of the world.
func bees(w *mobtest.World) []*mobtest.Ref { return w.Mobs("minecraft:bee") }

// beeWithNectar spawns an adult bee that just collected nectar (bee.json collected_nectar: return_to_home and
// has_nectar).
func beeWithNectar(w *mobtest.World, pos mgl64.Vec3) *mobtest.Ref {
	b := w.Spawn("minecraft:bee", pos, mob.SpawnOptions{Event: "minecraft:spawn_adult"})
	b.With(func(m *mob.Mob) { m.Fire("collected_nectar", nil) })
	return b
}

func hasNectar(m *mob.Mob) bool {
	v, _ := m.Property("minecraft:has_nectar")
	b, _ := v.(bool)
	return b
}

// TestBeeEntersAndLeavesHive: a bee with nectar flies home and enters its hive (go_home on_home,
// minecraft:bee_returned_to_hive sent to the block), stays 2400 ticks (through a save and load of the world), then
// leaves through the entrance in the daytime without its nectar, with the hive as its home, and the hive has honey.
func TestBeeEntersAndLeavesHive(t *testing.T) {
	mobtest.RequireDefinitions(t)
	mobtest.RequireFilters(t)
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon, Persistent: true})
	hive := cube.Pos{0, 1, 4}
	w.Do(func(tx *world.Tx) {
		w.Arena(tx, 10, 6)
		tx.SetBlock(hive, block.Beehive{Facing: cube.South}, nil)
	})
	b := beeWithNectar(w, mgl64.Vec3{0.5, 2, -4.5})
	b.With(func(m *mob.Mob) { m.SetHome(hive) })
	n, ok := w.TickUntil(600, func(*world.Tx) bool { return len(hiveAt(w, hive).Occupants()) == 1 })
	if !ok {
		t.Fatalf("bee did not enter its hive:\n%v", w.Dump(b))
	}
	if !b.Gone() || len(bees(w)) != 0 {
		t.Fatalf("the bee in the hive is still in the world")
	}
	occ := hiveAt(w, hive).Occupants()[0]
	if occ.Identifier != "minecraft:bee" || occ.TicksLeft < 2300 {
		t.Fatalf("occupant %v, %d ticks left", occ.Identifier, occ.TicksLeft)
	}
	t.Logf("entered after %d ticks", n)

	w.Reload()
	if len(hiveAt(w, hive).Occupants()) != 1 {
		t.Fatalf("the hive lost its bee in a reload")
	}
	n, ok = w.TickUntil(3000, func(*world.Tx) bool { return len(hiveAt(w, hive).Occupants()) == 0 })
	if !ok {
		t.Fatalf("bee did not leave the hive")
	}
	all := bees(w)
	if len(all) != 1 {
		t.Fatalf("%d bees after leaving", len(all))
	}
	t.Logf("left after %d more ticks", n)
	if l := hiveAt(w, hive).HoneyLevel; l < 1 {
		t.Errorf("honey level %d after a bee brought nectar", l)
	}
	all[0].With(func(m *mob.Mob) {
		if hasNectar(m) {
			t.Errorf("bee kept its nectar:\n%v", w.Dump(all[0]))
		}
		if home, ok := m.Home(); !ok || home != hive {
			t.Errorf("home %v %v, want the hive", home, ok)
		}
		if !m.HasComponent("minecraft:behavior.move_to_block") {
			t.Errorf("bee does not look for flowers again (exited_hive):\n%v", w.Dump(all[0]))
		}
		if p := m.Position(); p[2] < float64(hive[2])+1 {
			t.Errorf("bee left at %v, not through the south entrance", p)
		}
	})
}

// TestBeeFindsHive: a bee with nectar whose home is no hive looks for one (go_home on_failed, then find_hive's
// move_to_block on_reach) and enters it.
func TestBeeFindsHive(t *testing.T) {
	mobtest.RequireDefinitions(t)
	mobtest.RequireFilters(t)
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	hive := cube.Pos{3, 1, 3}
	w.Do(func(tx *world.Tx) {
		w.Arena(tx, 10, 6)
		tx.SetBlock(hive, block.Beehive{Nest: true, Facing: cube.West}, nil)
	})
	b := beeWithNectar(w, mgl64.Vec3{-3.5, 2, -3.5})
	n, ok := w.TickUntil(1500, func(*world.Tx) bool { return len(hiveAt(w, hive).Occupants()) == 1 })
	if !ok {
		t.Fatalf("bee did not find the nest:\n%v", w.Dump(b))
	}
	t.Logf("found the nest after %d ticks", n)
}

// TestBeesAngryWhenHarvested: harvesting a full hive with shears without a campfire below lets its bees out angry at
// the player (minecraft:exited_disturbed_hive).
func TestBeesAngryWhenHarvested(t *testing.T) {
	mobtest.RequireDefinitions(t)
	mobtest.RequireFilters(t)
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	hive := cube.Pos{0, 1, 4}
	w.Do(func(tx *world.Tx) {
		w.Arena(tx, 10, 6)
		tx.SetBlock(hive, block.Beehive{Facing: cube.South, HoneyLevel: 5}, nil)
	})
	b := w.Spawn("minecraft:bee", mgl64.Vec3{0.5, 2, 5.5}, mob.SpawnOptions{Event: "minecraft:spawn_adult"})
	w.Do(func(tx *world.Tx) {
		m, _ := b.Mob(tx)
		block.ExecuteMobEvent(tx, hive, "minecraft:bee_returned_to_hive", m)
	})
	if len(hiveAt(w, hive).Occupants()) != 1 {
		t.Fatal("bee not in the hive")
	}
	p := w.AddPlayer("harvester", mgl64.Vec3{0.5, 1, 0.5})
	p.Hold(item.NewStack(item.Shears{}, 1))
	p.With(func(pl *player.Player) {
		ctx := &item.UseContext{}
		if !hiveAt(w, hive).Activate(hive, cube.FaceSouth, pl.Tx(), pl, ctx) {
			t.Error("shears did not harvest")
		}
	})
	all := bees(w)
	if len(all) != 1 {
		t.Fatalf("%d bees out of the hive", len(all))
	}
	_, ok := w.TickUntil(100, func(tx *world.Tx) bool {
		m, ok := all[0].Mob(tx)
		if !ok {
			return false
		}
		tg, has := m.Target()
		return has && tg.H() == p.H()
	})
	if !ok {
		t.Errorf("bee not angry at the harvester:\n%v", w.Dump(all[0]))
	}
}
