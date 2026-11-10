package jraid

import (
	"fmt"
	"testing"
)

// TestNumGroups checks Raid.getNumGroups.
func TestNumGroups(t *testing.T) {
	for d, want := range map[Difficulty]int{Peaceful: 0, Easy: 3, Normal: 5, Hard: 7} {
		if got := NumGroups(d); got != want {
			t.Errorf("NumGroups(%v) = %d, want %d", d, got, want)
		}
	}
}

// bounds is the least and most raiders of a kind in a wave.
type bounds [5][2]int

// TestCompositionPerDifficulty checks every wave of every difficulty, bonus waves included, against the counts of
// Raid.RaiderType.spawnsPerWaveBeforeBonus plus Raid.getPotentialBonusSpawns, over many seeds: each count stays in
// its range and reaches both ends.
func TestCompositionPerDifficulty(t *testing.T) {
	type key struct {
		d     Difficulty
		wave  int
		bonus bool
	}
	// Expected [min, max] per kind (Vindicator, Evoker, Pillager, Witch, Ravager).
	want := map[key]bounds{
		// Easy: pillagers/vindicators +0..1, no extra witches or ravagers.
		{Easy, 1, false}: {{0, 1}, {0, 0}, {4, 5}, {0, 0}, {0, 0}},
		{Easy, 2, false}: {{2, 3}, {0, 0}, {3, 4}, {0, 0}, {0, 0}},
		{Easy, 3, false}: {{0, 1}, {0, 0}, {3, 4}, {0, 0}, {1, 1}},
		{Easy, 4, true}:  {{0, 1}, {0, 0}, {3, 4}, {0, 0}, {1, 1}},
		// Normal: +0..1 pillagers/vindicators, a witch from wave 3 except wave 4, a ravager on the bonus wave.
		{Normal, 1, false}: {{0, 1}, {0, 0}, {4, 5}, {0, 0}, {0, 0}},
		{Normal, 2, false}: {{2, 3}, {0, 0}, {3, 4}, {0, 0}, {0, 0}},
		{Normal, 3, false}: {{0, 1}, {0, 0}, {3, 4}, {0, 1}, {1, 1}},
		{Normal, 4, false}: {{1, 2}, {0, 0}, {4, 5}, {3, 3}, {0, 0}},
		{Normal, 5, false}: {{4, 5}, {1, 1}, {4, 5}, {0, 1}, {1, 1}},
		{Normal, 6, true}:  {{4, 5}, {1, 1}, {4, 5}, {0, 1}, {1, 2}},
		// Hard: +0..2 pillagers/vindicators.
		{Hard, 1, false}: {{0, 2}, {0, 0}, {4, 6}, {0, 0}, {0, 0}},
		{Hard, 2, false}: {{2, 4}, {0, 0}, {3, 5}, {0, 0}, {0, 0}},
		{Hard, 3, false}: {{0, 2}, {0, 0}, {3, 5}, {0, 1}, {1, 1}},
		{Hard, 4, false}: {{1, 3}, {0, 0}, {4, 6}, {3, 3}, {0, 0}},
		{Hard, 5, false}: {{4, 6}, {1, 1}, {4, 6}, {0, 1}, {1, 1}},
		{Hard, 6, false}: {{2, 4}, {1, 1}, {4, 6}, {0, 1}, {0, 0}},
		{Hard, 7, false}: {{5, 7}, {2, 2}, {2, 4}, {1, 2}, {2, 2}},
		{Hard, 8, true}:  {{5, 7}, {2, 2}, {2, 4}, {1, 2}, {2, 3}},
	}
	r := newRand(1)
	for k, b := range want {
		t.Run(fmt.Sprintf("%v/wave%d/bonus=%v", k.d, k.wave, k.bonus), func(t *testing.T) {
			var seen bounds
			for i := range seen {
				seen[i] = [2]int{1 << 30, -1}
			}
			for n := 0; n < 2000; n++ {
				var count [5]int
				for _, m := range Composition(r, k.d, k.wave, NumGroups(k.d), k.bonus) {
					count[m.Kind]++
				}
				for kind, c := range count {
					seen[kind][0], seen[kind][1] = min(seen[kind][0], c), max(seen[kind][1], c)
				}
			}
			for kind := range b {
				if seen[kind] != b[kind] {
					t.Errorf("%v: counts %v, want %v", Kind(kind), seen[kind], b[kind])
				}
			}
		})
	}
}

// TestCompositionLeaderAndRiders checks the captain (the first vindicator, evoker or pillager in spawn order) and
// the ravager riders: a pillager on wave 5, an evoker on the first ravager and vindicators on the others from wave
// 7 on, none otherwise.
func TestCompositionLeaderAndRiders(t *testing.T) {
	r := newRand(2)
	for _, d := range []Difficulty{Easy, Normal, Hard} {
		for wave := 1; wave <= NumGroups(d)+1; wave++ {
			bonus := wave > NumGroups(d)
			for n := 0; n < 200; n++ {
				ms := Composition(r, d, wave, NumGroups(d), bonus)
				leaders, ravagers := 0, 0
				for i, m := range ms {
					if m.Leader {
						leaders++
						for _, prev := range ms[:i] {
							if prev.Kind.CanBeLeader() {
								t.Fatalf("%v wave %d: leader %v after %v", d, wave, m.Kind, prev.Kind)
							}
						}
						if !m.Kind.CanBeLeader() {
							t.Fatalf("%v wave %d: %v leads", d, wave, m.Kind)
						}
					}
					if m.Kind != Ravager {
						if m.Rider != NoKind {
							t.Fatalf("%v wave %d: %v has a rider", d, wave, m.Kind)
						}
						continue
					}
					want := NoKind
					switch {
					case wave == 5:
						want = Pillager
					case wave >= 7 && ravagers == 0:
						want = Evoker
					case wave >= 7:
						want = Vindicator
					}
					if m.Rider != want {
						t.Fatalf("%v wave %d ravager %d: rider %v, want %v", d, wave, ravagers, m.Rider, want)
					}
					ravagers++
				}
				if leaders != 1 {
					t.Fatalf("%v wave %d: %d leaders", d, wave, leaders)
				}
			}
		}
	}
}

// TestEnchantOdds checks Raid.getEnchantOdds.
func TestEnchantOdds(t *testing.T) {
	want := []float32{0, 0, 0.1, 0.25, 0.5, 0.75}
	for lvl, w := range want {
		if got := EnchantOdds(lvl); got != w {
			t.Errorf("EnchantOdds(%d) = %v, want %v", lvl, got, w)
		}
	}
}

// TestDifficultyInstance checks DifficultyInstance.calculateDifficulty at a few points.
func TestDifficultyInstance(t *testing.T) {
	cases := []struct {
		d          Difficulty
		total, loc int64
		moon       float32
		want       float32
	}{
		{Peaceful, 1e9, 1e9, 1, 0},
		{Easy, 0, 0, 0, 0.75},
		{Normal, 0, 0, 0, 1.5},
		{Hard, 0, 0, 0, 2.25},
		// Global scale full (0.25), local full: hard 1 + moon min(0.25, 0.25).
		{Hard, 72000 + 1440000, 3600000, 1, 3 * (0.75 + 0.25 + 1 + 0.25)},
		{Normal, 72000 + 1440000, 3600000, 1, 2 * (0.75 + 0.25 + 0.75 + 0.25)},
		{Easy, 72000 + 1440000, 3600000, 1, 1 * (0.75 + 0.25 + (0.75+0.25)*0.5)},
	}
	for _, c := range cases {
		if got := NewDifficultyInstance(c.d, c.total, c.loc, c.moon).Effective; got != c.want {
			t.Errorf("%v %d %d %v: %v, want %v", c.d, c.total, c.loc, c.moon, got, c.want)
		}
	}
}

func (d Difficulty) String() string {
	return [...]string{"peaceful", "easy", "normal", "hard"}[d]
}
