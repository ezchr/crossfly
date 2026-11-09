//go:build crossfly_full

package mobtest_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// breedMany puts two adults in love and mates them n times
// (BreedableComponent::mate), calling check on every baby, which is removed
// afterwards.
func breedMany(t *testing.T, w *mobtest.World, a, b *mobtest.Ref, n int, check func(baby *mob.Mob)) {
	t.Helper()
	w.Do(func(tx *world.Tx) {
		ma, _ := a.Mob(tx)
		mb, _ := b.Mob(tx)
		ba, ok := core.BreedableOf(ma)
		bb, ok2 := core.BreedableOf(mb)
		if !ok || !ok2 {
			t.Fatalf("parents not breedable:\n%v\n%v", w.Dump(a), w.Dump(b))
		}
		for range n {
			ba.SetInLove(ma, nil)
			bb.SetInLove(mb, nil)
			babies := ba.Mate(ma, mb)
			if len(babies) == 0 {
				t.Fatal("no baby")
			}
			for _, baby := range babies {
				check(baby)
				_ = tx.RemoveEntity(baby)
			}
		}
	})
}

func breedFloor(w *mobtest.World) {
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
}

// TestBredAxolotlVariant: babies take a parent's variant; only the
// mutation_factor 0.00083 leaves a baby the blue its born event gives it.
func TestBredAxolotlVariant(t *testing.T) {
	needs(t, true, true, "minecraft:breedable", "minecraft:offspring")
	w := mobtest.New(t, mobtest.Options{})
	breedFloor(w)
	a := w.Spawn("minecraft:axolotl", mgl64.Vec3{0.5, 1, 0.5})
	b := w.Spawn("minecraft:axolotl", mgl64.Vec3{2.5, 1, 0.5})
	a.With(func(m *mob.Mob) { core.SetVariant(m, 2) }) // gold
	b.With(func(m *mob.Mob) { core.SetVariant(m, 2) })
	gold, blue := 0, 0
	breedMany(t, w, a, b, 300, func(baby *mob.Mob) {
		switch baby.Meta().Variant {
		case 2:
			gold++
		case 4:
			blue++
		default:
			t.Errorf("baby of two gold axolotls has variant %v", baby.Meta().Variant)
		}
	})
	if gold < 295 {
		t.Errorf("%d of 300 babies gold, %d blue: want nearly all gold", gold, blue)
	}
}

// TestBredHorseInheritance: foals take a parent's coat and markings with
// the mutation chances of horse.json, and parent-centric blended health,
// speed and jump within the horse ranges.
func TestBredHorseInheritance(t *testing.T) {
	needs(t, true, true, "minecraft:breedable", "minecraft:offspring")
	w := mobtest.New(t, mobtest.Options{})
	breedFloor(w)
	a := w.Spawn("minecraft:horse", mgl64.Vec3{0.5, 1, 0.5})
	b := w.Spawn("minecraft:horse", mgl64.Vec3{2.5, 1, 0.5})
	for _, r := range []*mobtest.Ref{a, b} {
		r.With(func(m *mob.Mob) {
			m.Fire("minecraft:on_tame", nil)
			core.SetVariant(m, 3)
			core.SetMarkVariant(m, 2)
			m.SetMaxHealth(28)
			m.SetHealth(28)
			m.Attributes().SetBase(mob.AttrMovement, 0.3)
			m.Attributes().SetBase(mob.AttrJumpStrength, 0.9)
		})
	}
	const n = 400
	coat, marks := 0, 0
	var health, speed, jump float64
	breedMany(t, w, a, b, n, func(baby *mob.Mob) {
		if baby.Meta().Variant == 3 {
			coat++
		}
		if baby.Meta().MarkVariant == 2 {
			marks++
		}
		h, s, j := baby.MaxHealth(), baby.Attr(mob.AttrMovement), baby.Attr(mob.AttrJumpStrength)
		if h < 15 || h > 30 || s < 0.1125 || s > 0.3375 || j < 0.4 || j > 1 {
			t.Errorf("foal out of range: health %v speed %v jump %v", h, s, j)
		}
		health += h
		speed += s
		jump += j
	})
	// variant mutates at 0.111, markings at 0.2 (random strategy: a mutated
	// roll can still hit the parents' value).
	if f := float64(coat) / n; f < 0.8 || f > 0.97 {
		t.Errorf("%.2f of foals have the parents' coat, want about 0.9", f)
	}
	if f := float64(marks) / n; f < 0.7 || f > 0.9 {
		t.Errorf("%.2f of foals have the parents' markings, want about 0.83", f)
	}
	// Two fast parents make fast foals (centred on the parents, not the range).
	if m := health / n; math.Abs(m-27.5) > 1.5 {
		t.Errorf("mean foal health %.2f, want about 28 (the parents')", m)
	}
	if m := speed / n; math.Abs(m-0.3) > 0.02 {
		t.Errorf("mean foal speed %.3f, want about 0.3", m)
	}
	if m := jump / n; math.Abs(m-0.9) > 0.04 {
		t.Errorf("mean foal jump %.3f, want about 0.9", m)
	}
}

// TestBredLlamaStrength: a cria gets a parent's strength (+1 at 3%) and so
// its chest slots; strength and the blended values survive a reload.
func TestBredLlamaStrength(t *testing.T) {
	needs(t, true, true, "minecraft:breedable", "minecraft:offspring", "minecraft:strength")
	w := mobtest.New(t, mobtest.Options{Persistent: true})
	breedFloor(w)
	adult := mob.SpawnOptions{Event: "minecraft:spawn_adult"}
	a := w.Spawn("minecraft:llama", mgl64.Vec3{0.5, 1, 0.5}, adult)
	b := w.Spawn("minecraft:llama", mgl64.Vec3{2.5, 1, 0.5}, adult)
	a.With(func(m *mob.Mob) { core.SetStrengthMax(m, 5); core.SetStrength(m, 3) })
	b.With(func(m *mob.Mob) { core.SetStrengthMax(m, 5); core.SetStrength(m, 5) })
	seen := map[int]int{}
	breedMany(t, w, a, b, 200, func(baby *mob.Mob) {
		if core.StrengthMax(baby) != 5 {
			t.Errorf("cria strength max %d, want 5", core.StrengthMax(baby))
		}
		seen[core.Strength(baby)]++
	})
	if seen[3] == 0 || seen[5] == 0 || seen[3]+seen[4]+seen[5] != 200 {
		t.Errorf("cria strengths %v, want 3 or 5 (4 at 3%%)", seen)
	}
	// One cria kept for the reload.
	var cria *mobtest.Ref
	w.Do(func(tx *world.Tx) {
		ma, _ := a.Mob(tx)
		mb, _ := b.Mob(tx)
		ba, _ := core.BreedableOf(ma)
		bb, _ := core.BreedableOf(mb)
		ba.SetInLove(ma, nil)
		bb.SetInLove(mb, nil)
		baby := ba.Mate(ma, mb)[0]
		baby.SetPersistent(true)
		core.SetStrength(baby, 4)
		baby.Attributes().SetBase(mob.AttrMovement, 0.123)
		baby.SetMaxHealth(21)
		baby.SetHealth(21)
		cria = w.Track(baby.H())
	})
	w.Reload()
	w.Tick(1)
	cria.With(func(m *mob.Mob) {
		if core.Strength(m) != 4 || core.StrengthMax(m) != 5 {
			t.Errorf("after reload strength %d/%d, want 4/5", core.Strength(m), core.StrengthMax(m))
		}
		if v := m.Attr(mob.AttrMovement); math.Abs(v-0.123) > 1e-6 {
			t.Errorf("after reload speed %v, want 0.123", v)
		}
		if m.MaxHealth() != 21 || m.Health() != 21 {
			t.Errorf("after reload health %v/%v, want 21/21", m.Health(), m.MaxHealth())
		}
	})
}

// TestPandaNeedsBamboo: pandas take bamboo only with 8 bamboo blocks within
// 5 (environment_requirements); without them the bamboo stays in the hand.
func TestPandaNeedsBamboo(t *testing.T) {
	needs(t, true, true, "minecraft:breedable")
	w := mobtest.New(t, mobtest.Options{})
	breedFloor(w)
	p := w.AddPlayer("keeper", mgl64.Vec3{3.5, 1, 3.5})
	a := w.Spawn("minecraft:panda", mgl64.Vec3{0.5, 1, 0.5})
	a.With(func(m *mob.Mob) {
		if core.IsBaby(m) {
			m.Fire("minecraft:ageable_grow_up", nil)
		}
	})
	bamboo, ok := core.ParseItemDescriptor("bamboo").Stack(1)
	if !ok {
		t.Skip("no bamboo item")
	}
	inLove := func() (in bool) {
		a.With(func(m *mob.Mob) { in = core.InLove(m) })
		return
	}
	if _, ok := p.Interact(a, bamboo); ok && inLove() {
		t.Fatal("panda fell in love without bamboo around")
	}
	w.Do(func(tx *world.Tx) {
		for x := -3; x < 5; x++ {
			tx.SetBlock(cube.Pos{x, 1, -3}, block.Bamboo{}, nil)
		}
	})
	if _, ok := p.Interact(a, bamboo); !ok || !inLove() {
		t.Fatalf("panda with 8 bamboo around did not fall in love:\n%v", w.Dump(a))
	}
}

// TestBredFoxTrustsBreeder: a cub trusts the players who fed its parents
// (TrustComponent::assignTrustedPlayer).
func TestBredFoxTrustsBreeder(t *testing.T) {
	needs(t, true, true, "minecraft:breedable", "minecraft:trust")
	w := mobtest.New(t, mobtest.Options{})
	breedFloor(w)
	p := w.AddPlayer("keeper", mgl64.Vec3{4.5, 1, 4.5})
	a := w.Spawn("minecraft:fox", mgl64.Vec3{0.5, 1, 0.5})
	b := w.Spawn("minecraft:fox", mgl64.Vec3{2.5, 1, 0.5})
	for _, r := range []*mobtest.Ref{a, b} {
		r.With(func(m *mob.Mob) {
			if core.IsBaby(m) {
				m.Fire("minecraft:ageable_grow_up", nil) // 5% spawn as cubs
			}
		})
	}
	w.Do(func(tx *world.Tx) {
		ma, _ := a.Mob(tx)
		mb, _ := b.Mob(tx)
		pl, _ := p.Entity(tx)
		ba, ok := core.BreedableOf(ma)
		bb, ok2 := core.BreedableOf(mb)
		if !ok || !ok2 {
			t.Fatalf("foxes not breedable: %v %v", w.Dump(a), w.Dump(b))
		}
		ba.SetInLove(ma, pl)
		bb.SetInLove(mb, nil)
		babies := ba.Mate(ma, mb)
		if len(babies) == 0 {
			t.Fatal("no cub")
		}
		tr, ok := mob.ComponentOf[interface{ Trusts(world.Entity) bool }](babies[0])
		if !ok {
			t.Fatalf("cub has no minecraft:trust")
		}
		if !tr.Trusts(pl) {
			t.Error("cub does not trust the player who fed its parent")
		}
	})
}
