package jraid

// Difficulty is net.minecraft.world.Difficulty, in its ordinal order.
type Difficulty int

const (
	Peaceful Difficulty = iota
	Easy
	Normal
	Hard
)

// DifficultyInstance is net.minecraft.world.DifficultyInstance: the regional difficulty at a position.
type DifficultyInstance struct {
	Base      Difficulty
	Effective float32
}

// NewDifficultyInstance is DifficultyInstance(base, totalGameTime, localGameTime, moonBrightness) with
// DifficultyInstance.calculateDifficulty: totalGameTime is the level's game time, localGameTime the inhabited time of
// the chunk (ServerLevel.getCurrentDifficultyAt) and moonBrightness the moon's brightness (0 for an unloaded chunk).
func NewDifficultyInstance(base Difficulty, totalGameTime, localGameTime int64, moonBrightness float32) DifficultyInstance {
	return DifficultyInstance{Base: base, Effective: calculateDifficulty(base, totalGameTime, localGameTime, moonBrightness)}
}

func calculateDifficulty(base Difficulty, totalGameTime, localGameTime int64, moonBrightness float32) float32 {
	if base == Peaceful {
		return 0
	}
	isHard := base == Hard
	scale := float32(0.75)
	globalScale := clampf((float32(totalGameTime)+-72000)/1440000, 0, 1) * 0.25
	scale += globalScale
	localScale := float32(0)
	if isHard {
		localScale += clampf(float32(localGameTime)/3600000, 0, 1) * 1
	} else {
		localScale += clampf(float32(localGameTime)/3600000, 0, 1) * 0.75
	}
	localScale += clampf(moonBrightness*0.25, 0, globalScale)
	if base == Easy {
		localScale *= 0.5
	}
	scale += localScale
	return float32(base) * scale
}

// IsHarderThan is DifficultyInstance.isHarderThan.
func (d DifficultyInstance) IsHarderThan(required float32) bool { return d.Effective > required }

// IsHard is DifficultyInstance.isHard.
func (d DifficultyInstance) IsHard() bool { return d.Effective >= float32(Hard) }

// SpecialMultiplier is DifficultyInstance.getSpecialMultiplier.
func (d DifficultyInstance) SpecialMultiplier() float32 {
	if d.Effective < 2 {
		return 0
	}
	if d.Effective > 4 {
		return 1
	}
	return (d.Effective - 2) / 2
}

func clampf(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
