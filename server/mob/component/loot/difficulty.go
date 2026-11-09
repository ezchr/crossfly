//go:build crossfly_full

package loot

import (
	"math"

	"github.com/df-mc/dragonfly/server/world"
)

// moonBrightness is Dimension::getMoonBrightness by moon phase (day % 8).
var moonBrightness = [8]float64{1, 0.75, 0.5, 0.25, 0, 0.25, 0.5, 0.75}

// SpecialMultiplier is BDS Level::getSpecialMultiplier (DifficultyUtils::
// getSpecialMultiplier): the clamped regional difficulty, 0..1, used by
// enchant_random_gear and random_regional_difficulty_chance.
func SpecialMultiplier(tx *world.Tx) float64 {
	w := tx.World()
	return SpecialMultiplierAt(DifficultyID(w.Difficulty()), w.Time())
}

// SpecialMultiplierAt is SpecialMultiplier for a difficulty id (0 peaceful
// .. 3 hard) and an absolute world time in ticks.
//
// BDS has no inhabited time: the chunk part is a constant 0.375 (0.5 on
// Hard), plus the moon brightness/4 capped by the time part, halved on Easy;
// regional = (0.75 + time part + that) x difficulty, clamped to (r-2)/2
// within [2, 4]. The time part is clamp((time-72000)/1440000, 0, 1)/4: the
// value BDS feeds in comes through a virtual call the dump does not resolve,
// so this keeps the Java formula it replaced.
func SpecialMultiplierAt(difficulty, worldTime int) float64 {
	if difficulty <= 0 {
		return 0
	}
	timePart := clampF((float64(worldTime)-72000)/1440000, 0, 1) * 0.25
	base := 0.375
	if difficulty == 3 {
		base = 0.5
	}
	day := max(worldTime/24000, 0)
	moon := math.Min(math.Max(moonBrightness[day%8]*0.25, 0), timePart)
	chunk := base + moon
	if difficulty == 1 {
		chunk *= 0.5
	}
	r := (chunk + timePart + 0.75) * float64(difficulty)
	switch {
	case r < 2:
		return 0
	case r > 4:
		return 1
	}
	return (r - 2) / 2
}

// DifficultyID is the Bedrock difficulty id (0 peaceful .. 3 hard).
func DifficultyID(d world.Difficulty) int {
	switch d {
	case world.DifficultyPeaceful:
		return 0
	case world.DifficultyEasy:
		return 1
	case world.DifficultyHard:
		return 3
	}
	return 2
}

func clampF(v, lo, hi float64) float64 { return math.Min(math.Max(v, lo), hi) }
