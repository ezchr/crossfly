//go:build crossfly_full

package jfarm

import "github.com/df-mc/dragonfly/server/world"

// Climate is a cow, pig, chicken or frog variant (the Java registry entry names, which are also the
// Bedrock minecraft:climate_variant property values), and the sheep spawn colour table to use.
type Climate string

const (
	Temperate Climate = "temperate"
	Warm      Climate = "warm"
	Cold      Climate = "cold"
)

// javaBiomeName maps the Bedrock biome names Dragonfly uses that Java does not have to the Java biome a
// world converter gives them (the legacy hills/mutated biomes and the renamed ones).
var javaBiomeName = map[string]string{
	"bamboo_jungle_hills":              "bamboo_jungle",
	"birch_forest_hills":               "birch_forest",
	"birch_forest_mutated":             "old_growth_birch_forest",
	"birch_forest_hills_mutated":       "old_growth_birch_forest",
	"cold_beach":                       "snowy_beach",
	"cold_taiga":                       "snowy_taiga",
	"cold_taiga_hills":                 "snowy_taiga",
	"cold_taiga_mutated":               "snowy_taiga",
	"desert_hills":                     "desert",
	"desert_mutated":                   "desert",
	"extreme_hills":                    "windswept_hills",
	"extreme_hills_edge":               "windswept_hills",
	"extreme_hills_mutated":            "windswept_gravelly_hills",
	"extreme_hills_plus_trees":         "windswept_forest",
	"extreme_hills_plus_trees_mutated": "windswept_gravelly_hills",
	"forest_hills":                     "forest",
	"hell":                             "nether_wastes",
	"ice_mountains":                    "snowy_plains",
	"ice_plains":                       "snowy_plains",
	"ice_plains_spikes":                "ice_spikes",
	"jungle_edge":                      "sparse_jungle",
	"jungle_edge_mutated":              "sparse_jungle",
	"jungle_hills":                     "jungle",
	"jungle_mutated":                   "jungle",
	"legacy_frozen_ocean":              "frozen_ocean",
	"mega_taiga":                       "old_growth_pine_taiga",
	"mega_taiga_hills":                 "old_growth_pine_taiga",
	"redwood_taiga_mutated":            "old_growth_spruce_taiga",
	"redwood_taiga_hills_mutated":      "old_growth_spruce_taiga",
	"mesa":                             "badlands",
	"mesa_bryce":                       "eroded_badlands",
	"mesa_plateau":                     "badlands",
	"mesa_plateau_mutated":             "badlands",
	"mesa_plateau_stone":               "wooded_badlands",
	"mesa_plateau_stone_mutated":       "wooded_badlands",
	"mushroom_island":                  "mushroom_fields",
	"mushroom_island_shore":            "mushroom_fields",
	"roofed_forest":                    "dark_forest",
	"roofed_forest_mutated":            "dark_forest",
	"savanna_mutated":                  "windswept_savanna",
	"savanna_plateau_mutated":          "windswept_savanna",
	"soulsand_valley":                  "soul_sand_valley",
	"stone_beach":                      "stony_shore",
	"swampland":                        "swamp",
	"swampland_mutated":                "swamp",
	"taiga_hills":                      "taiga",
	"taiga_mutated":                    "taiga",
}

// JavaBiome is the Java name of a Dragonfly biome.
func JavaBiome(b world.Biome) string {
	if b == nil {
		return ""
	}
	n := b.String()
	if j, ok := javaBiomeName[n]; ok {
		return j
	}
	return n
}

func set(names ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(names))
	for _, n := range names {
		m[n] = struct{}{}
	}
	return m
}

// The 26.3 biome tags (data/minecraft/tags/worldgen/biome) with the #is_* tags they include expanded.
var (
	warmCommon = []string{"desert", "warm_ocean", "bamboo_jungle", "jungle", "sparse_jungle", "savanna",
		"savanna_plateau", "windswept_savanna", "nether_wastes", "soul_sand_valley", "crimson_forest",
		"warped_forest", "basalt_deltas", "badlands", "eroded_badlands", "wooded_badlands", "mangrove_swamp"}
	coldCommon = []string{"snowy_plains", "ice_spikes", "frozen_peaks", "jagged_peaks", "snowy_slopes",
		"frozen_ocean", "deep_frozen_ocean", "grove", "deep_dark", "frozen_river", "snowy_taiga", "snowy_beach",
		"the_end", "end_highlands", "end_midlands", "small_end_islands", "end_barrens"}

	// spawns_warm_variant_farm_animals / spawns_cold_variant_farm_animals
	warmFarmAnimals = set(append(append([]string{}, warmCommon...), "deep_lukewarm_ocean", "lukewarm_ocean")...)
	coldFarmAnimals = set(append(append([]string{}, coldCommon...), "cold_ocean", "deep_cold_ocean",
		"old_growth_pine_taiga", "old_growth_spruce_taiga", "taiga", "windswept_forest", "windswept_gravelly_hills",
		"windswept_hills", "stony_peaks", "dappled_forest")...)
	// spawns_warm_variant_frogs / spawns_cold_variant_frogs
	warmFrogs = set(warmCommon...)
	coldFrogs = set(coldCommon...)
)

// FarmAnimalClimate is the variant a cow, pig or chicken spawns as in biome b (the spawn_conditions of
// data/minecraft/{cow,pig,chicken}_variant: warm or cold by biome tag at priority 1, else temperate;
// VariantUtils.selectVariantToSpawn). It is also SheepColorSpawnRules.getSheepColorConfiguration's choice
// (warm is checked first).
func FarmAnimalClimate(b world.Biome) Climate {
	n := JavaBiome(b)
	if _, ok := warmFarmAnimals[n]; ok {
		return Warm
	}
	if _, ok := coldFarmAnimals[n]; ok {
		return Cold
	}
	return Temperate
}

// FrogClimate is the variant a frog spawns as in biome b (data/minecraft/frog_variant). Frogs born from
// tadpoles take it from the biome they grow up in (Tadpole.ageUp -> Frog.finalizeSpawn).
func FrogClimate(b world.Biome) Climate {
	n := JavaBiome(b)
	if _, ok := warmFrogs[n]; ok {
		return Warm
	}
	if _, ok := coldFrogs[n]; ok {
		return Cold
	}
	return Temperate
}
