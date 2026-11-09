//go:build crossfly_full

package mob

import "github.com/df-mc/dragonfly/server/world"

// animalClass holds the mobs whose BDS 1.21.3 class derives from Animal
// (the _actorFromClass<...> of VanillaBuiltInEntities::registerMappings and
// VanillaActors::registerVanillaActorData, whose vtables point at
// Animal::isInvulnerableTo): Animal itself (cow, fox, frog), Armadillo,
// Axolotl, Bee, Camel, Cat, Chicken, Goat, Horse (horse, donkey, mule,
// skeleton and zombie horse), Llama (llama, trader llama), MushroomCow,
// Ocelot, Panda, Pig, PolarBear, Rabbit, Sheep, Sniffer, Strider, Turtle and
// Wolf. camel_husk is newer than 1.21.3; its definition has the
// start_sitting/stop_sitting events only Camel::_monitorSitting fires, so it
// is a Camel. Parrot has its own isInvulnerableTo.
var animalClass = map[string]bool{
	"minecraft:armadillo": true, "minecraft:axolotl": true, "minecraft:bee": true,
	"minecraft:camel": true, "minecraft:camel_husk": true, "minecraft:cat": true,
	"minecraft:chicken": true, "minecraft:cow": true, "minecraft:donkey": true,
	"minecraft:fox": true, "minecraft:frog": true, "minecraft:goat": true,
	"minecraft:horse": true, "minecraft:llama": true, "minecraft:mooshroom": true,
	"minecraft:mule": true, "minecraft:ocelot": true, "minecraft:panda": true,
	"minecraft:pig": true, "minecraft:polar_bear": true, "minecraft:rabbit": true,
	"minecraft:sheep": true, "minecraft:skeleton_horse": true, "minecraft:sniffer": true,
	"minecraft:strider": true, "minecraft:trader_llama": true, "minecraft:turtle": true,
	"minecraft:wolf": true, "minecraft:zombie_horse": true,
}

// riderInvulnerable is Animal::isInvulnerableTo, which Actor::hurt asks
// before anything else: a mob of an Animal class takes no damage whose
// source entity is its first passenger. The source entity is
// ActorDamageSource::getEntityUniqueID: the attacker, or a projectile's
// owner (attackerOf).
func (m *Mob) riderInvulnerable(attacker world.Entity) bool {
	if attacker == nil {
		return false
	}
	r := m.s.rideIfAny()
	if r == nil || len(r.riders) == 0 || r.riders[0].Handle != attacker.H() {
		return false
	}
	return animalClass[m.Identifier()]
}
