package jpoi

import (
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/world"
)

// Type is a point of interest type (Java PoiType + its registry key):
// MaxTickets is how many mobs may claim one record, ValidRange how close a
// mob must get to use it.
type Type struct {
	Name       string
	MaxTickets int
	ValidRange int
	// ordinal is the registration order (PoiTypes.bootstrap).
	ordinal int
}

// String implements fmt.Stringer.
func (t *Type) String() string { return t.Name }

// The vanilla PoI types (PoiTypes.bootstrap), with their ticket counts and
// valid ranges.
var (
	Armorer       = &Type{Name: "minecraft:armorer", MaxTickets: 1, ValidRange: 1, ordinal: 0}
	Butcher       = &Type{Name: "minecraft:butcher", MaxTickets: 1, ValidRange: 1, ordinal: 1}
	Cartographer  = &Type{Name: "minecraft:cartographer", MaxTickets: 1, ValidRange: 1, ordinal: 2}
	Cleric        = &Type{Name: "minecraft:cleric", MaxTickets: 1, ValidRange: 1, ordinal: 3}
	Farmer        = &Type{Name: "minecraft:farmer", MaxTickets: 1, ValidRange: 1, ordinal: 4}
	Fisherman     = &Type{Name: "minecraft:fisherman", MaxTickets: 1, ValidRange: 1, ordinal: 5}
	Fletcher      = &Type{Name: "minecraft:fletcher", MaxTickets: 1, ValidRange: 1, ordinal: 6}
	Leatherworker = &Type{Name: "minecraft:leatherworker", MaxTickets: 1, ValidRange: 1, ordinal: 7}
	Librarian     = &Type{Name: "minecraft:librarian", MaxTickets: 1, ValidRange: 1, ordinal: 8}
	Mason         = &Type{Name: "minecraft:mason", MaxTickets: 1, ValidRange: 1, ordinal: 9}
	Shepherd      = &Type{Name: "minecraft:shepherd", MaxTickets: 1, ValidRange: 1, ordinal: 10}
	Toolsmith     = &Type{Name: "minecraft:toolsmith", MaxTickets: 1, ValidRange: 1, ordinal: 11}
	Weaponsmith   = &Type{Name: "minecraft:weaponsmith", MaxTickets: 1, ValidRange: 1, ordinal: 12}
	Home          = &Type{Name: "minecraft:home", MaxTickets: 1, ValidRange: 1, ordinal: 13}
	Meeting       = &Type{Name: "minecraft:meeting", MaxTickets: 32, ValidRange: 6, ordinal: 14}
	Beehive       = &Type{Name: "minecraft:beehive", MaxTickets: 0, ValidRange: 1, ordinal: 15}
	BeeNest       = &Type{Name: "minecraft:bee_nest", MaxTickets: 0, ValidRange: 1, ordinal: 16}
	NetherPortal  = &Type{Name: "minecraft:nether_portal", MaxTickets: 0, ValidRange: 1, ordinal: 17}
	Lodestone     = &Type{Name: "minecraft:lodestone", MaxTickets: 0, ValidRange: 1, ordinal: 18}
	LightningRod  = &Type{Name: "minecraft:lightning_rod", MaxTickets: 0, ValidRange: 1, ordinal: 20}
)

// AllTypes lists the vanilla types in registration order.
var AllTypes = []*Type{Armorer, Butcher, Cartographer, Cleric, Farmer, Fisherman, Fletcher, Leatherworker, Librarian, Mason, Shepherd, Toolsmith, Weaponsmith, Home, Meeting, Beehive, BeeNest, NetherPortal, Lodestone, LightningRod}

// IsAcquirableJobSite is the #minecraft:acquirable_job_site tag
// (VillagerProfession.ALL_ACQUIRABLE_JOBS).
func IsAcquirableJobSite(t *Type) bool { return t != nil && t.ordinal <= Weaponsmith.ordinal }

// IsVillage is the #minecraft:village tag: job sites, home and meeting.
func IsVillage(t *Type) bool { return t != nil && t.ordinal <= Meeting.ordinal }

// IsBeeHome is the #minecraft:bee_home tag.
func IsBeeHome(t *Type) bool { return t == Beehive || t == BeeNest }

// Is returns a predicate matching exactly t (Holder.is(key)).
func Is(t *Type) func(*Type) bool { return func(x *Type) bool { return x == t } }

// Any matches every type.
func Any(*Type) bool { return true }

// byBlockName maps the Bedrock block names Crossfly uses to Java's PoI
// types (PoiTypes.bootstrap: every state of the block, except beds: only the
// head part).
var byBlockName = map[string]*Type{
	"minecraft:blast_furnace":     Armorer,
	"minecraft:lit_blast_furnace": Armorer,
	"minecraft:smoker":            Butcher,
	"minecraft:lit_smoker":        Butcher,
	"minecraft:cartography_table": Cartographer,
	"minecraft:brewing_stand":     Cleric,
	"minecraft:composter":         Farmer,
	"minecraft:barrel":            Fisherman,
	"minecraft:fletching_table":   Fletcher,
	"minecraft:cauldron":          Leatherworker,
	"minecraft:lava_cauldron":     Leatherworker,
	"minecraft:lectern":           Librarian,
	"minecraft:stonecutter_block": Mason,
	"minecraft:stonecutter":       Mason,
	"minecraft:loom":              Shepherd,
	"minecraft:smithing_table":    Toolsmith,
	"minecraft:grindstone":        Weaponsmith,
	"minecraft:bed":               Home,
	"minecraft:bell":              Meeting,
	"minecraft:beehive":           Beehive,
	"minecraft:bee_nest":          BeeNest,
	"minecraft:portal":            NetherPortal,
	"minecraft:lodestone":         Lodestone,
}

// TypeOf is PoiTypes.forState: the PoI type of block b, if any.
func TypeOf(b world.Block) (*Type, bool) {
	if b == nil {
		return nil, false
	}
	name, props := b.EncodeBlock()
	t, ok := byBlockName[name]
	if !ok {
		if strings.HasSuffix(name, "lightning_rod") {
			return LightningRod, true
		}
		return nil, false
	}
	if t == Home {
		// Only the head of a bed is a home (BedPart.HEAD).
		switch v := props["head_piece_bit"].(type) {
		case uint8:
			return t, v == 1
		case bool:
			return t, v
		}
		return nil, false
	}
	return t, true
}

var (
	statesOnce sync.Once
	poiStates  []world.Block
)

// states returns every registered block state that is a PoI, for palette
// searches.
func states() []world.Block {
	statesOnce.Do(func() {
		for _, b := range world.Blocks() {
			if _, ok := TypeOf(b); ok {
				poiStates = append(poiStates, b)
			}
		}
	})
	return poiStates
}
