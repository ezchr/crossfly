//go:build crossfly_full

package javaspawn

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Category is net.minecraft.world.entity.MobCategory.
type Category uint8

const (
	CategoryMonster Category = iota
	CategoryCreature
	CategoryAmbient
	CategoryAxolotls
	CategoryUndergroundWaterCreature
	CategoryWaterCreature
	CategoryWaterAmbient
	CategoryMisc
	numCategories
)

// categoryInfo is a MobCategory constant: name, max (the vanilla cap),
// isFriendly, isPersistent and despawnDistance. noDespawnDistance is 32 for
// all of them.
type categoryInfo struct {
	name            string
	max             int
	friendly        bool
	persistent      bool
	despawnDistance int
}

var categories = [numCategories]categoryInfo{
	CategoryMonster:                  {"monster", 70, false, false, 128},
	CategoryCreature:                 {"creature", 10, true, true, 128},
	CategoryAmbient:                  {"ambient", 15, true, false, 128},
	CategoryAxolotls:                 {"axolotls", 5, true, false, 128},
	CategoryUndergroundWaterCreature: {"underground_water_creature", 5, true, false, 128},
	CategoryWaterCreature:            {"water_creature", 5, true, false, 128},
	CategoryWaterAmbient:             {"water_ambient", 20, true, false, 64},
	CategoryMisc:                     {"misc", -1, true, true, 128},
}

// noDespawnDistance is MobCategory.getNoDespawnDistance.
const noDespawnDistance = 32

// String returns the serialized name ("monster", "water_ambient", ...).
func (c Category) String() string {
	if c < numCategories {
		return categories[c].name
	}
	return "unknown"
}

// Friendly is MobCategory.isFriendly.
func (c Category) Friendly() bool { return categories[c].friendly }

// Persistent is MobCategory.isPersistent (creatures: spawned every 400 ticks).
func (c Category) Persistent() bool { return categories[c].persistent }

// DespawnDistance is MobCategory.getDespawnDistance (128, water ambient 64).
func (c Category) DespawnDistance() int { return categories[c].despawnDistance }

// spawningCategories is NaturalSpawner.SPAWNING_CATEGORIES: all but misc,
// in MobCategory order.
var spawningCategories = [...]Category{CategoryMonster, CategoryCreature, CategoryAmbient, CategoryAxolotls,
	CategoryUndergroundWaterCreature, CategoryWaterCreature, CategoryWaterAmbient}

// DefaultSpawnLimits are the per-player caps of Paper's default bukkit.yml
// spawn-limits (monsters 70, animals 10, ambient 15, axolotls 5,
// water-underground-creature 5, water-animals 5, water-ambient 20): the
// vanilla MobCategory maxima. Paper's entities.spawning.spawn-limits default
// to -1 (use these).
var DefaultSpawnLimits = [numCategories]int{70, 10, 15, 5, 5, 5, 20, -1}

// DefaultTicksPerSpawn is bukkit.yml ticks-per (animal-spawns 400, all other
// categories 1): creatures only spawn on game ticks divisible by 400.
var DefaultTicksPerSpawn = [numCategories]int{1, 400, 1, 1, 1, 1, 1, 0}

// DespawnRange is a Paper entities.spawning.despawn-ranges entry. Zero
// fields take the defaults: Hard = the category's despawn distance (128,
// water ambient 64), Soft = 32 (MobCategory.getNoDespawnDistance). Paper's
// default despawn-range-shape is ELLIPSOID, which with equal horizontal and
// vertical limits is a sphere.
type DespawnRange struct{ Soft, Hard int }

// StructureAt is a structure at a position, for the structure spawn lists
// (ChunkGenerator.getMobsAt, NaturalSpawner.isInNetherFortressBounds).
type StructureAt struct {
	// Name is the structure id without namespace: "swamp_hut", "monument",
	// "fortress", "bastion_remnant", "pillager_outpost", ...
	Name string
	// InPiece is true if the position is inside one of the structure's
	// pieces, false if only inside its bounding box.
	InPiece bool
}

// Config configures Java natural spawning in a world. The zero value is
// Paper 26.3 with its default configuration (paper-world-defaults.yml,
// bukkit.yml, spigot.yml, server.properties).
type Config struct {
	// SpawnLimits overrides the per-player cap of categories (bukkit.yml
	// spawn-limits); 0 turns a category off. Unset: DefaultSpawnLimits.
	SpawnLimits map[Category]int
	// TicksPerSpawn overrides bukkit.yml ticks-per: a category spawns on
	// game ticks divisible by it, 0 turns it off. Unset: DefaultTicksPerSpawn.
	TicksPerSpawn map[Category]int
	// DespawnRanges overrides Paper's despawn-ranges per category.
	DespawnRanges map[Category]DespawnRange
	// SimulationDistance is server.properties simulation-distance (default
	// 10): the chunks around a player whose mobs count towards the player's
	// caps, and the furthest chunks spawned in.
	SimulationDistance int
	// MobSpawnRange is spigot.yml mob-spawn-range (default 8, at most 8):
	// chunks whose centre is closer than MobSpawnRange*16 blocks to a
	// player are spawned in.
	MobSpawnRange int
	// SeaLevel overrides the sea level (default: 63 in the overworld, 32
	// in the nether, 0 in the end, as the vanilla noise settings). Water
	// mobs, turtles, ocelots, drowned and guardians read it. Note: a Java
	// superflat world has sea level -63.
	SeaLevel *int
	// Seed is the Java world seed that decides the slime chunks.
	Seed int64
	// SlimeSeed is spigot.yml seed-slime (0: the default 987234911).
	SlimeSeed int64
	// AllChunksAreSlimeChunks is Paper's all-chunks-are-slime-chunks.
	AllChunksAreSlimeChunks bool
	// CountAllMobsForSpawning is Paper's count-all-mobs-for-spawning (off:
	// only naturally spawned mobs count towards the caps).
	CountAllMobsForSpawning bool
	// NoMonsters is the spawn_monsters game rule set to false.
	NoMonsters bool
	// NoPhantoms is the spawn_phantoms game rule set to false.
	NoPhantoms bool
	// Structures, if set, returns the structures at a position for the
	// structure spawn lists (witch huts, monuments, fortresses, outposts).
	// Without it only the biome lists are used.
	Structures func(tx *world.Tx, pos cube.Pos) []StructureAt
	// ManualTick stops the tick hook from running Tick in this world: the
	// caller runs Tick once per world tick (tests).
	ManualTick bool
	// RandSeed seeds the spawner's random source (0: random).
	RandSeed uint64
}

func (c *Config) withDefaults() *Config {
	out := *c
	if out.SimulationDistance <= 0 {
		out.SimulationDistance = 10
	}
	if out.MobSpawnRange <= 0 {
		out.MobSpawnRange = 8
	}
	if out.SlimeSeed == 0 {
		out.SlimeSeed = 987234911
	}
	return &out
}

// spawnLimit is World.getSpawnLimit of a category.
func (c *Config) spawnLimit(cat Category) int {
	if v, ok := c.SpawnLimits[cat]; ok {
		return v
	}
	return DefaultSpawnLimits[cat]
}

func (c *Config) ticksPer(cat Category) int {
	if v, ok := c.TicksPerSpawn[cat]; ok {
		return v
	}
	return DefaultTicksPerSpawn[cat]
}

// despawnRanges returns the hard and soft despawn distance of a category.
func (c *Config) despawnRanges(cat Category) (hard, soft float64) {
	hard, soft = float64(cat.DespawnDistance()), noDespawnDistance
	if c == nil {
		return
	}
	if r, ok := c.DespawnRanges[cat]; ok {
		if r.Hard > 0 {
			hard = float64(r.Hard)
		}
		if r.Soft > 0 {
			soft = float64(r.Soft)
		}
	}
	return
}

// seaLevel is Level.getSeaLevel.
func (c *Config) seaLevel(d world.Dimension) int {
	if c != nil && c.SeaLevel != nil {
		return *c.SeaLevel
	}
	switch d {
	case world.Nether:
		return 32
	case world.End:
		return 0
	}
	return 63
}
