//go:build crossfly_full

package javamob

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Category is a MobCategory: what a mob counts against when mobs spawn, and how far from players it despawns.
type Category struct {
	// Name is the category's serialised name ("monster").
	Name string
	// Max is the spawn cap (MobCategory.getMaxInstancesPerChunk); -1 for misc.
	Max int
	// Friendly mobs spawn in peaceful; Persistent ones are not despawned for being far away
	// (MobCategory.isFriendly, isPersistent).
	Friendly, Persistent bool
	// DespawnDistance is the distance beyond which a mob despawns at once, NoDespawnDistance the one within which
	// it never despawns (getDespawnDistance, getNoDespawnDistance: 32).
	DespawnDistance, NoDespawnDistance int
}

// The categories (MobCategory, 26.3).
var (
	CategoryMonster                  = Category{"monster", 70, false, false, 128, 32}
	CategoryCreature                 = Category{"creature", 10, true, true, 128, 32}
	CategoryAmbient                  = Category{"ambient", 15, true, false, 128, 32}
	CategoryAxolotls                 = Category{"axolotls", 5, true, false, 128, 32}
	CategoryUndergroundWaterCreature = Category{"underground_water_creature", 5, true, false, 128, 32}
	CategoryWaterCreature            = Category{"water_creature", 5, true, false, 128, 32}
	CategoryWaterAmbient             = Category{"water_ambient", 20, true, false, 64, 32}
	CategoryMisc                     = Category{"misc", -1, true, true, 128, 32}
)

// SpawnReason is EntitySpawnReason: why a mob is being added. FinalizeSpawn and spawn rules get it.
type SpawnReason uint8

// The spawn reasons, in EntitySpawnReason order.
const (
	SpawnNatural SpawnReason = iota
	SpawnChunkGeneration
	SpawnSpawner
	SpawnStructure
	SpawnBreeding
	SpawnMobSummoned
	SpawnJockey
	SpawnEvent
	SpawnConversion
	SpawnReinforcement
	SpawnTriggered
	SpawnBucket
	SpawnSpawnItemUse
	SpawnCommand
	SpawnDispenser
	SpawnPatrol
	SpawnTrialSpawner
	SpawnLoad
	SpawnDimensionTravel
)

// IsSpawner is EntitySpawnReason.isSpawner: a spawner or trial spawner.
func (r SpawnReason) IsSpawner() bool { return r == SpawnSpawner || r == SpawnTrialSpawner }

// PlacementType is SpawnPlacementType: where a natural spawn may be placed.
type PlacementType uint8

const (
	// PlacementNoRestrictions is SpawnPlacementTypes.NO_RESTRICTIONS.
	PlacementNoRestrictions PlacementType = iota
	// PlacementOnGround is SpawnPlacementTypes.ON_GROUND.
	PlacementOnGround
	// PlacementInWater is SpawnPlacementTypes.IN_WATER.
	PlacementInWater
	// PlacementInLava is SpawnPlacementTypes.IN_LAVA.
	PlacementInLava
)

// Heightmap is a Heightmap.Types value used by spawn placements.
type Heightmap uint8

const (
	HeightmapMotionBlockingNoLeaves Heightmap = iota
	HeightmapMotionBlocking
	HeightmapOceanFloor
	HeightmapWorldSurface
)

// SpawnRule is a SpawnPlacements.SpawnPredicate: whether a mob of the type may spawn at pos for the reason. It runs
// on the world goroutine, inside tx.
type SpawnRule func(tx *world.Tx, t *Type, reason SpawnReason, pos cube.Pos, r *rand.Rand) bool

// Placement is a SpawnPlacements entry of a type: its placement type, heightmap and spawn rule. The natural
// spawning package sets them with SetPlacement; mob packages never do.
type Placement struct {
	Type      PlacementType
	Heightmap Heightmap
	Rule      SpawnRule
}

// Type is one Java mob type: an EntityType with its dimensions and category plus the Go behaviour of the mob
// class. Register it from an init function of the mob's package (README.md).
type Type struct {
	// ID is the entity identifier, "minecraft:zombie". It is also the Bedrock identifier the Bedrock client
	// renders, and the Java entity type the Java client renders (server/java/javasession maps it).
	ID string
	// Category is the MobCategory.
	Category Category
	// Width and Height are EntityType.Builder.sized, EyeHeight eyeHeight (0.85 of Height when 0).
	Width, Height, EyeHeight float64
	// FireImmune is EntityType.Builder.fireImmune.
	FireImmune bool
	// NotInPeaceful is the opposite of EntityType.isAllowedInPeaceful: the mob is removed in peaceful.
	NotInPeaceful bool
	// BurnsInDaylight is EntityTypeTags.BURN_IN_DAYLIGHT (Mob.aiStep burnUndead).
	BurnsInDaylight bool
	// Undead, Arthropod, Illager and Aquatic are the entity type tags the enchantments and some goals read
	// (EntityTypeTags.UNDEAD, ARTHROPOD, ILLAGER, AQUATIC).
	Undead, Arthropod, Illager, Aquatic bool
	// Families are the Bedrock type families the mob also gets (mob.Mob.SetFamilies), so Bedrock-engine mobs
	// and code that reads families see it as they would its Bedrock counterpart ("zombie", "undead",
	// "monster", "mob").
	Families []string
	// Attributes returns the AttributeSupplier of the type (createAttributes). Nil means LivingAttributes.
	Attributes func() map[Attribute]float64
	// LootTable is the default loot table ("entities/zombie"); empty means "entities/<name>". Set NoLootTable
	// for none.
	LootTable   string
	NoLootTable bool
	// XPReward is Mob.xpReward (Monster: 5).
	XPReward int
	// New returns the behaviour of a new mob of the type (its Java class). It must not be nil.
	New func() Behaviour
	// Components are extra mob component keys the definition adds after the Java state, each with the raw JSON
	// {"id": "<ID>"} (a component of another package, such as a mount's mob.Vehicle). Their factories must be
	// registered with mob.RegisterComponent.
	Components []string

	placement Placement
	hasPlace  bool
}

var (
	typesMu sync.RWMutex
	types   = map[string]*Type{}
	order   []string
)

// Register registers a Java mob type. Call it from init only. Registering an identifier twice panics. The type is
// only added to the server when Java mob mode is on (Install).
func Register(t Type) *Type {
	if t.ID == "" || t.New == nil {
		panic("javamob: Register needs ID and New")
	}
	if t.EyeHeight == 0 {
		t.EyeHeight = t.Height * 0.85
	}
	if t.LootTable == "" && !t.NoLootTable {
		t.LootTable = "entities/" + trimNamespace(t.ID)
	}
	typesMu.Lock()
	defer typesMu.Unlock()
	if _, ok := types[t.ID]; ok {
		panic(fmt.Sprintf("javamob: type %v registered twice", t.ID))
	}
	p := &t
	if pl, ok := pendingPlacements[t.ID]; ok {
		p.placement, p.hasPlace = pl, true
	}
	types[t.ID] = p
	order = append(order, t.ID)
	return p
}

// pendingPlacements are placements set before their type registered (init order across packages).
var pendingPlacements = map[string]Placement{}

// SetPlacement sets the spawn placement of a type (SpawnPlacements.register). It may run before or after the type
// registers; call it from init only. It is the natural spawning package's to call.
func SetPlacement(id string, p Placement) {
	typesMu.Lock()
	defer typesMu.Unlock()
	if t, ok := types[id]; ok {
		t.placement, t.hasPlace = p, true
		return
	}
	pendingPlacements[id] = p
}

// Placement returns the type's spawn placement, if one was set (SpawnPlacements.getPlacementType,
// getHeightmapType, checkSpawnRules).
func (t *Type) Placement() (Placement, bool) { return t.placement, t.hasPlace }

// CheckSpawnRules runs the type's spawn rule (SpawnPlacements.checkSpawnRules): true if it has none.
func (t *Type) CheckSpawnRules(tx *world.Tx, reason SpawnReason, pos cube.Pos, r *rand.Rand) bool {
	if !t.hasPlace || t.placement.Rule == nil {
		return true
	}
	return t.placement.Rule(tx, t, reason, pos, r)
}

// TypeOf returns the registered type of an identifier.
func TypeOf(id string) (*Type, bool) {
	typesMu.RLock()
	defer typesMu.RUnlock()
	t, ok := types[id]
	return t, ok
}

// Types returns the registered types in registration order.
func Types() []*Type {
	typesMu.RLock()
	defer typesMu.RUnlock()
	out := make([]*Type, 0, len(order))
	for _, id := range order {
		out = append(out, types[id])
	}
	return out
}

// IDs returns the registered identifiers, sorted.
func IDs() []string {
	typesMu.RLock()
	defer typesMu.RUnlock()
	return slices.Sorted(slices.Values(order))
}

func trimNamespace(id string) string {
	for i := 0; i < len(id); i++ {
		if id[i] == ':' {
			return id[i+1:]
		}
	}
	return id
}
