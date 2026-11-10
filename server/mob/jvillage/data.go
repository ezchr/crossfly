//go:build crossfly_full

package jvillage

import (
	"github.com/df-mc/dragonfly/server/mob/jpoi"
)

// Profession is Java's VillagerProfession.
type Profession struct {
	// Name is the registry path ("farmer").
	Name string
	// JobSite is the PoI type of its workstation (nil for none and nitwit).
	JobSite *jpoi.Type
	// RequestedItems are the Java item names the villager also picks up
	// (farmer: wheat, seeds, bone meal).
	RequestedItems []string
	// SecondaryPoi are the Java block names it works at besides its job site
	// (farmer: farmland).
	SecondaryPoi []string
	// WorkSound is the Java sound event played when working.
	WorkSound string
	// Trades says whether it has trade sets per level (<name>/level_N).
	Trades bool
}

// The vanilla professions (VillagerProfession.bootstrap).
var (
	ProfNone          = &Profession{Name: "none"}
	ProfArmorer       = &Profession{Name: "armorer", JobSite: jpoi.Armorer, WorkSound: "entity.villager.work_armorer", Trades: true}
	ProfButcher       = &Profession{Name: "butcher", JobSite: jpoi.Butcher, WorkSound: "entity.villager.work_butcher", Trades: true}
	ProfCartographer  = &Profession{Name: "cartographer", JobSite: jpoi.Cartographer, WorkSound: "entity.villager.work_cartographer", Trades: true}
	ProfCleric        = &Profession{Name: "cleric", JobSite: jpoi.Cleric, WorkSound: "entity.villager.work_cleric", Trades: true}
	ProfFarmer        = &Profession{Name: "farmer", JobSite: jpoi.Farmer, RequestedItems: []string{"wheat", "wheat_seeds", "beetroot_seeds", "bone_meal"}, SecondaryPoi: []string{"farmland"}, WorkSound: "entity.villager.work_farmer", Trades: true}
	ProfFisherman     = &Profession{Name: "fisherman", JobSite: jpoi.Fisherman, WorkSound: "entity.villager.work_fisherman", Trades: true}
	ProfFletcher      = &Profession{Name: "fletcher", JobSite: jpoi.Fletcher, WorkSound: "entity.villager.work_fletcher", Trades: true}
	ProfLeatherworker = &Profession{Name: "leatherworker", JobSite: jpoi.Leatherworker, WorkSound: "entity.villager.work_leatherworker", Trades: true}
	ProfLibrarian     = &Profession{Name: "librarian", JobSite: jpoi.Librarian, WorkSound: "entity.villager.work_librarian", Trades: true}
	ProfMason         = &Profession{Name: "mason", JobSite: jpoi.Mason, WorkSound: "entity.villager.work_mason", Trades: true}
	ProfNitwit        = &Profession{Name: "nitwit"}
	ProfShepherd      = &Profession{Name: "shepherd", JobSite: jpoi.Shepherd, WorkSound: "entity.villager.work_shepherd", Trades: true}
	ProfToolsmith     = &Profession{Name: "toolsmith", JobSite: jpoi.Toolsmith, WorkSound: "entity.villager.work_toolsmith", Trades: true}
	ProfWeaponsmith   = &Profession{Name: "weaponsmith", JobSite: jpoi.Weaponsmith, WorkSound: "entity.villager.work_weaponsmith", Trades: true}
)

// Professions lists them in registration order.
var Professions = []*Profession{ProfNone, ProfArmorer, ProfButcher, ProfCartographer, ProfCleric, ProfFarmer, ProfFisherman, ProfFletcher, ProfLeatherworker, ProfLibrarian, ProfMason, ProfNitwit, ProfShepherd, ProfToolsmith, ProfWeaponsmith}

// ProfessionByName looks up a profession by path or "minecraft:" name.
func ProfessionByName(name string) (*Profession, bool) {
	name = trimNS(name)
	for _, p := range Professions {
		if p.Name == name {
			return p, true
		}
	}
	return nil, false
}

// HeldJobSite is VillagerProfession.heldJobSite: the PoI type a villager of
// this profession keeps as JOB_SITE. None accepts any acquirable job site,
// nitwit none.
func (p *Profession) HeldJobSite(t *jpoi.Type) bool {
	if p == ProfNone {
		return false
	}
	return p.JobSite != nil && t == p.JobSite
}

// AcquirableJobSite is VillagerProfession.acquirableJobSite.
func (p *Profession) AcquirableJobSite(t *jpoi.Type) bool {
	if p == ProfNone {
		return jpoi.IsAcquirableJobSite(t)
	}
	return p.JobSite != nil && t == p.JobSite
}

// ProfessionForJobSite is the profession whose job site t is
// (AssignProfessionFromJobSite looks it up).
func ProfessionForJobSite(t *jpoi.Type) (*Profession, bool) {
	for _, p := range Professions {
		if p.JobSite != nil && p.JobSite == t {
			return p, true
		}
	}
	return nil, false
}

// TradeSet returns the trade set key of the profession at level ("farmer/level_1").
func (p *Profession) TradeSet(level int) (string, bool) {
	if !p.Trades || level < 1 || level > 5 {
		return "", false
	}
	return p.Name + "/level_" + string(rune('0'+level)), true
}

// The villager types (VillagerType).
const (
	TypeDesert  = "desert"
	TypeJungle  = "jungle"
	TypePlains  = "plains"
	TypeSavanna = "savanna"
	TypeSnow    = "snow"
	TypeSwamp   = "swamp"
	TypeTaiga   = "taiga"
)

// VillagerTypes lists the types in registration order.
var VillagerTypes = []string{TypeDesert, TypeJungle, TypePlains, TypeSavanna, TypeSnow, TypeSwamp, TypeTaiga}

// typeByBiome is VillagerType.BY_BIOME (Java biome paths).
var typeByBiome = map[string]string{
	"badlands": TypeDesert, "desert": TypeDesert, "eroded_badlands": TypeDesert, "wooded_badlands": TypeDesert,
	"bamboo_jungle": TypeJungle, "jungle": TypeJungle, "sparse_jungle": TypeJungle,
	"savanna_plateau": TypeSavanna, "savanna": TypeSavanna, "windswept_savanna": TypeSavanna,
	"deep_frozen_ocean": TypeSnow, "frozen_ocean": TypeSnow, "frozen_river": TypeSnow, "ice_spikes": TypeSnow,
	"snowy_beach": TypeSnow, "snowy_taiga": TypeSnow, "snowy_plains": TypeSnow, "grove": TypeSnow,
	"snowy_slopes": TypeSnow, "frozen_peaks": TypeSnow, "jagged_peaks": TypeSnow,
	"swamp": TypeSwamp, "mangrove_swamp": TypeSwamp,
	"old_growth_spruce_taiga": TypeTaiga, "old_growth_pine_taiga": TypeTaiga, "windswept_gravelly_hills": TypeTaiga,
	"windswept_hills": TypeTaiga, "taiga": TypeTaiga, "windswept_forest": TypeTaiga,
}

// TypeByBiome is VillagerType.byBiome for a Java biome name; plains for any
// other biome.
func TypeByBiome(biome string) string {
	if t, ok := typeByBiome[trimNS(biome)]; ok {
		return t
	}
	return TypePlains
}

// bedrockBiomeAliases maps Bedrock biome names (Dragonfly's) whose Java
// name differs.
var bedrockBiomeAliases = map[string]string{
	"mesa": "badlands", "mesa_bryce": "eroded_badlands", "mesa_plateau_stone": "wooded_badlands",
	"bamboo_jungle": "bamboo_jungle", "jungle_edge": "sparse_jungle",
	"savanna_plateau": "savanna_plateau", "savanna_mutated": "windswept_savanna",
	"deep_frozen_ocean": "deep_frozen_ocean", "frozen_ocean": "frozen_ocean", "frozen_river": "frozen_river",
	"ice_plains_spikes": "ice_spikes", "cold_beach": "snowy_beach", "cold_taiga": "snowy_taiga",
	"ice_plains": "snowy_plains", "grove": "grove", "snowy_slopes": "snowy_slopes", "frozen_peaks": "frozen_peaks",
	"jagged_peaks": "jagged_peaks", "swampland": "swamp", "mangrove_swamp": "mangrove_swamp",
	"redwood_taiga_mutated": "old_growth_spruce_taiga", "mega_taiga": "old_growth_pine_taiga",
	"extreme_hills_plus_trees_mutated": "windswept_gravelly_hills", "extreme_hills_mutated": "windswept_gravelly_hills",
	"extreme_hills": "windswept_hills", "extreme_hills_plus_trees": "windswept_forest",
}

// TypeByBedrockBiome maps a Dragonfly (Bedrock) biome name to the villager
// type.
func TypeByBedrockBiome(name string) string {
	name = trimNS(name)
	if j, ok := bedrockBiomeAliases[name]; ok {
		name = j
	}
	return TypeByBiome(name)
}

// Data is Java's VillagerData: type, profession and level (1..5).
type Data struct {
	Type       string
	Profession *Profession
	Level      int
}

// DefaultData is Villager.createDefaultVillagerData.
func DefaultData() Data { return Data{Type: TypePlains, Profession: ProfNone, Level: 1} }

// nextLevelXP is VillagerData.NEXT_LEVEL_XP_THRESHOLDS.
var nextLevelXP = [...]int{0, 10, 70, 150, 250}

// CanLevelUp is VillagerData.canLevelUp.
func CanLevelUp(level int) bool { return level >= 1 && level < 5 }

// MinXPPerLevel is VillagerData.getMinXpPerLevel.
func MinXPPerLevel(level int) int {
	if CanLevelUp(level) {
		return nextLevelXP[level-1]
	}
	return 0
}

// MaxXPPerLevel is VillagerData.getMaxXpPerLevel.
func MaxXPPerLevel(level int) int {
	if CanLevelUp(level) {
		return nextLevelXP[level]
	}
	return 0
}

// NBT encodes d as Java's VillagerData compound.
func (d Data) NBT() map[string]any {
	p := d.Profession
	if p == nil {
		p = ProfNone
	}
	return map[string]any{"type": "minecraft:" + d.Type, "profession": "minecraft:" + p.Name, "level": int32(max(1, d.Level))}
}

// DataFromNBT decodes a VillagerData compound (missing fields default as in
// Java's codec).
func DataFromNBT(m map[string]any) Data {
	d := DefaultData()
	if s, ok := m["type"].(string); ok {
		for _, t := range VillagerTypes {
			if t == trimNS(s) {
				d.Type = t
			}
		}
	}
	if s, ok := m["profession"].(string); ok {
		if p, ok := ProfessionByName(s); ok {
			d.Profession = p
		}
	}
	if l, ok := m["level"].(int32); ok {
		d.Level = max(1, int(l))
	}
	return d
}

func trimNS(s string) string {
	if len(s) > 10 && s[:10] == "minecraft:" {
		return s[10:]
	}
	return s
}
