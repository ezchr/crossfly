package javasession

import (
	"strings"
	"unicode"

	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
)

// Bedrock variant numbers to Java names, for the variants Java keeps in registries (the entity data
// holds the registry entry's network id) or numbers differently. Sources: Geyser's Java->Bedrock
// entity translators read backwards, the Bedrock 1.26.60 entity JSONs and the 26.3 decompile.

// bedrockBoatToJava is the Java boat type of a Bedrock minecraft:boat Variant (oak, spruce, birch,
// jungle, acacia, dark oak, mangrove, bamboo, cherry, pale oak): Bedrock has one boat id for every
// wood, Java one type per wood.
var bedrockBoatToJava = []string{
	"minecraft:oak_boat", "minecraft:spruce_boat", "minecraft:birch_boat", "minecraft:jungle_boat",
	"minecraft:acacia_boat", "minecraft:dark_oak_boat", "minecraft:mangrove_boat",
	"minecraft:bamboo_raft", "minecraft:cherry_boat", "minecraft:pale_oak_boat",
}

// bedrockChestBoatToJava is bedrockBoatToJava for minecraft:chest_boat.
var bedrockChestBoatToJava = []string{
	"minecraft:oak_chest_boat", "minecraft:spruce_chest_boat", "minecraft:birch_chest_boat",
	"minecraft:jungle_chest_boat", "minecraft:acacia_chest_boat", "minecraft:dark_oak_chest_boat",
	"minecraft:mangrove_chest_boat", "minecraft:bamboo_chest_raft", "minecraft:cherry_chest_boat",
	"minecraft:pale_oak_chest_boat",
}

// bedrockCatVariant names the Bedrock cat Variants (white, tuxedo, ginger, siamese, british
// shorthair, calico, persian, ragdoll, tabby, black, jellie) as minecraft:cat_variant entries.
var bedrockCatVariant = []string{
	"white", "black", "red", "siamese", "british_shorthair", "calico", "persian", "ragdoll", "tabby",
	"all_black", "jellie",
}

// bedrockWolfVariant names the Bedrock wolf Variants as minecraft:wolf_variant entries.
var bedrockWolfVariant = []string{
	"pale", "ashen", "black", "chestnut", "rusty", "snowy", "spotted", "striped", "woods",
}

// bedrockFrogVariant names the Bedrock frog Variants as minecraft:frog_variant entries.
var bedrockFrogVariant = []string{"temperate", "cold", "warm"}

// bedrockVillagerProfession names the Bedrock villager_v2 Variants (professions) as
// minecraft:villager_profession entries.
var bedrockVillagerProfession = []string{
	"none", "farmer", "fisherman", "shepherd", "fletcher", "librarian", "cartographer", "cleric",
	"armorer", "weaponsmith", "toolsmith", "butcher", "leatherworker", "mason", "nitwit",
}

// bedrockVillagerType names the Bedrock villager_v2 MarkVariants (biome types) as
// minecraft:villager_type entries.
var bedrockVillagerType = []string{"plains", "desert", "jungle", "savanna", "snow", "swamp", "taiga"}

// javaVillagerProfession and javaVillagerType are the protocol ids of the built-in registries
// minecraft:villager_profession and minecraft:villager_type (reports/registries.json, the same in
// 26.2 and 26.3).
var (
	javaVillagerProfession = map[string]int32{
		"none": 0, "armorer": 1, "butcher": 2, "cartographer": 3, "cleric": 4, "farmer": 5,
		"fisherman": 6, "fletcher": 7, "leatherworker": 8, "librarian": 9, "mason": 10, "nitwit": 11,
		"shepherd": 12, "toolsmith": 13, "weaponsmith": 14,
	}
	javaVillagerType = map[string]int32{
		"desert": 0, "jungle": 1, "plains": 2, "savanna": 3, "snow": 4, "swamp": 5, "taiga": 6,
	}
)

// bedrockSoundVariant maps the Bedrock minecraft:sound_variant property of each mob to its Java
// sound variant entry; values not listed keep their name.
var bedrockSoundVariant = map[string]map[string]string{
	"minecraft:pig":     {"default": "classic"},
	"minecraft:cow":     {"default": "classic"},
	"minecraft:chicken": {"default": "classic"},
	"minecraft:cat":     {"default": "classic"},
	"minecraft:wolf":    {"default": "classic", "mad": "angry"},
}

// nth is list[i], or "" when i is out of range.
func nth(list []string, i int32) string {
	if i < 0 || int(i) >= len(list) {
		return ""
	}
	return list[i]
}

// registryEntry is the network id of minecraft:<entry> in the synchronised registry, or -1.
func registryEntry(registry, entry string) int32 {
	if entry == "" {
		return -1
	}
	return v777.RegistryID(registry, "minecraft:"+entry)
}

// paintingVariantName is the minecraft:painting_variant entry of a Bedrock painting motive: the
// camel case Bedrock names in snake case ("BurningSkull" -> "burning_skull"); newer motives are
// already snake case.
func paintingVariantName(motive string) string {
	var b strings.Builder
	for i, r := range motive {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}
