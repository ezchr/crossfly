//go:build crossfly_full

package javaspawn

// EntityType is what spawning reads of a net.minecraft.world.entity.EntityType
// (EntityType.Builder settings in EntityTypes).
type EntityType struct {
	// ID is the entity type id, e.g. "minecraft:zombie".
	ID       string
	Category Category
	// Width and Height are the base dimensions (sized()).
	Width, Height float64
	// FireImmune is fireImmune(): magma blocks are valid spawn floors and
	// burning blocks are not dangerous.
	FireImmune bool
	// AllowedInPeaceful is false for notInPeaceful() types.
	AllowedInPeaceful bool
	// CanSpawnFarFromPlayer is canSpawnFarFromPlayer().
	CanSpawnFarFromPlayer bool
	// CanSummon is false for noSummon() types.
	CanSummon bool
	// ImmuneTo is the block tag of immuneTo() (default_immune_to).
	ImmuneTo string
}

// Type returns the vanilla 26.3 entity type of a Java id ("minecraft:cow").
func Type(id string) (*EntityType, bool) {
	t, ok := entityTypes[id]
	return t, ok
}

// Spawner is a MobSpawnSettings.SpawnerData with its weight: the type and
// its pack size (count, a constant or uniform int provider).
type Spawner struct {
	Type     string
	Weight   int
	Min, Max int
}

// SpawnCost is MobSpawnSettings.MobSpawnCost (soul sand valley, warped
// forest): the potential charge a mob adds and the energy budget of a spawn.
type SpawnCost struct{ Charge, EnergyBudget float64 }

// MobSpawnSettings is a biome's gameplay/natural_mob_spawns.
type MobSpawnSettings struct {
	Spawns [numCategories][]Spawner
	Costs  map[string]SpawnCost
}

// StructureOverride is a StructureSpawnOverride: the spawn list used inside
// a structure's pieces (Piece) or its whole bounding box.
type StructureOverride struct {
	Piece  bool
	Spawns []Spawner
}

// weightedPick is WeightedList.getRandom.
func weightedPick(list []Spawner, rnd randSource) (Spawner, bool) {
	total := 0
	for _, s := range list {
		total += s.Weight
	}
	if total <= 0 {
		return Spawner{}, false
	}
	v := rnd.IntN(total)
	for _, s := range list {
		if v -= s.Weight; v < 0 {
			return s, true
		}
	}
	return Spawner{}, false
}

// sampleCount is the SpawnerData count (UniformInt.sample).
func (s Spawner) sampleCount(rnd randSource) int {
	if s.Max <= s.Min {
		return s.Min
	}
	return s.Min + rnd.IntN(s.Max-s.Min+1)
}

// randSource is the subset of *rand.Rand spawning uses.
type randSource interface {
	IntN(n int) int
	Float32() float32
	Float64() float64
}
