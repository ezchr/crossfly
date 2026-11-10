//go:build crossfly_full

package combat

// actorAliases are the alternate names BDS accepts for mob identifiers: the
// second name of each ActorMapping in the actor type table
// (EntityTypeFromString looks up both). pig.json still turns the pig into
// "minecraft:pig_zombie", the zombified piglin's old name.
var actorAliases = map[string]string{
	"minecraft:pig_zombie":       "minecraft:zombie_pigman",
	"minecraft:skeletonhorse":    "minecraft:skeleton_horse",
	"minecraft:zombiehorse":      "minecraft:zombie_horse",
	"minecraft:cavespider":       "minecraft:cave_spider",
	"minecraft:magmacube":        "minecraft:magma_cube",
	"minecraft:zombievillager":   "minecraft:zombie_villager",
	"minecraft:skeleton.stray":   "minecraft:stray",
	"minecraft:skeleton.wither":  "minecraft:wither_skeleton",
	"minecraft:guardian.elder":   "minecraft:elder_guardian",
	"minecraft:wither.boss":      "minecraft:wither",
	"minecraft:dragon":           "minecraft:ender_dragon",
	"minecraft:evocationillager": "minecraft:evocation_illager",
}

// ActorID returns the identifier a mob name stands for: id itself, or the
// mob it is an alternate name of (minecraft:pig_zombie ->
// minecraft:zombie_pigman).
func ActorID(id string) string {
	if a, ok := actorAliases[id]; ok {
		return a
	}
	return id
}
