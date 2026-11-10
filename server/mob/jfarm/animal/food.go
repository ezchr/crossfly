//go:build crossfly_full

package animal

import (
	"strings"

	"github.com/df-mc/dragonfly/server/world"
)

// ItemTag is a Java item tag: a set of item names without the minecraft: prefix. Items are matched by
// the name Dragonfly encodes them with, translated to the Java name where the editions differ.
type ItemTag map[string]struct{}

func tag(names ...string) ItemTag {
	t := make(ItemTag, len(names))
	for _, n := range names {
		t[n] = struct{}{}
	}
	return t
}

// The animal food tags of Java 26.3 (data/minecraft/tags/item/*_food.json).
var (
	CowFood       = tag("wheat")
	SheepFood     = tag("wheat")
	GoatFood      = tag("wheat")
	ChickenFood   = tag("wheat_seeds", "melon_seeds", "pumpkin_seeds", "beetroot_seeds", "torchflower_seeds", "pitcher_pod")
	PigFood       = tag("carrot", "potato", "beetroot")
	RabbitFood    = tag("carrot", "golden_carrot", "dandelion")
	TurtleFood    = tag("seagrass")
	FrogFood      = tag("slime_ball")
	ArmadilloFood = tag("spider_eye")
	SnifferFood   = tag("torchflower_seeds")
	BeeFood       = tag("dandelion", "open_eyeblossom", "poppy", "blue_orchid", "allium", "azure_bluet", "red_tulip",
		"orange_tulip", "white_tulip", "pink_tulip", "oxeye_daisy", "cornflower", "lily_of_the_valley", "wither_rose",
		"torchflower", "sunflower", "lilac", "peony", "rose_bush", "pitcher_plant", "flowering_azalea_leaves",
		"flowering_azalea", "mangrove_propagule", "cherry_leaves", "pink_petals", "wildflowers", "chorus_flower",
		"spore_blossom", "cactus_flower")
)

// bedrockToJava are the item names that differ between the editions (Bedrock name -> Java name).
var bedrockToJava = map[string]string{
	"azalea_leaves_flowered": "flowering_azalea_leaves",
}

// JavaItemName is the Java name of it without the minecraft: prefix ("" for nil).
func JavaItemName(it world.Item) string {
	if it == nil {
		return ""
	}
	name, _ := it.EncodeItem()
	name = strings.TrimPrefix(name, "minecraft:")
	if j, ok := bedrockToJava[name]; ok {
		return j
	}
	return name
}

// Has reports whether it is in the tag (ItemStack.is(TagKey)).
func (t ItemTag) Has(it world.Item) bool {
	if it == nil {
		return false
	}
	_, ok := t[JavaItemName(it)]
	return ok
}
