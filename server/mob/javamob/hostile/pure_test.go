//go:build crossfly_full

package hostile

import (
	"math"
	"math/rand/v2"
	"testing"
)

func newRand() *rand.Rand { return rand.New(rand.NewPCG(1, 2)) }

// TestCubeSpawnSize: AbstractCubeMob.setSpawnSize gives 1, 2 or 4; with no
// special multiplier each third, with a full one size 1 never stays.
func TestCubeSpawnSize(t *testing.T) {
	r := newRand()
	counts := map[int]int{}
	for i := 0; i < 30000; i++ {
		counts[cubeSpawnSize(r, 0)]++
	}
	for _, s := range []int{1, 2, 4} {
		if c := counts[s]; c < 9000 || c > 11000 {
			t.Errorf("size %d: %d of 30000, want about a third", s, c)
		}
	}
	if len(counts) != 3 {
		t.Errorf("sizes %v", counts)
	}
	counts = map[int]int{}
	for i := 0; i < 30000; i++ {
		counts[cubeSpawnSize(r, 1)]++
	}
	// scale 0 -> 1 with 50%, scale 1 -> 2 with 50%: P(1)=1/6, P(2)=1/3, P(4)=1/2.
	if c := counts[1]; c < 4500 || c > 5500 {
		t.Errorf("size 1 at multiplier 1: %d, want about 5000", c)
	}
	if c := counts[4]; c < 14000 || c > 16000 {
		t.Errorf("size 4 at multiplier 1: %d, want about 15000", c)
	}
}

// TestCubeSplits: a dead size-4 slime leaves 2..4 size-2 slimes on the
// corners of a square half its width wide, 0.5 up; a size-1 none.
func TestCubeSplits(t *testing.T) {
	r := newRand()
	if s := cubeSplits(r, 1, 0.52, 4); s != nil {
		t.Fatalf("size 1 splits into %v", s)
	}
	width := 0.52 * 4
	s := cubeSplits(r, 4, width, 4)
	if len(s) != 4 {
		t.Fatalf("%d children", len(s))
	}
	off := width / 4
	want := [][2]float64{{-off, -off}, {off, -off}, {-off, off}, {off, off}}
	for i, c := range s {
		if c.size != 2 {
			t.Errorf("child %d size %d", i, c.size)
		}
		if math.Abs(c.offset[0]-want[i][0]) > 1e-9 || math.Abs(c.offset[2]-want[i][1]) > 1e-9 || c.offset[1] != 0.5 {
			t.Errorf("child %d offset %v, want x %v z %v y 0.5", i, c.offset, want[i][0], want[i][1])
		}
	}
	for i := 0; i < 1000; i++ {
		if n := cubeSplitCount(r); n < 2 || n > 4 {
			t.Fatalf("split count %d", n)
		}
	}
	// Size 3 (from NBT) halves down to 1.
	if s := cubeSplits(r, 3, 0.52*3, 2); s[0].size != 1 {
		t.Errorf("size 3 child size %d", s[0].size)
	}
}

// TestCubeMoveJumpDelay: CubeMobMoveControl's delay is 10..29 ticks, x4 for
// magma cubes, and a third of it while aggressive.
func TestCubeJumpDelay(t *testing.T) {
	r := newRand()
	slime := &Cube{kind: slimeKind, size: 2}
	magma := &Cube{kind: &CubeKind{JumpDelayFactor: 4}, size: 2}
	for i := 0; i < 2000; i++ {
		if d := slime.jumpDelay(r); d < 10 || d > 29 {
			t.Fatalf("slime delay %d", d)
		}
		if d := magma.jumpDelay(r); d < 40 || d > 116 || d%4 != 0 {
			t.Fatalf("magma delay %d", d)
		}
	}
	tiny := &Cube{kind: slimeKind, size: 1}
	if tiny.canDealDamage() || !slime.canDealDamage() {
		t.Errorf("only slimes bigger than 1 hurt: tiny %v, size 2 %v", tiny.canDealDamage(), slime.canDealDamage())
	}
	if !(&Cube{kind: &CubeKind{AlwaysDealsDamage: true}, size: 1}).canDealDamage() {
		t.Errorf("a tiny magma cube hurts")
	}
	if v := (&Cube{kind: slimeKind, size: 4}).soundVolume(); math.Abs(v-1.6) > 1e-9 {
		t.Errorf("volume %v", v)
	}
}

// TestWaterAnimalAir: a fish on land runs out of air after 320 ticks and is
// hurt every 20 ticks after that; water refills it.
func TestWaterAnimalAir(t *testing.T) {
	air := 300
	first := -1
	var hurts []int
	for tick := 1; tick <= 400; tick++ {
		if waterAnimalAir(&air, false) {
			if first < 0 {
				first = tick
			}
			hurts = append(hurts, tick)
		}
	}
	if first != 320 {
		t.Errorf("first drowning hurt at tick %d, want 320", first)
	}
	if len(hurts) < 2 || hurts[1]-hurts[0] != 20 {
		t.Errorf("hurts at %v, want every 20 ticks", hurts)
	}
	if waterAnimalAir(&air, true); air != 300 {
		t.Errorf("air in water %d", air)
	}
}

// TestPufferStates: Pufferfish.tick puffs to mid at once, full after 40
// ticks of fear, then back to mid 60 ticks and small 100 ticks later.
func TestPufferStates(t *testing.T) {
	f := &Fish{}
	f.inflateCounter = 1
	var states []int
	for i := 0; i < 50; i++ {
		f.puffState = f.pufferStep()
		states = append(states, f.puffState)
	}
	if states[0] != PuffMid {
		t.Fatalf("first state %d", states[0])
	}
	full := -1
	for i, s := range states {
		if s == PuffFull {
			full = i
			break
		}
	}
	// inflateCounter goes 1, 2, ...; full when it is > 40 at the start of a tick.
	if full != 40 {
		t.Errorf("full at step %d, want 40", full)
	}
	f.inflateCounter, f.deflateTimer = 0, 0
	toMid, toSmall := -1, -1
	for i := 0; i < 200; i++ {
		before := f.puffState
		f.puffState = f.pufferStep()
		if before == PuffFull && f.puffState == PuffMid {
			toMid = i
		}
		if before == PuffMid && f.puffState == PuffSmall {
			toSmall = i
		}
	}
	if toMid != 61 || toSmall != 101 {
		t.Errorf("deflate to mid at %d (want 61), to small at %d (want 101)", toMid, toSmall)
	}
	for s, want := range map[int]float64{0: 0.5, 1: 0.7, 2: 1} {
		if pufferScale(s) != want {
			t.Errorf("scale %d = %v", s, pufferScale(s))
		}
	}
}

// TestTropicalVariants: the packed id matches Java's layout and the 22 common
// variants are as in TropicalFish.COMMON_VARIANTS.
func TestTropicalVariants(t *testing.T) {
	if len(CommonTropicalVariants) != 22 {
		t.Fatalf("%d common variants", len(CommonTropicalVariants))
	}
	// COMMON_VARIANTS[0]: STRIPEY (large, index 1), orange, gray.
	if got, want := CommonTropicalVariants[0].Packed(), int32(1|1<<8|1<<16|7<<24); got != want {
		t.Errorf("stripey orange/gray packed %#x, want %#x", got, want)
	}
	// Clownfish: KOB orange/white = small, index 0, 1, 0.
	if got := CommonTropicalVariants[5].Packed(); got != 1<<16 {
		t.Errorf("clownfish packed %#x", got)
	}
	for _, v := range CommonTropicalVariants {
		if back := unpackTropicalVariant(v.Packed()); back != v {
			t.Errorf("round trip %v -> %v", v, back)
		}
	}
	if v := unpackTropicalVariant(0x7f | 9<<8); v.Pattern != (TropicalPattern{}) {
		t.Errorf("unknown pattern unpacks to %v, want kob", v.Pattern)
	}
	r := newRand()
	school := 0
	for i := 0; i < 10000; i++ {
		if _, s := randomTropicalVariant(r); s {
			school++
		}
	}
	if school < 8800 || school > 9200 {
		t.Errorf("%d of 10000 common, want about 9000", school)
	}
	if tropicalPatternByName("clayfish") != (TropicalPattern{Base: 1, Index: 5}) || tropicalPattern(11).Name() != "clayfish" {
		t.Errorf("clayfish pattern")
	}
}

// TestSalmonSizes: weights 30/50/15 and the scales.
func TestSalmonSizes(t *testing.T) {
	r := newRand()
	c := map[SalmonSize]int{}
	for i := 0; i < 95000; i++ {
		c[randomSalmonSize(r)]++
	}
	for s, w := range map[SalmonSize]int{SalmonSmall: 30000, SalmonMedium: 50000, SalmonLarge: 15000} {
		if math.Abs(float64(c[s]-w)) > 1500 {
			t.Errorf("%v: %d, want about %d", s.Name(), c[s], w)
		}
	}
	if SalmonLarge.Scale() != 1.5 || salmonSizeByName("x") != SalmonMedium || salmonSizeByName("small") != SalmonSmall {
		t.Errorf("salmon size table")
	}
}

// TestLocalDifficulty: DifficultyInstance values at known points.
func TestLocalDifficulty(t *testing.T) {
	if d := newLocalDifficulty(difficultyPeaceful, 1e7, 1e7, 1); d.effective != 0 {
		t.Errorf("peaceful %v", d.effective)
	}
	// Normal, new world, new chunk, full moon: 2 x 0.75 = 1.5.
	if d := newLocalDifficulty(difficultyNormal, 0, 0, 1); math.Abs(d.effective-1.5) > 1e-9 || d.specialMultiplier() != 0 {
		t.Errorf("normal start %v %v", d.effective, d.specialMultiplier())
	}
	// Hard, after 21 days, chunk inhabited 50 h, full moon: 3 x (0.75+0.25+1+0.25) = 6.75.
	if d := newLocalDifficulty(difficultyHard, 72000+1440000, 3600000, 1); math.Abs(d.effective-6.75) > 1e-9 || d.specialMultiplier() != 1 {
		t.Errorf("hard max %v %v", d.effective, d.specialMultiplier())
	}
	// Easy halves the local part: 1 x (0.75 + 0.25 + (0.75+0.25)/2) = 1.5.
	if d := newLocalDifficulty(difficultyEasy, 72000+1440000, 3600000, 1); math.Abs(d.effective-1.5) > 1e-9 {
		t.Errorf("easy max %v", d.effective)
	}
	if !newLocalDifficulty(difficultyNormal, 0, 0, 1).harderThan(1.4) {
		t.Errorf("1.5 harder than 1.4")
	}
}

// TestSkyDarkenCurve: the sky darkness is 0 at noon and 11 at midnight, and
// phantoms (>= 5) can spawn from dusk.
func TestTimeOfDay(t *testing.T) {
	dark := func(time int) int {
		f := 1 - (math.Cos(timeOfDay(time)*2*math.Pi)*2 + 0.5)
		f = 1 - clamp(f, 0, 1)
		return int((1 - f) * 11)
	}
	if d := dark(6000); d != 0 {
		t.Errorf("noon darkness %d", d)
	}
	if d := dark(18000); d != 11 {
		t.Errorf("midnight darkness %d", d)
	}
	if d := dark(13000); d < 5 {
		t.Errorf("night start darkness %d", d)
	}
	if d := dark(12000); d >= 5 {
		t.Errorf("sunset darkness %d", d)
	}
}

// TestMth: the rotation helpers.
func TestMth(t *testing.T) {
	if v := rotlerp(350, 10, 30); v != 10 {
		t.Errorf("rotlerp across 0: %v", v)
	}
	if v := rotlerp(0, 90, 30); v != 30 {
		t.Errorf("rotlerp clamps: %v", v)
	}
	if v := approachDegrees(170, -170, 4); v != 174 {
		t.Errorf("approachDegrees %v", v)
	}
	if v := wrapDegrees(540); v != -180 {
		t.Errorf("wrap 540 = %v", v)
	}
	if reducedTickDelay(300) != 150 || reducedTickDelay(21) != 11 {
		t.Errorf("reducedTickDelay")
	}
}
