//go:build crossfly_full

package jfarm

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// RabbitVariant is Rabbit.Variant: the Java ids, which Bedrock's minecraft:variant shares for 0-5 (Bedrock
// has no killer bunny and shows it as brown).
type RabbitVariant int32

const (
	RabbitBrown          RabbitVariant = 0
	RabbitWhite          RabbitVariant = 1
	RabbitBlack          RabbitVariant = 2
	RabbitWhiteSplotched RabbitVariant = 3
	RabbitGold           RabbitVariant = 4
	RabbitSalt           RabbitVariant = 5
	RabbitEvil           RabbitVariant = 99
)

// Bedrock is the minecraft:variant a Bedrock client is shown.
func (v RabbitVariant) Bedrock() int32 {
	if v < RabbitBrown || v > RabbitSalt {
		return int32(RabbitBrown)
	}
	return int32(v)
}

// RabbitVariantByID is Variant.byId: unknown ids are brown (ByIdMap.sparse with DEFAULT).
func RabbitVariantByID(id int32) RabbitVariant {
	switch v := RabbitVariant(id); v {
	case RabbitBrown, RabbitWhite, RabbitBlack, RabbitWhiteSplotched, RabbitGold, RabbitSalt, RabbitEvil:
		return v
	}
	return RabbitBrown
}

var (
	// spawns_white_rabbits / spawns_gold_rabbits (data/minecraft/tags/worldgen/biome).
	whiteRabbitBiomes = set("snowy_plains", "ice_spikes", "frozen_ocean", "snowy_taiga", "frozen_river", "snowy_beach",
		"frozen_peaks", "jagged_peaks", "snowy_slopes", "grove")
	goldRabbitBiomes = set("desert")
)

// RandomRabbitVariant is Rabbit.getRandomRabbitVariant for biome b: 80% white / 20% white splotched in snowy
// biomes, gold in deserts, else 50% brown, 40% salt, 10% black.
func RandomRabbitVariant(r *rand.Rand, b world.Biome) RabbitVariant {
	n := JavaBiome(b)
	v := r.IntN(100)
	if _, ok := whiteRabbitBiomes[n]; ok {
		if v < 80 {
			return RabbitWhite
		}
		return RabbitWhiteSplotched
	}
	if _, ok := goldRabbitBiomes[n]; ok {
		return RabbitGold
	}
	switch {
	case v < 50:
		return RabbitBrown
	case v < 90:
		return RabbitSalt
	}
	return RabbitBlack
}

// RabbitOffspring is the variant of Rabbit.getBreedOffspring: 1 in 20 the biome's random variant (biome, the
// variant of the parent's position), else either parent's.
func RabbitOffspring(r *rand.Rand, biome RabbitVariant, self, partner RabbitVariant) RabbitVariant {
	if r.IntN(20) == 0 {
		return biome
	}
	if r.IntN(2) == 0 {
		return partner
	}
	return self
}

const (
	// RabbitMoreCarrotsDelay is Rabbit.MORE_CARROTS_DELAY: after a bite a rabbit waits about 40/1 ticks
	// (moreCarrotTicks drops by 0-2 a tick) before it wants carrots again.
	RabbitMoreCarrotsDelay = 40
	// RabbitJumpDelay and RabbitPanicJumpDelay are Rabbit.JUMP_DELAY_TICKS and PANIC_JUMP_DELAY_TICKS: the wait
	// after landing below and at flee speed (2.2).
	RabbitJumpDelay      = 10
	RabbitPanicJumpDelay = 3
	// RabbitJumpDuration is Rabbit.JUMP_DURATION_IN_TICKS.
	RabbitJumpDuration = 15
)

// TickMoreCarrots is the moreCarrotTicks countdown of Rabbit.customServerAiStep.
func TickMoreCarrots(r *rand.Rand, ticks int32) int32 {
	if ticks > 0 {
		ticks -= int32(r.IntN(3))
		if ticks < 0 {
			ticks = 0
		}
	}
	return ticks
}

// RaidableCarrot is RaidGardenGoal.isValidTarget at farmland pos: the block above is a fully grown carrot.
// (The goal also wants a hungry rabbit: moreCarrotTicks 0.)
func RaidableCarrot(tx *world.Tx, pos cube.Pos) bool {
	if _, ok := tx.Block(pos).(block.Farmland); !ok {
		return false // supports_crops: farmland only
	}
	c, ok := tx.Block(pos.Side(cube.FaceUp)).(block.Carrot)
	return ok && c.Growth >= 7
}

// EatCarrot is the bite of RaidGardenGoal.tick at the crop position: the carrot loses one growth stage (a
// stage 0 carrot is broken with drops, Level.destroyBlock(pos, true)). It reports whether there was a carrot
// (the rabbit then waits RabbitMoreCarrotsDelay ticks).
func EatCarrot(tx *world.Tx, pos cube.Pos) bool {
	c, ok := tx.Block(pos).(block.Carrot)
	if !ok {
		return false
	}
	if c.Growth == 0 {
		tx.SetBlock(pos, nil, nil)
		tx.AddParticle(pos.Vec3Centre(), breakParticle(c))
		for _, s := range c.BreakInfo().Drops(nil, nil) {
			dropItem(tx, pos, s)
		}
		return true
	}
	// levelEvent 2001 with the old state, then the younger carrot (flag 2).
	tx.AddParticle(pos.Vec3Centre(), breakParticle(c))
	c.Growth--
	tx.SetBlock(pos, c, nil)
	return true
}
