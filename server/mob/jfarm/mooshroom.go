//go:build crossfly_full

package jfarm

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/google/uuid"
)

// MooshroomVariant is MushroomCow.Variant: red 0 (the default) or brown 1, the same ids as the Bedrock
// minecraft:variant of a mooshroom and Java's MushroomCow.DATA_TYPE.
type MooshroomVariant int32

const (
	MooshroomRed   MooshroomVariant = 0
	MooshroomBrown MooshroomVariant = 1
)

// Other is the other variant.
func (v MooshroomVariant) Other() MooshroomVariant {
	if v == MooshroomBrown {
		return MooshroomRed
	}
	return MooshroomBrown
}

// Name is the Java Type NBT value ("red", "brown").
func (v MooshroomVariant) Name() string {
	if v == MooshroomBrown {
		return "brown"
	}
	return "red"
}

// MushroomItem is the mushroom the variant drops when sheared (shearing/mooshroom/<variant>: 5).
func (v MooshroomVariant) MushroomItem() string {
	if v == MooshroomBrown {
		return "brown_mushroom"
	}
	return "red_mushroom"
}

// MooshroomShearDrops is the number of mushrooms shearing gives.
const MooshroomShearDrops = 5

// MooshroomMutateChance is MushroomCow.MUTATE_CHANCE: parents of the same variant have a 1 in 1024
// chance of a calf of the other variant.
const MooshroomMutateChance = 1024

// MooshroomOffspring is MushroomCow.getOffspringVariant.
func MooshroomOffspring(r *rand.Rand, a, b MooshroomVariant) MooshroomVariant {
	if a == b && r.IntN(MooshroomMutateChance) == 0 {
		return a.Other()
	}
	if r.IntN(2) == 0 {
		return a
	}
	return b
}

// MooshroomLightning is MushroomCow.thunderHit: each bolt (by UUID) flips the variant once. It returns
// the variant and whether it changed (play entity.mooshroom.convert at volume 2).
func MooshroomLightning(v MooshroomVariant, last *uuid.UUID, bolt uuid.UUID) (MooshroomVariant, bool) {
	if bolt == *last {
		return v, false
	}
	*last = bolt
	return v.Other(), true
}

// StewEffect is one SuspiciousStewEffects entry.
type StewEffect struct {
	Effect effect.Type
	// Ticks is the duration, Mth.floor(seconds * 20).
	Ticks int32
}

// stewEffects are the SuspiciousEffectHolder flowers of 26.3 (Blocks: FlowerBlock(effect, seconds) and
// EyeblossomBlock.Type), keyed by Java item name.
var stewEffects = map[string]StewEffect{
	"dandelion":          {effect.Saturation, 7},
	"golden_dandelion":   {effect.Saturation, 7},
	"torchflower":        {effect.NightVision, 100},
	"poppy":              {effect.NightVision, 100},
	"blue_orchid":        {effect.Saturation, 7},
	"allium":             {effect.FireResistance, 60},
	"azure_bluet":        {effect.Blindness, 220},
	"red_tulip":          {effect.Weakness, 140},
	"orange_tulip":       {effect.Weakness, 140},
	"white_tulip":        {effect.Weakness, 140},
	"pink_tulip":         {effect.Weakness, 140},
	"oxeye_daisy":        {effect.Regeneration, 140},
	"cornflower":         {effect.JumpBoost, 100},
	"wither_rose":        {effect.Wither, 140},
	"lily_of_the_valley": {effect.Poison, 220},
	"open_eyeblossom":    {effect.Blindness, 220},
	"closed_eyeblossom":  {effect.Nausea, 140},
}

// StewEffectOf is SuspiciousEffectHolder.tryGet for an item by Java name: the effect a brown mooshroom fed
// it puts in its next stew.
func StewEffectOf(javaItem string) (StewEffect, bool) {
	e, ok := stewEffects[javaItem]
	return e, ok
}
