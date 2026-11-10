package item

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// Egg is an item that can be used to craft food items, or as a throwable entity to spawn chicks. Chickens lay
// the egg of their climate variant: white eggs in temperate climates, brown eggs in warm and blue eggs in cold
// ones.
type Egg struct {
	// Variant is the climate variant of the chicken the egg is from.
	Variant EggVariant
}

// EggVariant is the climate variant of a chicken egg, matching the minecraft:climate_variant of the chicken
// that laid it.
type EggVariant uint8

const (
	// EggTemperate is the white egg of temperate chickens.
	EggTemperate EggVariant = iota
	// EggWarm is the brown egg of warm chickens.
	EggWarm
	// EggCold is the blue egg of cold chickens.
	EggCold
)

// EggVariants returns every egg variant.
func EggVariants() []EggVariant {
	return []EggVariant{EggTemperate, EggWarm, EggCold}
}

// String returns the minecraft:climate_variant value of the variant: "temperate", "warm" or "cold".
func (v EggVariant) String() string {
	switch v {
	case EggWarm:
		return "warm"
	case EggCold:
		return "cold"
	}
	return "temperate"
}

// MaxCount ...
func (e Egg) MaxCount() int {
	return 16
}

// Use ...
func (e Egg) Use(tx *world.Tx, user User, ctx *UseContext) bool {
	opts := world.EntitySpawnOpts{Position: eyePosition(user), Velocity: user.Rotation().Vec3().Mul(1.5)}
	tx.AddEntity(e.entity(tx, opts, user))
	tx.PlaySound(user.Position(), sound.ItemThrow{})

	ctx.SubtractFromCount(1)
	return true
}

// EncodeItem ...
func (e Egg) EncodeItem() (name string, meta int16) {
	switch e.Variant {
	case EggWarm:
		return "minecraft:brown_egg", 0
	case EggCold:
		return "minecraft:blue_egg", 0
	}
	return "minecraft:egg", 0
}

// colouredEggEntity creates the thrown egg of a brown or blue egg. See RegisterEggEntity.
var colouredEggEntity func(opts world.EntitySpawnOpts, egg Egg, owner world.Entity) *world.EntityHandle

// RegisterEggEntity sets the function that creates thrown brown and blue eggs, which
// world.EntityRegistryConfig.Egg cannot tell from white ones. Package entity sets it from an init function;
// without it they are thrown as white eggs.
func RegisterEggEntity(f func(opts world.EntitySpawnOpts, egg Egg, owner world.Entity) *world.EntityHandle) {
	colouredEggEntity = f
}

// entity creates the thrown egg entity of e, thrown by owner (nil for a dispenser).
func (e Egg) entity(tx *world.Tx, opts world.EntitySpawnOpts, owner world.Entity) *world.EntityHandle {
	if e.Variant != EggTemperate && colouredEggEntity != nil {
		return colouredEggEntity(opts, e, owner)
	}
	return tx.World().EntityRegistry().Config().Egg(opts, owner)
}

func init() {
	// The white egg registers with the other items in register.go.
	world.RegisterItem(Egg{Variant: EggWarm})
	world.RegisterItem(Egg{Variant: EggCold})
}
