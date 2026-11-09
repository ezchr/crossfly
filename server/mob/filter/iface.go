//go:build crossfly_full

package filter

import (
	"math/rand/v2"

	"github.com/go-gl/mathgl/mgl64"
)

// Actor is what a filter subject has to provide. The mob type, players and
// any other entity that can appear as self/other/target/... implement it.
//
// Everything beyond these five methods is optional: a test that needs a
// capability the subject does not implement sees the zero value (false, 0, no
// items, no relation) and applies its operator to that, which matches what
// BDS does for actors that lack the matching component.
type Actor interface {
	// Position is the actor's feet position. Environment tests (biome tag,
	// brightness, weather, ...) are evaluated at this position.
	Position() mgl64.Vec3
	// HasFamily reports whether minecraft:type_family contains the family.
	HasFamily(family string) bool
	// HasComponent reports whether the actor currently has the component
	// (full name, e.g. "minecraft:is_baby"), as the active component group
	// stack defines it.
	HasComponent(name string) bool
	// Flag answers one boolean state question; unknown flags return false.
	Flag(f Flag) bool
	// Number answers one numeric question; ok=false means "not applicable"
	// (no owner for OwnerDistance, no home for HomeDistance, ...).
	Number(n Number) (v float64, ok bool)
}

// Flag is a boolean state of an actor used by filter tests.
type Flag uint8

// Flags. Each maps to the filter test(s) named in its comment.
const (
	FlagNone                 Flag = iota
	FlagInWater                   // in_water
	FlagInLava                    // in_lava
	FlagUnderwater                // is_underwater (head fully in water)
	FlagInWaterOrRain             // in_water_or_rain
	FlagInContactWithWater        // in_contact_with_water (water, rain, splash bottle)
	FlagOnGround                  // on_ground
	FlagOnFire                    // on_fire
	FlagOnHotBlock                // on_hot_block
	FlagTakingFireDamage          // taking_fire_damage
	FlagBaby                      // is_baby
	FlagRiding                    // is_riding
	FlagSneaking                  // is_sneaking
	FlagSneakHeld                 // is_sneak_held
	FlagSprinting                 // is_sprinting
	FlagSleeping                  // is_sleeping
	FlagSitting                   // is_sitting
	FlagMoving                    // is_moving
	FlagClimbing                  // is_climbing
	FlagOnLadder                  // on_ladder
	FlagImmobile                  // is_immobile
	FlagTamed                     // is_tamed
	FlagLeashed                   // is_leashed
	FlagPersistent                // is_persistent
	FlagPanicking                 // is_panicking
	FlagAvoidingMobs              // is_avoiding_mobs
	FlagRaider                    // is_raider
	FlagInCaravan                 // in_caravan
	FlagVisible                   // is_visible
	FlagNavigating                // is_navigating
	FlagContainerOpen             // has_container_open
	FlagNameTag                   // has_nametag
	FlagRangedWeapon              // has_ranged_weapon
	FlagTradeSupply               // has_trade_supply
	FlagInVillage                 // is_in_village
	FlagBoundToCreakingHeart      // is_bound_to_creaking_heart
	FlagSurfaceMob                // surface_mob
	FlagInClouds                  // in_clouds
)

// Number is a numeric attribute of an actor used by filter tests.
type Number uint8

// Numbers.
const (
	NumberNone              Number = iota
	NumberHealth                   // actor_health (current health)
	NumberMaxHealth                // is_missing_health
	NumberRiderCount               // rider_count
	NumberVariant                  // is_variant (minecraft:variant)
	NumberMarkVariant              // is_mark_variant
	NumberSkinID                   // is_skin_id
	NumberYRotation                // y_rotation (body yaw, degrees)
	NumberHomeDistance             // home_distance
	NumberInactivitySeconds        // inactivity_timer
)

// Relation is another actor linked to an actor.
type Relation uint8

// Relations.
const (
	RelationNone                 Relation = iota
	RelationTarget                        // has_target, target_distance, is_target, trigger target
	RelationOwner                         // owner_distance, is_owner
	RelationVehicle                       // is_vehicle_family, is_riding_self
	RelationControllingPassenger          // is_controlling_passenger_family
	RelationLeashHolder                   // is_leashed_to
	RelationLastHurtBy                    // was_last_hurt_by (BDS: last player within 5 s or mob within 3 s)
)

// Related is implemented by actors that know other actors.
type Related interface {
	Related(r Relation) Actor
}

// Domain is an equipment location, as used by has_equipment & co. The values
// match BDS (0 any, 1 hand, 2 armor, 3 head, 4 torso, 5 leg, 6 feet,
// 7 inventory, 8 body); main_hand and off_hand were added later.
type Domain uint8

// Domains.
const (
	DomainAny Domain = iota
	DomainHand
	DomainArmor
	DomainHead
	DomainTorso
	DomainLeg
	DomainFeet
	DomainInventory
	DomainBody
	DomainMainHand
	DomainOffHand
	domainInvalid Domain = 255
)

// Item is a stack seen by equipment tests.
type Item struct {
	// Name is the namespaced identifier, e.g. "minecraft:shears".
	Name string
	// Aux is the legacy data value; has_equipment values like "bucket:0" match it.
	Aux int
	// Damage is the durability damage taken (has_damaged_equipment).
	Damage int
	// Tags are item tags, e.g. "minecraft:is_axe".
	Tags []string
	// Enchantments are enchantment names, with or without namespace.
	Enchantments []string
}

// Equipped is implemented by actors with equipment or an inventory. Return
// the non-empty stacks in the domain; for DomainAny return all equipped
// items (hands + armor + body), for DomainInventory the container contents.
type Equipped interface {
	Equipment(d Domain) []Item
}

// SlotChecker is optional for all_slots_empty / any_slot_empty: it reports
// whether every / any slot of the domain is empty. Without it the tests
// derive the answer from Equipment (any_slot_empty then cannot see empty
// container slots).
type SlotChecker interface {
	SlotsEmpty(d Domain, all bool) bool
}

// PropertyHolder is implemented by actors with description.properties.
// The value is int32, float32, bool or string (enum).
type PropertyHolder interface {
	PropertyValue(name string) (any, bool)
}

// Effects is implemented by actors with mob effects. name has no namespace
// ("weakness"); an empty name asks for any effect.
type Effects interface {
	HasEffect(name string) bool
}

// Abilities is implemented by players (has_ability: instabuild, mayfly, ...).
type Abilities interface {
	HasAbility(name string) bool
}

// Tagged is implemented by actors with /tag tags.
type Tagged interface {
	HasTag(tag string) bool
}

// Coloured is implemented by actors with minecraft:color (is_color). Return
// the colour name as Bedrock spells it: white, orange, magenta, light_blue,
// yellow, lime, pink, gray, silver, cyan, purple, blue, brown, green, red, black.
type Coloured interface {
	Colour() string
}

// Trusting is implemented by actors with minecraft:trust (trusts).
type Trusting interface {
	Trusts(other Actor) bool
}

// InBlock is optional for in_block: report whether the actor's box
// intersects a block with this name. Without it World.BlockName at the
// actor's position is used.
type InBlock interface {
	InBlock(name string) bool
}

// Block is the block subject (subject "block"), used by is_block and
// is_waterlogged and as the position of environment tests.
type Block interface {
	Name() string
	Waterlogged() bool
	Position() mgl64.Vec3
}

// Weather describes the weather at a place.
type Weather struct {
	Precipitation bool // raining or snowing
	Snow          bool // precipitation falls as snow here
	Thunder       bool
}

// World answers environment questions. Embed NopWorld to implement only a
// subset.
type World interface {
	// Time is the absolute world time in ticks.
	Time() int64
	// Daytime is Dimension::isDay.
	Daytime() bool
	// Difficulty is 0 peaceful, 1 easy, 2 normal, 3 hard.
	Difficulty() int
	// GameRule returns a boolean game rule (lower-case name).
	GameRule(name string) bool
	// Dimension is "overworld", "nether" or "the_end".
	Dimension() string
	Brightness(pos mgl64.Vec3) float64
	LightLevel(pos mgl64.Vec3) int
	BiomeHasTag(pos mgl64.Vec3, tag string) bool
	Temperature(pos mgl64.Vec3) float64
	SnowCovered(pos mgl64.Vec3) bool
	Humid(pos mgl64.Vec3) bool
	// Underground reports that the sky is not visible from pos.
	Underground(pos mgl64.Vec3) bool
	WeatherAt(pos mgl64.Vec3) Weather
	DimensionWeather() Weather
	// BlockName is the block at pos ("minecraft:powder_snow").
	BlockName(pos mgl64.Vec3) string
	// NearestPlayerDistance returns ok=false when there is no player.
	NearestPlayerDistance(pos mgl64.Vec3) (float64, bool)
	RedstonePower(pos mgl64.Vec3) int
}

// NopWorld implements World with zero answers.
type NopWorld struct{}

func (NopWorld) Time() int64                                      { return 0 }
func (NopWorld) Daytime() bool                                    { return false }
func (NopWorld) Difficulty() int                                  { return 2 }
func (NopWorld) GameRule(string) bool                             { return false }
func (NopWorld) Dimension() string                                { return "overworld" }
func (NopWorld) Brightness(mgl64.Vec3) float64                    { return 0 }
func (NopWorld) LightLevel(mgl64.Vec3) int                        { return 0 }
func (NopWorld) BiomeHasTag(mgl64.Vec3, string) bool              { return false }
func (NopWorld) Temperature(mgl64.Vec3) float64                   { return 0.8 }
func (NopWorld) SnowCovered(mgl64.Vec3) bool                      { return false }
func (NopWorld) Humid(mgl64.Vec3) bool                            { return false }
func (NopWorld) Underground(mgl64.Vec3) bool                      { return false }
func (NopWorld) WeatherAt(mgl64.Vec3) Weather                     { return Weather{} }
func (NopWorld) DimensionWeather() Weather                        { return Weather{} }
func (NopWorld) BlockName(mgl64.Vec3) string                      { return "minecraft:air" }
func (NopWorld) NearestPlayerDistance(mgl64.Vec3) (float64, bool) { return 0, false }
func (NopWorld) RedstonePower(mgl64.Vec3) int                     { return 0 }

// Damage describes the damage being handled by a damage_sensor, for has_damage.
type Damage struct {
	// Cause is the Bedrock damage cause name: "fall", "fire", "lava",
	// "entity_attack", "projectile", "lightning", "magma", ...
	Cause string
	// Fatal reports that the damage will kill the actor.
	Fatal bool
}

// Context is the set of subjects and world state a filter is evaluated in.
// It mirrors BDS's VariantParameterList: Self is always set, the other
// subjects only when the caller (sensor, interaction, damage, ...) has them.
type Context struct {
	Self    Actor
	Other   Actor
	Player  Actor
	Target  Actor
	Parent  Actor
	Baby    Actor
	Damager Actor
	// Block is the block subject; environment tests with subject "block" use
	// its position while actor tests still see Self (as BDS does).
	Block Block
	// World answers environment tests. nil makes them false.
	World World
	// Rand drives random_chance and event randomize. nil uses the global source.
	Rand *rand.Rand
	// Damage is set while evaluating damage_sensor triggers.
	Damage *Damage
}

// Subject returns the actor for s, or nil if the context does not have one.
func (c *Context) Subject(s Subject) Actor {
	switch s {
	case SubjectSelf, SubjectBlock:
		return c.Self
	case SubjectOther:
		return c.Other
	case SubjectPlayer:
		return c.Player
	case SubjectTarget:
		return c.Target
	case SubjectParent:
		return c.Parent
	case SubjectBaby:
		return c.Baby
	case SubjectDamager:
		return c.Damager
	}
	return nil
}

// WithSelf returns a copy of the context with Self replaced. It is what an
// event trigger does when it runs an event on another actor.
func (c *Context) WithSelf(a Actor) *Context {
	cp := *c
	cp.Self = a
	return &cp
}

func (c *Context) intN(n int) int {
	if n <= 0 {
		return 0
	}
	if c.Rand != nil {
		return c.Rand.IntN(n)
	}
	return rand.IntN(n)
}
