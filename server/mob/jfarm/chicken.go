//go:build crossfly_full

package jfarm

import "math/rand/v2"

// EggTimer is Chicken.eggTime: the ticks until a chicken lays its next egg, 6000 to 11999 (5 to 10
// minutes), drawn again after every egg.
type EggTimer int32

// NewEggTimer is the Chicken constructor's (and every lay's) random.nextInt(6000) + 6000.
func NewEggTimer(r *rand.Rand) EggTimer { return EggTimer(r.IntN(6000) + 6000) }

// Tick is the egg part of Chicken.aiStep: a living adult chicken that is not a chicken jockey counts down
// and lays when the timer runs out, then draws a new time. It reports whether to lay now (the caller drops
// EggItem from the gameplay/chicken_lay loot table, plays entity.chicken.egg at EggPitch and emits the
// entity_place game event).
func (t *EggTimer) Tick(r *rand.Rand, alive, baby, jockey bool) (lay bool) {
	if !alive || baby || jockey {
		return false
	}
	*t--
	if *t > 0 {
		return false
	}
	*t = NewEggTimer(r)
	return true
}

// EggItem is the item the loot table gameplay/chicken_lay gives a chicken of climate c: egg, brown_egg
// (warm) or blue_egg (cold), Java names.
func EggItem(c Climate) string {
	switch c {
	case Warm:
		return "brown_egg"
	case Cold:
		return "blue_egg"
	}
	return "egg"
}

// EggPitch is the pitch of the egg sound: (r - r) * 0.2 + 1.
func EggPitch(r *rand.Rand) float64 { return float64((r.Float32()-r.Float32())*0.2 + 1) }

// ChickenJockeyXP is Chicken.getBaseExperienceReward for a chicken jockey (others give Animal's 1 to 3).
const ChickenJockeyXP = 10

// ChickenFallFactor is Chicken.aiStep's slow fall: while not on the ground and falling, the vertical
// motion is multiplied by 0.6 every tick.
const ChickenFallFactor = 0.6
