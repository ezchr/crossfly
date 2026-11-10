//go:build crossfly_full

package hostile

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Java RandomSource calls on a mob's random (*rand.Rand). The exact random
// sequences of Java are not observable, only their distributions are.

// nextFloat is RandomSource.nextFloat: [0, 1).
func nextFloat(r *rand.Rand) float64 { return float64(r.Float32()) }

// nextInt is RandomSource.nextInt(n): [0, n). n <= 0 panics in Java; here it
// returns 0.
func nextInt(r *rand.Rand, n int) int {
	if n <= 0 {
		return 0
	}
	return r.IntN(n)
}

// nextBool is RandomSource.nextBoolean.
func nextBool(r *rand.Rand) bool { return r.IntN(2) == 0 }

// randomBetweenInclusive is Mth.randomBetweenInclusive(random, min, max).
func randomBetweenInclusive(r *rand.Rand, lo, hi int) int { return lo + nextInt(r, hi-lo+1) }

// reducedTickDelay is Goal.reducedTickDelay: goals that do not ask to be
// updated every tick run every other tick, so their delays are halved
// (rounded up).
func reducedTickDelay(ticks int) int { return (ticks + 1) / 2 }

// adjustedTickDelay is Goal.adjustedTickDelay: the delay as given for goals
// that tick every tick, halved otherwise.
func adjustedTickDelay(everyTick bool, ticks int) int {
	if everyTick {
		return ticks
	}
	return reducedTickDelay(ticks)
}

// wrapDegrees is Mth.wrapDegrees: the angle in [-180, 180).
func wrapDegrees(a float64) float64 {
	a = math.Mod(a, 360)
	if a >= 180 {
		a -= 360
	}
	if a < -180 {
		a += 360
	}
	return a
}

// rotlerp is MoveControl.rotlerp: turn a towards b by at most maxStep degrees.
func rotlerp(a, b, maxStep float64) float64 {
	diff := wrapDegrees(b - a)
	diff = math.Max(-maxStep, math.Min(maxStep, diff))
	r := a + diff
	if r < 0 {
		r += 360
	} else if r > 360 {
		r -= 360
	}
	return r
}

// approach is Mth.approach.
func approach(current, target, step float64) float64 {
	step = math.Abs(step)
	if current < target {
		return math.Min(current+step, target)
	}
	return math.Max(current-step, target)
}

// approachDegrees is Mth.approachDegrees.
func approachDegrees(current, target, step float64) float64 {
	return approach(current, current+wrapDegrees(target-current), step)
}

// degreesDifferenceAbs is Mth.degreesDifferenceAbs.
func degreesDifferenceAbs(a, b float64) float64 { return math.Abs(wrapDegrees(b - a)) }

// lerp is Mth.lerp.
func lerp(t, a, b float64) float64 { return a + t*(b-a) }

// clamp is Mth.clamp.
func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

// blockPos is BlockPos.containing(pos).
func blockPos(p mgl64.Vec3) cube.Pos { return cube.PosFromVec3(p) }

// horizontalDistance is Vec3.horizontalDistance.
func horizontalDistance(v mgl64.Vec3) float64 { return math.Hypot(v[0], v[2]) }

// Difficulty ids of net.minecraft.world.Difficulty.
const (
	difficultyPeaceful = 0
	difficultyEasy     = 1
	difficultyNormal   = 2
	difficultyHard     = 3
)

// difficultyID is Difficulty.getId of the world's difficulty.
func difficultyID(tx *world.Tx) int {
	switch tx.World().Difficulty() {
	case world.DifficultyPeaceful:
		return difficultyPeaceful
	case world.DifficultyEasy:
		return difficultyEasy
	case world.DifficultyHard:
		return difficultyHard
	}
	return difficultyNormal
}

// InhabitedTime returns how long players were in the chunk (LevelChunk.
// getInhabitedTime), for the local difficulty. Crossfly does not track it
// yet, so the chunk part of the local difficulty is 0 unless a package sets
// this.
var InhabitedTime = func(tx *world.Tx, pos cube.Pos) int64 { return 0 }

// moonBrightnessPerPhase is DimensionType.MOON_BRIGHTNESS_PER_PHASE.
var moonBrightnessPerPhase = [8]float64{1, 0.75, 0.5, 0.25, 0, 0.25, 0.5, 0.75}

// localDifficulty is DifficultyInstance at pos (ServerLevel.
// getCurrentDifficultyAt): its effective difficulty.
type localDifficulty struct {
	base      int
	effective float64
}

// difficultyAt is ServerLevel.getCurrentDifficultyAt.
func difficultyAt(tx *world.Tx, pos cube.Pos) localDifficulty {
	w := tx.World()
	gameTime := w.Time()
	moon := moonBrightnessPerPhase[((gameTime/24000)%8+8)%8]
	return newLocalDifficulty(difficultyID(tx), int64(gameTime), InhabitedTime(tx, pos), moon)
}

// newLocalDifficulty is DifficultyInstance(base, totalGameTime,
// localGameTime, moonBrightness) and its calculateDifficulty.
func newLocalDifficulty(base int, totalGameTime, localGameTime int64, moonBrightness float64) localDifficulty {
	d := localDifficulty{base: base}
	if base == difficultyPeaceful {
		return d
	}
	scale := 0.75
	global := clamp((float64(totalGameTime)-72000)/1440000, 0, 1) * 0.25
	scale += global
	local := 0.0
	hardScale := 0.75
	if base == difficultyHard {
		hardScale = 1
	}
	local += clamp(float64(localGameTime)/3600000, 0, 1) * hardScale
	local += clamp(moonBrightness*0.25, 0, global)
	if base == difficultyEasy {
		local *= 0.5
	}
	scale += local
	d.effective = float64(base) * scale
	return d
}

// harderThan is DifficultyInstance.isHarderThan.
func (d localDifficulty) harderThan(v float64) bool { return d.effective > v }

// specialMultiplier is DifficultyInstance.getSpecialMultiplier.
func (d localDifficulty) specialMultiplier() float64 {
	switch {
	case d.effective < 2:
		return 0
	case d.effective > 4:
		return 1
	}
	return (d.effective - 2) / 2
}
