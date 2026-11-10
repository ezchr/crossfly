//go:build crossfly_full

package jpets

import (
	"math/rand/v2"
	"slices"
	"sort"
)

// Data-driven variants (VariantUtils.selectVariantToSpawn with
// PriorityProvider.select): every spawn condition of every variant becomes an
// entry; the entries are sorted by priority, highest first, and only those of
// the highest priority whose condition holds are kept; one of them is picked
// at random (Util.getRandomSafe). A variant with two matching conditions of
// that priority is in the list twice.
//
// The tables are the builtin registries of 26.3
// (data/minecraft/{wolf,cat,zombie_nautilus}_variant/*.json) in registry
// order.

// SpawnContext is what spawn conditions test (variant.SpawnContext: the
// biome, the structures at the position and the moon brightness).
type SpawnContext interface {
	// Biome is the biome id at the position ("minecraft:forest").
	Biome() string
	// InStructure reports whether the position is inside a structure of
	// the given id ("minecraft:swamp_hut").
	InStructure(id string) bool
	// MoonBrightness is DimensionType.MOON_BRIGHTNESS_PER_PHASE of the
	// current moon phase (1 full moon .. 0 new moon).
	MoonBrightness() float64
}

// condition is one spawn condition; nil always holds.
type condition func(SpawnContext) bool

type selector struct {
	cond     condition
	priority int
}

// variantDef is one entry of a variant registry.
type variantDef struct {
	name      string
	selectors []selector
}

func biomeIn(biomes ...string) condition {
	return func(c SpawnContext) bool { return slices.Contains(biomes, c.Biome()) }
}

func structureIn(ids ...string) condition {
	return func(c SpawnContext) bool {
		for _, id := range ids {
			if c.InStructure(id) {
				return true
			}
		}
		return false
	}
}

func moonBrightnessAtLeast(min float64) condition {
	return func(c SpawnContext) bool { return c.MoonBrightness() >= min }
}

// Biome tags used by spawn conditions (data/minecraft/tags/worldgen/biome).
var (
	tagIsJungle  = []string{"minecraft:bamboo_jungle", "minecraft:jungle", "minecraft:sparse_jungle"}
	tagIsSavanna = []string{"minecraft:savanna", "minecraft:savanna_plateau", "minecraft:windswept_savanna"}
	tagIsBadland = []string{"minecraft:badlands", "minecraft:eroded_badlands", "minecraft:wooded_badlands"}
	// SnowFoxBiomes is #minecraft:spawns_snow_foxes (Fox.Variant.byBiome).
	SnowFoxBiomes = []string{"minecraft:snowy_plains", "minecraft:ice_spikes", "minecraft:frozen_ocean",
		"minecraft:snowy_taiga", "minecraft:frozen_river", "minecraft:snowy_beach", "minecraft:frozen_peaks",
		"minecraft:jagged_peaks", "minecraft:snowy_slopes", "minecraft:grove"}
)

// wolfVariants is the minecraft:wolf_variant registry.
var wolfVariants = []variantDef{
	{"ashen", []selector{{biomeIn("minecraft:snowy_taiga"), 1}}},
	{"black", []selector{{biomeIn("minecraft:old_growth_pine_taiga"), 1}}},
	{"chestnut", []selector{{biomeIn("minecraft:old_growth_spruce_taiga"), 1}}},
	{"pale", []selector{{nil, 0}}},
	{"rusty", []selector{{biomeIn(tagIsJungle...), 1}}},
	{"snowy", []selector{{biomeIn("minecraft:grove"), 1}}},
	{"spotted", []selector{{biomeIn(tagIsSavanna...), 1}}},
	{"striped", []selector{{biomeIn(tagIsBadland...), 1}}},
	{"woods", []selector{{biomeIn("minecraft:forest"), 1}}},
}

// catVariants is the minecraft:cat_variant registry. The all black cat
// (#minecraft:cats_spawn_as_black = swamp huts) wins in a witch hut; under a
// full moon it is one of the eleven at priority 0, listed twice.
var catVariants = []variantDef{
	{"all_black", []selector{{structureIn("minecraft:swamp_hut"), 1}, {moonBrightnessAtLeast(0.9), 0}}},
	{"black", []selector{{nil, 0}}},
	{"british_shorthair", []selector{{nil, 0}}},
	{"calico", []selector{{nil, 0}}},
	{"jellie", []selector{{nil, 0}}},
	{"persian", []selector{{nil, 0}}},
	{"ragdoll", []selector{{nil, 0}}},
	{"red", []selector{{nil, 0}}},
	{"siamese", []selector{{nil, 0}}},
	{"tabby", []selector{{nil, 0}}},
	{"white", []selector{{nil, 0}}},
}

// zombieNautilusVariants is the minecraft:zombie_nautilus_variant registry.
var zombieNautilusVariants = []variantDef{
	{"temperate", []selector{{nil, 0}}},
	{"warm", []selector{{biomeIn("minecraft:warm_ocean"), 1}}},
}

// selectVariant is PriorityProvider.pick.
func selectVariant(defs []variantDef, r *rand.Rand, c SpawnContext) (string, bool) {
	type entry struct {
		name     string
		priority int
		cond     condition
	}
	var entries []entry
	for _, d := range defs {
		for _, s := range d.selectors {
			entries = append(entries, entry{d.name, s.priority, s.cond})
		}
	}
	// List.sort is stable (TimSort): equal priorities keep registry order.
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].priority > entries[j].priority })
	highest := -1 << 31
	kept := entries[:0]
	for _, e := range entries {
		if e.priority < highest {
			continue
		}
		if e.cond == nil || e.cond(c) {
			highest = e.priority
			kept = append(kept, e)
		}
	}
	if len(kept) == 0 {
		return "", false
	}
	return kept[r.IntN(len(kept))].name, true
}

// WolfSpawnVariant is the wolf variant chosen by Wolf.finalizeSpawn for the
// first wolf of a pack (the rest of the pack copies it: WolfPackData).
func WolfSpawnVariant(r *rand.Rand, c SpawnContext) string {
	v, _ := selectVariant(wolfVariants, r, c)
	return v
}

// CatSpawnVariant is the cat variant chosen by Cat.finalizeSpawn.
func CatSpawnVariant(r *rand.Rand, c SpawnContext) string {
	v, _ := selectVariant(catVariants, r, c)
	return v
}

// ZombieNautilusSpawnVariant is ZombieNautilus.finalizeSpawn's variant.
func ZombieNautilusSpawnVariant(r *rand.Rand, c SpawnContext) string {
	v, _ := selectVariant(zombieNautilusVariants, r, c)
	return v
}

// Bedrock numbers of the variants, which mob.Meta.Variant holds and both
// clients render from (javasession bedrockWolfVariant / bedrockCatVariant).
var (
	bedrockWolfVariants = []string{"pale", "ashen", "black", "chestnut", "rusty", "snowy", "spotted", "striped", "woods"}
	bedrockCatVariants  = []string{"white", "black", "red", "siamese", "british_shorthair", "calico", "persian", "ragdoll", "tabby", "all_black", "jellie"}
)

// WolfVariantID is the Meta.Variant of a wolf variant name (pale if unknown).
func WolfVariantID(name string) int32 { return int32(max(0, slices.Index(bedrockWolfVariants, name))) }

// WolfVariantName is the variant name of a Meta.Variant.
func WolfVariantName(id int32) string { return nthOr(bedrockWolfVariants, id, "pale") }

// CatVariantID is the Meta.Variant of a cat variant name (black, the default
// CatVariants.DEFAULT, if unknown).
func CatVariantID(name string) int32 {
	if i := slices.Index(bedrockCatVariants, name); i >= 0 {
		return int32(i)
	}
	return 1
}

// CatVariantName is the variant name of a Meta.Variant.
func CatVariantName(id int32) string { return nthOr(bedrockCatVariants, id, "black") }

func nthOr(list []string, i int32, def string) string {
	if i < 0 || int(i) >= len(list) {
		return def
	}
	return list[i]
}

// Sound variants (WolfSoundVariants/CatSoundVariants.pickRandomSoundVariant:
// Registry.getRandom, every entry equally likely).
var (
	WolfSoundVariants = []string{"angry", "big", "classic", "cute", "grumpy", "puglin", "sad"}
	CatSoundVariants  = []string{"classic", "royal"}
)

// RandomSoundVariant picks one of list.
func RandomSoundVariant(r *rand.Rand, list []string) string { return list[r.IntN(len(list))] }

// BedrockSoundVariant is the minecraft:sound_variant property value Bedrock
// uses for a Java sound variant (javasession bedrockSoundVariant reversed):
// classic is "default", the angry wolf "mad".
func BedrockSoundVariant(java string) string {
	switch java {
	case "classic":
		return "default"
	case "angry":
		return "mad"
	}
	return java
}
