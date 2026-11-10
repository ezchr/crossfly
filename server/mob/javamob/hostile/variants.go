//go:build crossfly_full

package hostile

import (
	"time"

	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
)

// The small mobs of this row are variants of the foundation's zombie,
// skeleton and spider (and the pets row's camel): these are their own rules.

const (
	huskID       = "minecraft:husk"
	strayID      = "minecraft:stray"
	boggedID     = "minecraft:bogged"
	parchedID    = "minecraft:parched"
	caveSpiderID = "minecraft:cave_spider"
	giantID      = "minecraft:giant"
	camelHuskID  = "minecraft:camel_husk"
)

// ticks converts game ticks to a duration.
func ticks(n int) time.Duration { return time.Duration(n) * time.Second / 20 }

// HuskHungerTicks is Husk.doHurtTarget: a husk with an empty main hand that
// lands a hit gives Hunger for 140 ticks per point of effective local
// difficulty (rounded down), 0 for none.
func HuskHungerTicks(effectiveDifficulty float64, mainHandEmpty bool) int {
	if !mainHandEmpty {
		return 0
	}
	return 140 * int(effectiveDifficulty)
}

// HuskHit is the end of Husk.doHurtTarget for a landed hit.
func HuskHit(m *mob.Mob, target world.Entity) {
	main, _ := m.HeldItems()
	n := HuskHungerTicks(difficultyAt(m.Tx(), blockPos(m.Pos())).effective, main.Empty())
	if n <= 0 {
		return
	}
	if l, ok := target.(interface{ AddEffect(effect.Effect) }); ok {
		l.AddEffect(effect.New(effect.Hunger, 1, ticks(n)))
	}
}

// Husk facts (Husk): it does not burn in the sun (not in #burn_in_daylight,
// isSunSensitive false), turns into a zombie when drowning (convertsInWater,
// convertsToWhenDrowning ZOMBIE, conversion level event 1041), and its baby
// is 0.49 x 0.98 with eyes at 0.825.
const (
	HuskConvertsTo      = "minecraft:zombie"
	HuskConversionSound = 1041
	HuskBabyWidth       = 0.49
	HuskBabyHeight      = 0.98
	HuskBabyEyeHeight   = 0.825
	camelHuskRideChance = 0.1
)

// HuskCamelJockey is the camel husk part of Husk.finalizeSpawn: a husk
// spawning naturally (its group did not try yet), with room for a camel
// husk, rides one 10% of the time, holding an iron spear, with a parched
// riding behind it. It reports whether to make the jockey.
func HuskCamelJockey(reasonNatural, groupTried, room bool, roll float64) bool {
	return reasonNatural && !groupTried && room && roll < camelHuskRideChance
}

// StrayArrowEffect is Stray.getArrow: Slowness for 600 ticks on its arrows.
func StrayArrowEffect() effect.Effect { return effect.New(effect.Slowness, 1, ticks(600)) }

// BoggedArrowEffect is Bogged.getArrow: Poison for 100 ticks.
func BoggedArrowEffect() effect.Effect { return effect.New(effect.Poison, 1, ticks(100)) }

// ParchedArrowEffect is Parched.getArrow: Weakness for 600 ticks.
func ParchedArrowEffect() effect.Effect { return effect.New(effect.Weakness, 1, ticks(600)) }

// Bogged and parched shoot slower than skeletons (getAttackInterval 70,
// getHardAttackInterval 50) and have 16 health.
const (
	SlowSkeletonAttackInterval     = 70
	SlowSkeletonHardAttackInterval = 50
	BoggedParchedHealth            = 16
)

// ParchedImmuneToEffect is Parched.canBeAffected: weakness never takes.
func ParchedImmuneToEffect(t effect.Type) bool { return t == effect.Weakness }

// BoggedShear is Bogged.mobInteract with shears on an unsheared bogged: the
// shear sound, its mushrooms (loot table shearing/bogged) drop at its
// height, it is sheared from then on (both clients: Meta.Sheared, java J16),
// and the shears lose durability. It reports whether it sheared.
func BoggedShear(m *mob.Mob, held item.Stack, dropLoot func(m *mob.Mob)) bool {
	if held.Empty() || m.Sheared() {
		return false
	}
	if name, _ := held.Item().EncodeItem(); name != "minecraft:shears" {
		return false
	}
	playSound(m, "entity.bogged.shear", 1, 1)
	dropLoot(m)
	m.UpdateMeta(func(meta *mob.Meta) { meta.Sheared = true })
	return true
}

// CaveSpiderPoisonSeconds is CaveSpider.doHurtTarget: a landed hit poisons
// for 7 s on normal, 15 s on hard, none on easy/peaceful.
func CaveSpiderPoisonSeconds(difficulty int) int {
	switch difficulty {
	case difficultyNormal:
		return 7
	case difficultyHard:
		return 15
	}
	return 0
}

// CaveSpiderHit is the end of CaveSpider.doHurtTarget for a landed hit.
func CaveSpiderHit(m *mob.Mob, target world.Entity) {
	s := CaveSpiderPoisonSeconds(difficultyID(m.Tx()))
	if s == 0 {
		return
	}
	if l, ok := target.(interface{ AddEffect(effect.Effect) }); ok {
		l.AddEffect(effect.New(effect.Poison, 1, time.Duration(s)*time.Second))
	}
}

// CaveSpiderHealth is CaveSpider.createCaveSpider; it never spawns with a
// jockey or effects (finalizeSpawn returns the group data untouched).
const CaveSpiderHealth = 12

// Giant attributes (Giant.createAttributes); it only wanders where it is
// dark (getWalkTargetValue = the light-level path cost) and has no goals.
const (
	GiantHealth = 100
	GiantSpeed  = 0.5
	GiantAttack = 50
)

// Camel husk (CamelHusk): a camel that despawns, never breeds or grows,
// eats #camel_husk_food, can be leashed only without a mob rider, and dashes
// 4x (chargeSpeedModifier); interacting with it makes it persistent.
const CamelHuskChargeSpeed = 4.0
