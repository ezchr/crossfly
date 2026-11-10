//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
)

func golemBuildWorld(t *testing.T) *mobtest.World {
	t.Helper()
	needs(t, true, true)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 6, block.Stone{}) })
	return w
}

func golemInGroup(m *mob.Mob, group string) bool {
	g, ok := m.Instance().(interface{ HasGroup(string) bool })
	return ok && g.HasGroup(group)
}

func golemOnlyMob(t *testing.T, w *mobtest.World, id string) *mobtest.Ref {
	t.Helper()
	refs := w.Mobs(id)
	if len(refs) != 1 {
		t.Fatalf("%d %v in the world, want 1", len(refs), id)
	}
	return refs[0]
}

func TestDispensedPumpkinBuildsPlayerIronGolem(t *testing.T) {
	w := golemBuildWorld(t)
	w.Do(func(tx *world.Tx) {
		for _, p := range []cube.Pos{{0, 1, 0}, {0, 2, 0}, {1, 2, 0}, {-1, 2, 0}} {
			tx.SetBlock(p, block.Iron{}, nil)
		}
		if r := (block.Pumpkin{Carved: true}).Dispense(cube.Pos{0, 3, 1}, cube.FaceNorth, tx, &item.DispenseContext{}); r != item.DispenseSuccess {
			t.Fatalf("dispense = %v", r)
		}
		for _, p := range []cube.Pos{{0, 1, 0}, {0, 2, 0}, {1, 2, 0}, {-1, 2, 0}, {0, 3, 0}} {
			if _, ok := tx.Block(p).(block.Air); !ok {
				t.Errorf("%v holds %T after the golem was built", p, tx.Block(p))
			}
		}
	})
	w.Tick(1)
	golemOnlyMob(t, w, "minecraft:iron_golem").With(func(m *mob.Mob) {
		if !golemInGroup(m, "minecraft:player_created") || golemInGroup(m, "minecraft:village_created") {
			t.Fatal("iron golem built from blocks is not player created")
		}
	})
}

func TestDispensedJackOLanternBuildsSnowGolem(t *testing.T) {
	w := golemBuildWorld(t)
	w.Do(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 1, 0}, block.Snow{}, nil)
		tx.SetBlock(cube.Pos{0, 2, 0}, block.Snow{}, nil)
		(block.LitPumpkin{}).Dispense(cube.Pos{0, 3, 1}, cube.FaceNorth, tx, &item.DispenseContext{})
	})
	w.Tick(1)
	golemOnlyMob(t, w, "minecraft:snow_golem")
}

func TestCopperGolemBuiltWithCopperOxidation(t *testing.T) {
	w := golemBuildWorld(t)
	w.Do(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 1, 0}, block.Copper{Type: block.NormalCopper(), Oxidation: block.WeatheredOxidation(), Waxed: true}, nil)
		(block.Pumpkin{Carved: true}).Dispense(cube.Pos{0, 2, 1}, cube.FaceNorth, tx, &item.DispenseContext{})
	})
	w.Tick(1)
	golemOnlyMob(t, w, "minecraft:copper_golem").With(func(m *mob.Mob) {
		if v, _ := m.Property("minecraft:oxidation_level"); v != "weathered" {
			t.Fatalf("copper golem oxidation %v, want weathered", v)
		}
		if !golemInGroup(m, "minecraft:copper_oxidizing") {
			t.Fatal("built copper golem does not oxidise")
		}
	})
}
