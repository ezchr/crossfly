//go:build crossfly_full

package jfarm_test

import (
	"os"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/mob/janimal"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/jfarm"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

func TestMain(m *testing.M) {
	if err := mobtest.RegisterTypes(); err != nil {
		panic(err)
	}
	javamob.Install()
	os.Exit(m.Run())
}

func spawn(t *testing.T, w *mobtest.World, id string, pos mgl64.Vec3) *mobtest.Ref {
	t.Helper()
	var ref *mobtest.Ref
	w.Do(func(tx *world.Tx) {
		m, err := javamob.Spawn(tx, id, pos, javamob.SpawnCommand, javamob.SpawnOptions{})
		if err != nil {
			t.Fatal(err)
		}
		ref = w.Track(m.H())
	})
	return ref
}

func with(t *testing.T, w *mobtest.World, r *mobtest.Ref, f func(b *janimal.Body)) {
	t.Helper()
	w.Do(func(tx *world.Tx) {
		bm, ok := r.Mob(tx)
		if !ok {
			t.Fatal("mob gone")
		}
		b, ok := janimal.BodyOf(bm)
		if !ok {
			t.Fatal("not a Java animal")
		}
		f(b)
	})
}

func count(w *mobtest.World, id string) (n, babies int) {
	w.Do(func(tx *world.Tx) {
		for e := range tx.Entities() {
			if e.H().Type().EncodeEntity() != id {
				continue
			}
			if b, ok := janimal.BodyOf(e); ok {
				n++
				if b.AnimalState().Baby() {
					babies++
				}
			}
		}
	})
	return
}

// Two cows in love 4 blocks apart breed: one calf, both parents on a 6000 tick cooldown.
func TestCowsBreed(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Grass{}) })
	a := spawn(t, w, "minecraft:cow", mgl64.Vec3{0.5, 1, 0.5})
	b := spawn(t, w, "minecraft:cow", mgl64.Vec3{4.5, 1, 0.5})
	for _, r := range []*mobtest.Ref{a, b} {
		with(t, w, r, func(b *janimal.Body) { b.AnimalState().SetInLove(uuid.New()) })
	}
	for i := 0; i < 400; i++ {
		w.Tick(1)
		if _, babies := count(w, "minecraft:cow"); babies > 0 {
			break
		}
	}
	n, babies := count(w, "minecraft:cow")
	if n != 3 || babies != 1 {
		t.Fatalf("%d cows, %d calves after breeding", n, babies)
	}
	with(t, w, a, func(b *janimal.Body) {
		if age := b.AnimalState().Age(); age < 5600 || age > 6000 || b.AnimalState().InLove() {
			t.Fatalf("parent age %d", age)
		}
	})
}

// A sheared sheep on grass eats it and grows its wool back.
func TestSheepRegrowsWool(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 6, block.Grass{}) })
	s := spawn(t, w, "minecraft:sheep", mgl64.Vec3{0.5, 1, 0.5})
	with(t, w, s, func(b *janimal.Body) { b.B.(*jfarm.Sheep).Shear(b.Mob) })
	grown := false
	for i := 0; i < 20000 && !grown; i += 20 {
		w.Tick(20)
		with(t, w, s, func(b *janimal.Body) { grown = !b.B.(*jfarm.Sheep).Sheared() })
	}
	if !grown {
		t.Fatal("wool did not grow back in 20000 ticks")
	}
	dirt := false
	w.Do(func(tx *world.Tx) {
		for x := -6; x <= 6; x++ {
			for z := -6; z <= 6; z++ {
				if _, ok := tx.Block(cube.Pos{x, 0, z}).(block.Dirt); ok {
					dirt = true
				}
			}
		}
	})
	if !dirt {
		t.Fatal("no grass block turned to dirt")
	}
}

// A chicken lays its first egg within 12000 ticks.
func TestChickenLaysEgg(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 6, block.Stone{}) })
	spawn(t, w, "minecraft:chicken", mgl64.Vec3{0.5, 1, 0.5})
	found := false
	for i := 0; i < 12000 && !found; i += 100 {
		w.Tick(100)
		w.Do(func(tx *world.Tx) {
			for e := range tx.Entities() {
				var it interface{ Item() item.Stack }
				if b, ok := e.(interface{ Behaviour() entity.Behaviour }); ok {
					it, _ = b.Behaviour().(interface{ Item() item.Stack })
				}
				if it != nil {
					if n, _ := it.Item().Item().EncodeItem(); n == "minecraft:egg" {
						found = true
					}
				}
			}
		})
	}
	if !found {
		t.Fatal("no egg in 12000 ticks")
	}
}

func items(w *mobtest.World, name string) (n int) {
	w.Do(func(tx *world.Tx) {
		for e := range tx.Entities() {
			if b, ok := e.(interface{ Behaviour() entity.Behaviour }); ok {
				if it, ok := b.Behaviour().(interface{ Item() item.Stack }); ok {
					if nm, _ := it.Item().Item().EncodeItem(); strings.HasSuffix(nm, name) {
						n += it.Item().Count()
					}
				}
			}
		}
	})
	return
}

// Shearing a sheep drops 1-3 wool of its colour; a sheared sheep cannot be dyed.
func TestSheepShearAndDye(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 4, block.Stone{}) })
	s := spawn(t, w, "minecraft:sheep", mgl64.Vec3{0.5, 1, 0.5})
	with(t, w, s, func(b *janimal.Body) {
		sh := b.B.(*jfarm.Sheep)
		held, ok := sh.MobInteract(b.Mob, nil, item.NewStack(item.Dye{Colour: item.ColourRed()}, 2))
		if !ok || held.Count() != 1 || sh.Colour() != item.ColourRed() {
			t.Fatalf("dyeing: ok=%v count=%d colour=%v", ok, held.Count(), sh.Colour())
		}
		sh.Shear(b.Mob)
		if _, ok := sh.MobInteract(b.Mob, nil, item.NewStack(item.Dye{Colour: item.ColourBlue()}, 1)); ok {
			t.Fatal("dyed a sheared sheep")
		}
		if b.Mob.Meta().Color != item.ColourRed().Uint8() || !b.Mob.Meta().Sheared {
			t.Fatal("meta not shown")
		}
	})
	w.Tick(2)
	if n := items(w, "red_wool"); n < 1 || n > 3 {
		t.Fatalf("%d wool dropped", n)
	}
}

// Shearing a mooshroom turns it into a cow (and drops 5 red mushrooms).
func TestMooshroomShear(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 4, block.Stone{}) })
	m := spawn(t, w, "minecraft:mooshroom", mgl64.Vec3{0.5, 1, 0.5})
	with(t, w, m, func(b *janimal.Body) { b.B.(*jfarm.Mooshroom).Shear(b.Mob) })
	w.Tick(2)
	if n, _ := count(w, "minecraft:cow"); n != 1 {
		t.Fatalf("%d cows after shearing", n)
	}
	if n, _ := count(w, "minecraft:mooshroom"); n != 0 {
		t.Fatal("mooshroom kept")
	}
	// Crossfly has no red/brown mushroom block yet: the 5 mushrooms drop once it does.
	if _, ok := world.ItemByName("minecraft:red_mushroom", 0); ok {
		if n := items(w, "minecraft:red_mushroom"); n != 5 {
			t.Fatalf("%d mushrooms", n)
		}
	}
}

// Pack spawns: the first member is an adult, about 5% of the rest are babies.
func TestAgeableGroupBabies(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 4, block.Stone{}) })
	babies := 0
	w.Do(func(tx *world.Tx) {
		var group any
		for i := 0; i < 400; i++ {
			m, err := javamob.Spawn(tx, "minecraft:pig", mgl64.Vec3{0.5, 1, 0.5}, javamob.SpawnNatural, javamob.SpawnOptions{GroupData: &group})
			if err != nil {
				t.Fatal(err)
			}
			if m.Baby() {
				if i == 0 {
					t.Fatal("first pack member is a baby")
				}
				babies++
			}
			_ = m.Close()
		}
	})
	if babies < 5 || babies > 40 {
		t.Fatalf("%d babies in 399 pack members, want about 20", babies)
	}
}
