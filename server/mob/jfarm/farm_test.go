//go:build crossfly_full

package jfarm

import (
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/google/uuid"
)

func rng() *rand.Rand { return rand.New(rand.NewPCG(1, 2)) }

// Eggs come every 6000 to 11999 ticks; babies and jockeys never lay.
func TestEggTimerRange(t *testing.T) {
	r := rng()
	for i := 0; i < 2000; i++ {
		e := NewEggTimer(r)
		if e < 6000 || e > 11999 {
			t.Fatalf("egg timer %d out of range", e)
		}
	}
	e := EggTimer(3)
	if e.Tick(r, true, true, false) || e.Tick(r, true, false, true) || e.Tick(r, false, false, false) || e != 3 {
		t.Fatal("baby, jockey or dead chicken counted down")
	}
	lays, ticks := 0, 0
	e = NewEggTimer(r)
	first := int(e)
	for ticks = 1; ticks <= 200000; ticks++ {
		if e.Tick(r, true, false, false) {
			lays++
			if lays == 1 && ticks != first {
				t.Fatalf("first egg after %d ticks, timer was %d", ticks, first)
			}
		}
	}
	// 200000 ticks / mean 9000 ticks = 22 eggs.
	if lays < 17 || lays > 30 {
		t.Fatalf("%d eggs in 200000 ticks", lays)
	}
	if EggItem(Warm) != "brown_egg" || EggItem(Cold) != "blue_egg" || EggItem(Temperate) != "egg" {
		t.Fatal("egg items")
	}
}

func TestWoolByte(t *testing.T) {
	var w Wool
	w = w.WithColour(item.ColourBlack()).WithSheared(true)
	if w != 0x1f || !w.Sheared() || w.Colour() != item.ColourBlack() {
		t.Fatalf("wool byte %#x", w)
	}
	w = w.WithSheared(false).WithColour(item.ColourRed())
	if w != 14 || w.Sheared() {
		t.Fatalf("wool byte %#x", w)
	}
}

func TestSheepColourMixing(t *testing.T) {
	r := rng()
	cases := []struct{ a, b, want item.Colour }{
		{item.ColourRed(), item.ColourYellow(), item.ColourOrange()},
		{item.ColourYellow(), item.ColourRed(), item.ColourOrange()},
		{item.ColourWhite(), item.ColourBlack(), item.ColourGrey()},
		{item.ColourBlue(), item.ColourGreen(), item.ColourCyan()},
		{item.ColourPink(), item.ColourPurple(), item.ColourMagenta()},
		{item.ColourWhite(), item.ColourGrey(), item.ColourLightGrey()},
	}
	for _, c := range cases {
		for i := 0; i < 20; i++ {
			if got := MixedColour(r, c.a, c.b); got != c.want {
				t.Fatalf("%v + %v = %v, want %v", c.a, c.b, got, c.want)
			}
		}
	}
	// No recipe: either parent, both happen.
	seen := map[item.Colour]int{}
	for i := 0; i < 200; i++ {
		seen[MixedColour(r, item.ColourBlack(), item.ColourBrown())]++
	}
	if len(seen) != 2 || seen[item.ColourBlack()] == 0 || seen[item.ColourBrown()] == 0 {
		t.Fatalf("unmixable colours: %v", seen)
	}
}

func TestSheepSpawnColours(t *testing.T) {
	r := rng()
	n := 200000
	count := map[item.Colour]int{}
	for i := 0; i < n; i++ {
		count[SheepSpawnColour(r, Temperate)]++
	}
	// 82% common colour (minus 1/500 pink), 5% black/grey/light grey, 3% brown, 0.164% pink.
	within := func(c item.Colour, want float64) {
		got := float64(count[c]) / float64(n)
		if got < want*0.85 || got > want*1.15 {
			t.Errorf("%v: %.4f, want %.4f", c, got, want)
		}
	}
	within(item.ColourWhite(), 0.82*499/500)
	within(item.ColourBlack(), 0.05)
	within(item.ColourGrey(), 0.05)
	within(item.ColourLightGrey(), 0.05)
	within(item.ColourBrown(), 0.03)
	within(item.ColourPink(), 0.82/500)
	if c := SheepSpawnColour(r, Warm); c != item.ColourBrown() && c != item.ColourGrey() && c != item.ColourLightGrey() && c != item.ColourWhite() && c != item.ColourBlack() && c != item.ColourPink() {
		t.Fatal(c)
	}
}

func TestClimates(t *testing.T) {
	if FarmAnimalClimate(biome.Desert{}) != Warm || FarmAnimalClimate(biome.Plains{}) != Temperate ||
		FarmAnimalClimate(biome.Taiga{}) != Cold || FarmAnimalClimate(biome.LukewarmOcean{}) != Warm {
		t.Fatal("farm animal climate")
	}
	// Frogs: taiga and lukewarm oceans are not in the frog tags.
	if FrogClimate(biome.Taiga{}) != Temperate || FrogClimate(biome.LukewarmOcean{}) != Temperate ||
		FrogClimate(biome.MangroveSwamp{}) != Warm || FrogClimate(biome.SnowyPlains{}) != Cold {
		t.Fatal("frog climate")
	}
}

func TestMooshroom(t *testing.T) {
	r := rng()
	mut := 0
	for i := 0; i < 1024*200; i++ {
		if MooshroomOffspring(r, MooshroomRed, MooshroomRed) == MooshroomBrown {
			mut++
		}
	}
	if mut < 140 || mut > 270 {
		t.Fatalf("%d mutations in 204800 red calves, want about 200", mut)
	}
	var last uuid.UUID
	bolt := uuid.New()
	v, ok := MooshroomLightning(MooshroomRed, &last, bolt)
	if !ok || v != MooshroomBrown {
		t.Fatal("lightning")
	}
	if v, ok = MooshroomLightning(v, &last, bolt); ok || v != MooshroomBrown {
		t.Fatal("same bolt converted twice")
	}
	if e, ok := StewEffectOf("oxeye_daisy"); !ok || e.Ticks != 140 {
		t.Fatal("stew effect")
	}
}

func TestPigBoost(t *testing.T) {
	r := rng()
	var s Steering
	if s.Factor() != 1 {
		t.Fatal("factor without boost")
	}
	if !s.Boost(r) || s.Boost(r) {
		t.Fatal("boost while boosting")
	}
	if s.BoostTime() < 140 || s.BoostTime() > 980 {
		t.Fatalf("boost time %d", s.BoostTime())
	}
	max := 0.0
	ticks := 0
	for s.Boosting() {
		s.Tick()
		ticks++
		if f := s.Factor(); f > max {
			max = f
		}
	}
	if ticks != int(s.BoostTime())+2 || max < 2.1 || max > 2.15+1e-6 {
		t.Fatalf("boost ran %d ticks (time %d), peak %f", ticks, s.BoostTime(), max)
	}
}
