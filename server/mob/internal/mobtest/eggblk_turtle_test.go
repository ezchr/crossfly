//go:build crossfly_full

package mobtest_test

import (
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// The egg blocks only register with the mobs.
func init() {
	block.RegisterMobBlocks()
	item.RegisterMobItems()
}

// eggHatchWindow is a world time whose time of day (0.676) is in the 0.65-0.69 window turtle eggs move in.
const eggHatchWindow = 21600

// eggSand builds a sand beach of radius r at y 0 on stone (sand falls).
func eggSand(w *mobtest.World, tx *world.Tx, r int) {
	w.Floor(tx, -1, r, block.Stone{})
	w.Floor(tx, 0, r, block.Sand{})
}

// TestTurtleLaysEggs: a turtle that wants to lay eggs (minecraft:go_lay_egg) walks onto sand and lays 1-4
// turtle eggs above it, which start their hatching ticks; it then stops wanting to lay.
func TestTurtleLaysEggs(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { eggSand(w, tx, 8) })
	tu := w.Spawn("minecraft:turtle", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:ageable_grow_up"})
	tu.With(func(m *mob.Mob) { m.Fire("minecraft:go_lay_egg", nil) })
	var egg block.TurtleEgg
	var at cube.Pos
	_, ok := w.TickUntil(600, func(tx *world.Tx) bool {
		for p := range cube.Range3D(cube.Pos{-8, 1, -8}, cube.Pos{8, 1, 8}) {
			if e, ok := tx.Block(p).(block.TurtleEgg); ok {
				egg, at = e, p
				return true
			}
		}
		return false
	})
	if !ok {
		t.Fatalf("the turtle laid no eggs: %v", w.Dump(tu))
	}
	if egg.Cracks != 0 || egg.AdditionalCount < 0 || egg.AdditionalCount > 3 {
		t.Errorf("laid %+v", egg)
	}
	w.Do(func(tx *world.Tx) {
		if !tx.BlockUpdateScheduled(at, egg) {
			t.Error("the laid eggs have no hatching tick")
		}
	})
	w.Tick(2)
	tu.With(func(m *mob.Mob) {
		if m.HasComponent("minecraft:behavior.lay_egg") {
			t.Error("the turtle still wants to lay eggs after laying")
		}
	})
}

// TestTurtleEggCounts: turtles lay a random count of 1-4 eggs (LayEggGoal::_layEgg), every count turning up.
func TestTurtleEggCounts(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { eggSand(w, tx, 6) })
	seen := map[int]bool{}
	for i := 0; i < 24 && len(seen) < 4; i++ {
		tu := w.Spawn("minecraft:turtle", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:ageable_grow_up"})
		tu.With(func(m *mob.Mob) { m.Fire("minecraft:go_lay_egg", nil) })
		if _, ok := w.TickUntil(600, func(tx *world.Tx) bool {
			for p := range cube.Range3D(cube.Pos{-6, 1, -6}, cube.Pos{6, 1, 6}) {
				if e, ok := tx.Block(p).(block.TurtleEgg); ok {
					seen[e.AdditionalCount] = true
					tx.SetBlock(p, nil, nil)
					return true
				}
			}
			return false
		}); !ok {
			t.Fatalf("turtle %d laid no eggs", i)
		}
		w.Do(func(tx *world.Tx) {
			if e, ok := tu.Entity(tx); ok {
				_ = e.Close()
			}
		})
	}
	if len(seen) != 4 {
		t.Errorf("egg counts laid: %v, want all of 1-4", seen)
	}
}

// TestTurtleEggsHatch: max cracked turtle eggs on sand in the hatch window become one persistent baby turtle
// per egg, at home on the eggs' block, and the block goes.
func TestTurtleEggsHatch(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{Time: eggHatchWindow})
	pos := cube.Pos{0, 1, 0}
	w.Do(func(tx *world.Tx) {
		eggSand(w, tx, 3)
		e := block.TurtleEgg{AdditionalCount: 2, Cracks: 2}
		tx.SetBlock(pos, e, nil)
		e.ScheduledTick(pos, tx, rand.New(rand.NewPCG(1, 2)))
		if _, ok := tx.Block(pos).(block.Air); !ok {
			t.Errorf("hatched eggs left %v", tx.Block(pos))
		}
	})
	babies := w.Mobs("minecraft:turtle")
	if len(babies) != 3 {
		t.Fatalf("%d turtles hatched from three eggs", len(babies))
	}
	for _, b := range babies {
		b.With(func(m *mob.Mob) {
			if !m.Baby() || !m.Persistent() {
				t.Errorf("hatched turtle baby=%v persistent=%v", m.Baby(), m.Persistent())
			}
			if h, ok := m.Home(); !ok || h != pos {
				t.Errorf("hatched turtle home %v (%v), want %v", h, ok, pos)
			}
		})
	}
}

// eggCount returns the eggs left in the turtle egg blocks of the box a-b.
func eggCount(tx *world.Tx, a, b cube.Pos) int {
	n := 0
	for p := range cube.Range3D(a, b) {
		if e, ok := tx.Block(p).(block.TurtleEgg); ok {
			n += e.AdditionalCount + 1
		}
	}
	return n
}

// TestTurtleEggTrampling: a cow walking over turtle eggs tramples them (1 in 100 a tick in the middle of the
// eggs); turtles do not, nor do mobs as small as a chicken.
func TestTurtleEggTrampling(t *testing.T) {
	mobtest.RequireDefinitions(t)
	for _, c := range []struct {
		id    string
		event string
		want  bool
	}{
		{"minecraft:cow", "minecraft:ageable_grow_up", true},
		{"minecraft:turtle", "minecraft:ageable_grow_up", false},
		{"minecraft:chicken", "minecraft:ageable_grow_up", false},
	} {
		t.Run(c.id, func(t *testing.T) {
			w := mobtest.New(t, mobtest.Options{})
			a, b := cube.Pos{-2, 1, -2}, cube.Pos{2, 1, 2}
			w.Do(func(tx *world.Tx) {
				w.Arena(tx, 3, 3)
				w.Fill(tx, a, b, block.TurtleEgg{AdditionalCount: 3})
			})
			w.Spawn(c.id, mgl64.Vec3{0.5, 1.45, 0.5}, mob.SpawnOptions{Event: c.event})
			_, trampled := w.TickUntil(1500, func(tx *world.Tx) bool { return eggCount(tx, a, b) < 100 })
			if trampled != c.want {
				t.Errorf("%v trampled eggs: %v, want %v", c.id, trampled, c.want)
			}
		})
	}
}
