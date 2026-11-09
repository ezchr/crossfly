//go:build crossfly_full

package mob

// removeInPeacefulTypes are the mobs removed while the difficulty is
// peaceful: those with BDS's RemoveInPeacefulFlagComponent, which the
// Monster constructor adds to every Monster subclass (Monster::normalTick
// removes them on peaceful; Slime::preNormalTick does the same for slimes
// and magma cubes) and the Piglin and EnderDragon constructors take away
// again. The classes come from VanillaBuiltInEntities::registerMappings and
// registerVanillaActorData (1.21.3).
var removeInPeacefulTypes = map[string]bool{
	// Zombie (and its ZombieVillager and PigZombie subclasses).
	"minecraft:zombie": true, "minecraft:husk": true, "minecraft:drowned": true,
	"minecraft:zombie_villager": true, "minecraft:zombie_villager_v2": true, "minecraft:zombie_pigman": true,
	// Skeleton, and Monster itself.
	"minecraft:skeleton": true, "minecraft:stray": true, "minecraft:wither_skeleton": true,
	"minecraft:bogged": true, "minecraft:zoglin": true,
	// Slime and LavaSlime.
	"minecraft:slime": true, "minecraft:magma_cube": true,
	// The other Monster subclasses.
	"minecraft:creeper": true, "minecraft:spider": true, "minecraft:cave_spider": true,
	"minecraft:enderman": true, "minecraft:silverfish": true, "minecraft:endermite": true,
	"minecraft:witch": true, "minecraft:blaze": true, "minecraft:ghast": true,
	"minecraft:guardian": true, "minecraft:elder_guardian": true, "minecraft:phantom": true,
	"minecraft:vindicator": true, "minecraft:evocation_illager": true, "minecraft:pillager": true,
	"minecraft:ravager": true, "minecraft:vex": true, "minecraft:warden": true,
	"minecraft:breeze": true, "minecraft:wither": true,
	// Added after 1.21.3, Java fallback: no Bedrock data. Java's Parched is
	// an AbstractSkeleton and its Creaking a Monster (Monster.shouldDespawnInPeaceful).
	"minecraft:parched": true, "minecraft:creaking": true,
}
