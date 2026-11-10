//go:build crossfly_full

package jpets

import (
	"math/rand/v2"
)

// Horse stats (AbstractHorse). Java computes these with float constants
// (0.4F, 0.45F) widened to double; f32 keeps those exact values.

func f32(v float32) float64 { return float64(v) }

// GenerateMaxHealth is AbstractHorse.generateMaxHealth(random::nextInt):
// 15 + nextInt(8) + nextInt(9), so 15..30.
func GenerateMaxHealth(r *rand.Rand) float64 {
	return generateMaxHealth(func(n int) int { return r.IntN(n) })
}

func generateMaxHealth(bound func(n int) int) float64 {
	return float64(float32(15 + bound(8) + bound(9)))
}

// GenerateJumpStrength is AbstractHorse.generateJumpStrength(random::nextDouble):
// 0.4F + 3 x nextDouble() x 0.2.
func GenerateJumpStrength(r *rand.Rand) float64 {
	return generateJumpStrength(r.Float64)
}

func generateJumpStrength(p func() float64) float64 {
	return f32(0.4) + p()*0.2 + p()*0.2 + p()*0.2
}

// GenerateSpeed is AbstractHorse.generateSpeed(random::nextDouble):
// (0.45F + 3 x nextDouble() x 0.3) x 0.25.
func GenerateSpeed(r *rand.Rand) float64 {
	return generateSpeed(r.Float64)
}

func generateSpeed(p func() float64) float64 {
	return (f32(0.45) + p()*0.3 + p()*0.3 + p()*0.3) * 0.25
}

// The ranges of the inherited stats (AbstractHorse.MIN_/MAX_*, floats).
var (
	MinHorseSpeed    = float64(float32(generateSpeed(func() float64 { return 0 })))
	MaxHorseSpeed    = float64(float32(generateSpeed(func() float64 { return 1 })))
	MinHorseJump     = float64(float32(generateJumpStrength(func() float64 { return 0 })))
	MaxHorseJump     = float64(float32(generateJumpStrength(func() float64 { return 1 })))
	MinHorseHealth   = generateMaxHealth(func(int) int { return 0 })
	MaxHorseHealth   = generateMaxHealth(func(n int) int { return n - 1 })
	horseBreedMargin = 0.15 // AbstractHorse.BREEDING_CROSS_FACTOR
)

// HorseAttributes are the base values of an equine's attributes
// (AbstractHorse.createBaseHorseAttributes, AbstractChestedHorse.
// createBaseChestedHorseAttributes and each type's createAttributes).
type HorseAttributes struct {
	MaxHealth, Speed, JumpStrength float64
	StepHeight, SafeFall, FallMult float64
}

// BaseHorseAttributes is AbstractHorse.createBaseHorseAttributes.
var BaseHorseAttributes = HorseAttributes{
	MaxHealth: 53, Speed: f32(0.225), JumpStrength: 0.7,
	StepHeight: 1, SafeFall: 6, FallMult: 0.5,
}

// BaseChestedAttributes is AbstractChestedHorse.createBaseChestedHorseAttributes
// (donkey, mule, llama).
var BaseChestedAttributes = HorseAttributes{
	MaxHealth: 53, Speed: f32(0.175), JumpStrength: 0.5,
	StepHeight: 1, SafeFall: 6, FallMult: 0.5,
}

// RandomizeHorse is Horse.randomizeAttributes: health, speed and jump.
func RandomizeHorse(r *rand.Rand, a *HorseAttributes) {
	a.MaxHealth = GenerateMaxHealth(r)
	a.Speed = GenerateSpeed(r)
	a.JumpStrength = GenerateJumpStrength(r)
}

// RandomizeChested is AbstractChestedHorse.randomizeAttributes: only health.
func RandomizeChested(r *rand.Rand, a *HorseAttributes) {
	a.MaxHealth = GenerateMaxHealth(r)
}

// OffspringAttribute is AbstractHorse.createOffspringAttribute: the average of
// the parents (clamped to the range) plus a spread of |a-b| + 2 x 15% of the
// range, weighted by the mean of three random doubles; values past an end of
// the range are mirrored back into it.
func OffspringAttribute(a, b, lo, hi float64, r *rand.Rand) float64 {
	if hi <= lo {
		panic("jpets: incorrect range for an attribute")
	}
	a, b = clamp(a, lo, hi), clamp(b, lo, hi)
	margin := horseBreedMargin * (hi - lo)
	rng := abs(a-b) + margin*2
	avg := (a + b) / 2
	quality := (r.Float64()+r.Float64()+r.Float64())/3 - 0.5
	v := avg + rng*quality
	switch {
	case v > hi:
		return hi - (v - hi)
	case v < lo:
		return lo + (lo - v)
	}
	return v
}

// OffspringAttributes is AbstractHorse.setOffspringAttributes: health, jump
// strength and speed (in that order) from the base values of both parents.
func OffspringAttributes(a, b HorseAttributes, r *rand.Rand) HorseAttributes {
	baby := a
	baby.MaxHealth = OffspringAttribute(a.MaxHealth, b.MaxHealth, MinHorseHealth, MaxHorseHealth, r)
	baby.JumpStrength = OffspringAttribute(a.JumpStrength, b.JumpStrength, MinHorseJump, MaxHorseJump, r)
	baby.Speed = OffspringAttribute(a.Speed, b.Speed, MinHorseSpeed, MaxHorseSpeed, r)
	return baby
}

// Temper (AbstractHorse.temper, getMaxTemper: 100, Llama 30).
const (
	MaxTemperHorse = 100
	MaxTemperLlama = 30
)

// ModifyTemper is AbstractHorse.modifyTemper: clamp(temper+amount, 0, max).
func ModifyTemper(temper, amount, max int) int {
	return int(clamp(float64(temper+amount), 0, float64(max)))
}

// TameByRiding is the roll of RunAroundLikeCrazyGoal.tick once a player has
// stayed on long enough: random.nextInt(maxTemper) < temper tames the horse;
// otherwise the temper rises by 5 and the rider is thrown off.
func TameByRiding(r *rand.Rand, temper, maxTemper int) bool {
	return maxTemper > 0 && r.IntN(maxTemper) < temper
}

// HorseFood is one row of AbstractHorse.handleEating / Llama.handleEating.
type HorseFood struct {
	Heal   float64
	AgeUp  int
	Temper int
	// Love: a tamed adult not in love falls in love (golden carrot, golden
	// apples; hay for llamas, which also need canFallInLove).
	Love bool
}

// horseFoods is AbstractHorse.handleEating.
var horseFoods = map[string]HorseFood{
	"minecraft:wheat":                  {Heal: 2, AgeUp: 20, Temper: 3},
	"minecraft:sugar":                  {Heal: 1, AgeUp: 30, Temper: 3},
	"minecraft:hay_block":              {Heal: 20, AgeUp: 180},
	"minecraft:apple":                  {Heal: 3, AgeUp: 60, Temper: 3},
	"minecraft:red_mushroom":           {Heal: 3, Temper: 3},
	"minecraft:carrot":                 {Heal: 3, AgeUp: 60, Temper: 3},
	"minecraft:golden_carrot":          {Heal: 4, AgeUp: 60, Temper: 5, Love: true},
	"minecraft:golden_apple":           {Heal: 10, AgeUp: 240, Temper: 10, Love: true},
	"minecraft:enchanted_golden_apple": {Heal: 10, AgeUp: 240, Temper: 10, Love: true},
}

// llamaFoods is Llama.handleEating.
var llamaFoods = map[string]HorseFood{
	"minecraft:wheat":     {Heal: 2, AgeUp: 10, Temper: 3},
	"minecraft:hay_block": {Heal: 10, AgeUp: 90, Temper: 6, Love: true},
}

// HorseFoodOf returns the food row of an item for a horse, donkey or mule
// (llama false) or a llama (llama true).
func HorseFoodOf(name string, llama bool) (HorseFood, bool) {
	if llama {
		f, ok := llamaFoods[name]
		return f, ok
	}
	f, ok := horseFoods[name]
	return f, ok
}

// IsHorseFood is ItemTags.HORSE_FOOD (data/minecraft/tags/item/horse_food.json).
func IsHorseFood(name string) bool {
	switch name {
	case "minecraft:wheat", "minecraft:sugar", "minecraft:hay_block", "minecraft:apple",
		"minecraft:carrot", "minecraft:golden_carrot", "minecraft:golden_apple",
		"minecraft:enchanted_golden_apple":
		return true
	}
	return false
}

// IsHorseTempt is ItemTags.HORSE_TEMPT_ITEMS (golden carrot and golden apples).
func IsHorseTempt(name string) bool {
	switch name {
	case "minecraft:golden_carrot", "minecraft:golden_apple", "minecraft:enchanted_golden_apple":
		return true
	}
	return false
}

// IsLlamaFood is ItemTags.LLAMA_FOOD; LLAMA_TEMPT_ITEMS is only hay.
func IsLlamaFood(name string) bool { return name == "minecraft:wheat" || name == "minecraft:hay_block" }

// EatState is what handleEating reads from the mob.
type EatState struct {
	Health, MaxHealth float64
	Baby, AgeLocked   bool
	Tamed             bool
	// Age is AgeableMob.getAge (0 adult ready to breed).
	Age    int
	InLove bool
	// CanFallInLove is Animal.canFallInLove (llama hay only).
	CanFallInLove bool
	Temper        int
	MaxTemper     int
}

// EatResult is what handleEating does: heal, age up the baby, set temper and
// love. Used reports whether the item is consumed (and the eat sound plays).
type EatResult struct {
	Heal      float64
	AgeUp     int
	NewTemper int
	Love      bool
	Used      bool
}

// HandleEating is AbstractHorse.handleEating (llama false) and
// Llama.handleEating (llama true) for food f.
func HandleEating(f HorseFood, s EatState, llama bool) EatResult {
	res := EatResult{NewTemper: s.Temper}
	if f.Love && s.Tamed && s.Age == 0 {
		if llama {
			// Llama: hay with isTamed && getAge()==0 && canFallInLove.
			if s.CanFallInLove {
				res.Used, res.Love = true, true
			}
		} else if !s.InLove {
			res.Used, res.Love = true, true
		}
	}
	if s.Health < s.MaxHealth && f.Heal > 0 {
		res.Heal = f.Heal
		res.Used = true
	}
	if s.Baby && f.AgeUp > 0 && !s.AgeLocked {
		res.AgeUp = f.AgeUp
		res.Used = true
	}
	if f.Temper > 0 && (res.Used || !s.Tamed) && s.Temper < s.MaxTemper {
		res.NewTemper = ModifyTemper(s.Temper, f.Temper, s.MaxTemper)
		res.Used = true
	}
	return res
}

// PlayerJumpPendingScale is PlayerRideableJumping.getPlayerJumpPendingScale:
// the Java client sends the jump bar 0..100 in player_command.
func PlayerJumpPendingScale(jumpAmount int) float64 {
	if jumpAmount >= 90 {
		return 1
	}
	return float64(float32(0.4) + float32(0.4)*float32(jumpAmount)/90)
}

// HorseJumpVelocity is AbstractHorse.executeRidersJump with
// LivingEntity.getJumpPower(amount): jump strength x amount x block jump
// factor + jump boost; forward input adds 0.4 x amount in the facing
// direction. It returns the new y velocity and the horizontal push.
func HorseJumpVelocity(jumpStrength, amount, blockJumpFactor, jumpBoost float64, forward bool, yawDeg float64) (vy, dx, dz float64) {
	vy = float64(float32(jumpStrength)*float32(amount)*float32(blockJumpFactor) + float32(jumpBoost))
	if forward {
		s, c := sinDeg32(yawDeg), cosDeg32(yawDeg)
		dx = float64(-0.4 * s * float32(amount))
		dz = float64(0.4 * c * float32(amount))
	}
	return vy, dx, dz
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
