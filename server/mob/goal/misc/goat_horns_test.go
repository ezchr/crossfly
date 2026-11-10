//go:build crossfly_full

package misc_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/goal/misc"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// adultGoat spawns a goat that is an adult from the start.
func adultGoat(w *mobtest.World, pos mgl64.Vec3) *mobtest.Ref {
	g := w.Spawn("minecraft:goat", pos, mob.SpawnOptions{NoEvent: true})
	g.With(func(m *mob.Mob) { m.Fire("minecraft:ageable_grow_up", nil) })
	return g
}

// TestGoatHornCount: Goat::initializeComponents gives a new adult goat two
// horns (one, 1 in 10) and a kid none, as actor data 122; a kid that grows
// up keeps none (or gets the one); the count is saved (GoatHornCount).
func TestGoatHornCount(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{Persistent: true})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 12, block.Stone{}) })
	adults, ones := 0, 0
	for i := 0; i < 40; i++ {
		w.Spawn("minecraft:goat", mgl64.Vec3{float64(i%8) - 3.5, 1, float64(i/8) - 3.5})
	}
	adult := adultGoat(w, mgl64.Vec3{-8.5, 1, -8.5})
	kid := w.Spawn("minecraft:goat", mgl64.Vec3{8.5, 1, 8.5}, mob.SpawnOptions{Event: "minecraft:entity_born"})
	w.Tick(2)
	for _, g := range w.Mobs("minecraft:goat") {
		g.With(func(m *mob.Mob) {
			v, ok := m.ActorData(protocol.EntityDataKeyGoatHornCount)
			if !ok {
				t.Fatalf("goat without actor data 122")
			}
			n := v.(int32)
			switch {
			case m.Baby():
				if n != 0 {
					t.Errorf("kid has %v horns", n)
				}
			case n == 1:
				adults++
				ones++
			case n != 2:
				t.Errorf("adult goat has %v horns", n)
			default:
				adults++
			}
		})
	}
	if adults < 30 || ones > 12 {
		t.Errorf("%v of %v adult goats have one horn, want about 1 in 10", ones, adults)
	}
	kid.With(func(m *mob.Mob) { m.Fire("minecraft:ageable_grow_up", nil) })
	w.Tick(1)
	kid.With(func(m *mob.Mob) {
		if n := misc.GoatHornCount(m); m.Baby() || n > 1 {
			t.Errorf("grown kid (baby %v) has %v horns, want 0 or 1", m.Baby(), n)
		}
	})
	adult.With(func(m *mob.Mob) { m.SetActorData(protocol.EntityDataKeyGoatHornCount, int32(1)) })
	w.Reload()
	w.Tick(2)
	adult.With(func(m *mob.Mob) {
		if n := misc.GoatHornCount(m); n != 1 {
			t.Errorf("reloaded goat has %v horns, want the saved 1", n)
		}
	})
}

// goatHorns counts the goat horn items lying around.
func goatHorns(w *mobtest.World) int {
	n := 0
	w.Do(func(tx *world.Tx) {
		for e := range tx.Entities() {
			if it, ok := e.(*entity.Ent); ok {
				if b, ok := it.Behaviour().(*entity.ItemBehaviour); ok {
					if _, ok := b.Item().Item().(item.GoatHorn); ok {
						n += b.Item().Count()
					}
				}
			}
		}
	})
	return n
}

// TestGoatLosesHornOnLog: a goat whose target steps aside while it lowers
// its head charges to where the target stood; the block past that is a log,
// so it loses a horn there (RamAttackGoalUtils::RamGoalItemDropperImpl
// tryDropHorn): a goat horn drops and the count goes down. A ram that hits
// its target drops nothing.
func TestGoatLosesHornOnLog(t *testing.T) {
	w := arena(t, 14)
	w.Do(func(tx *world.Tx) {
		w.Fill(tx, cube.Pos{-3, 1, 8}, cube.Pos{3, 2, 8}, block.Log{Wood: block.OakWood()})
	})
	p := w.AddPlayer("p", mgl64.Vec3{0.5, 1, 7.5})
	p.SetMaxHealth(200)
	g := adultGoat(w, mgl64.Vec3{0.5, 1, 0.5})
	raw := string(mobtest.Component(t, "goat.json", "ram_default", "minecraft:behavior.ram_attack"))
	raw = raw[:len(raw)-1] + `, "cooldown_range": [0, 0]}`
	swap(w, g, "ram", goal(t, "minecraft:behavior.ram_attack", raw), true)
	var before int
	g.With(func(m *mob.Mob) { before = misc.GoatHornCount(m) })
	setTarget(w, g, p)
	if _, ok := flagSeen(w, g, 80, protocol.EntityDataFlagRamAttack); !ok {
		t.Fatalf("goat never lowered its head: %v", w.Dump(g))
	}
	// Step aside while it gets ready.
	p.Teleport(mgl64.Vec3{7.5, 1, 2.5})
	if _, ok := w.TickUntil(120, func(*world.Tx) bool { return goatHorns(w) == 1 }); !ok {
		t.Fatalf("no goat horn after the ram into the logs: %v", w.Dump(g))
	}
	g.With(func(m *mob.Mob) {
		if n := misc.GoatHornCount(m); n != before-1 {
			t.Errorf("goat has %v horns after losing one, had %v", n, before)
		}
	})
	if h := p.Health(); h != 200 {
		t.Errorf("player hit for %v while out of the way", 200-h)
	}
}
