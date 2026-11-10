package entity

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
)

// NewEgg creates a white Egg entity. Egg is as a throwable entity that can be used
// to spawn chicks.
func NewEgg(opts world.EntitySpawnOpts, owner world.Entity) *world.EntityHandle {
	return NewEggOf(opts, item.Egg{}, owner)
}

// NewEggOf creates a thrown egg of the egg item passed. Its colour shows to Bedrock clients (the
// minecraft:climate_variant property of the thrown egg) and Java clients (the item of the thrown egg), and the
// chicks it may hatch are of its climate variant.
func NewEggOf(opts world.EntitySpawnOpts, egg item.Egg, owner world.Entity) *world.EntityHandle {
	conf := eggConf
	conf.Owner = ownerHandle(owner)
	conf.PickupItem = item.NewStack(egg, 1)
	return opts.New(EggType, conf)
}

func init() { item.RegisterEggEntity(NewEggOf) }

// eggConf is the egg's minecraft:projectile (egg.json). PickupItem holds the egg item thrown, for its colour:
// eggs break on every hit, so it is never picked up.
var eggConf = ProjectileBehaviourConfig{
	Gravity:       0.03,
	Drag:          0.01,
	Particle:      particle.EggSmash{},
	ParticleCount: 6,
	DisablePickup: true,
	Hit:           hatchEgg,
}

// Egg spawn_chance of egg.json: the chance of hatching chicks and, if it does, of four instead of one.
const (
	eggFirstSpawnChance  = 0.125
	eggFirstSpawnCount   = 1
	eggSecondSpawnChance = 0.03125
	eggSecondSpawnCount  = 4
)

// eggChicks returns how many chicks an egg hatches for the two random rolls r1 and r2 in [0, 1), as
// SpawnChanceSubcomponent::doOnHitEffect counts them: none unless r1 <= first_spawn_chance, then
// second_spawn_count if r2 <= second_spawn_chance, else first_spawn_count.
func eggChicks(r1, r2 float64) int {
	if r1 > eggFirstSpawnChance {
		return 0
	}
	if r2 <= eggSecondSpawnChance {
		return eggSecondSpawnCount
	}
	return eggFirstSpawnCount
}

// mobEventFirer is the part of block.MobHost that fires an entity event on a mob it spawned. Package mob
// implements it.
type mobEventFirer interface {
	FireMobEvent(e world.Entity, event string) bool
}

// hatchEgg is the spawn_chance of the egg's projectile on_hit (egg.json, BDS
// SpawnChanceSubcomponent::doOnHitEffect): with a chance of 1/8 a chicken hatches where the egg broke, four of
// them if a second roll of 1/32 also passes. They are born as babies (spawn_baby: the entity_born event) at
// the egg's position, raised onto the top of the collision box of the block there if it has one, facing the
// egg's yaw, and never despawn. A brown or blue egg hatches warm or cold chicks: the 26.60 egg.json fires
// minecraft:hatch_warm or minecraft:hatch_cold on them (on_spawn, filtered on the egg's climate_variant).
// Without mobs nothing hatches.
func hatchEgg(e *Ent, tx *world.Tx, _ trace.Result) {
	n := eggChicks(rand.Float64(), rand.Float64())
	if n == 0 || block.Mobs() == nil {
		return
	}
	pos := e.Position()
	bpos := cube.PosFromVec3(pos)
	if boxes := tx.Block(bpos).Model().BBox(bpos, tx); len(boxes) > 0 {
		top := boxes[0].Max().Y()
		for _, b := range boxes[1:] {
			top = max(top, b.Max().Y())
		}
		pos[1] = float64(bpos[1]) + top
	}
	var event string
	switch eggOf(e.data).Variant {
	case item.EggWarm:
		event = "minecraft:hatch_warm"
	case item.EggCold:
		event = "minecraft:hatch_cold"
	}
	opts := block.MobSpawnOptions{Yaw: e.Rotation().Yaw(), Event: "minecraft:entity_born", Persistent: true}
	for range n {
		chick, ok := block.SpawnMob(tx, "minecraft:chicken", pos, opts)
		if !ok {
			return
		}
		if f, ok := block.Mobs().(mobEventFirer); ok && event != "" {
			f.FireMobEvent(chick, event)
		}
	}
}

// eggOf returns the egg item of the thrown egg data.
func eggOf(data *world.EntityData) item.Egg {
	if b, ok := data.Data.(*ProjectileBehaviour); ok {
		if egg, ok := b.conf.PickupItem.Item().(item.Egg); ok {
			return egg
		}
	}
	return item.Egg{}
}

// ThrownEggItem returns the egg item thrown if e is a thrown egg.
func ThrownEggItem(e world.Entity) (item.Stack, bool) {
	ent, ok := e.(*Ent)
	if !ok || ent.H().Type() != EggType {
		return item.Stack{}, false
	}
	return item.NewStack(eggOf(ent.data), 1), true
}

// ActorProperties returns the minecraft:climate_variant of a thrown egg, so Bedrock clients draw brown and blue
// eggs in their colour. Other projectiles have no properties.
func (lt *ProjectileBehaviour) ActorProperties() map[string]any {
	egg, ok := lt.conf.PickupItem.Item().(item.Egg)
	if !ok {
		return nil
	}
	return map[string]any{eggClimateVariant: egg.Variant.String()}
}

// eggClimateVariant is the property of the thrown egg (egg.json) that is its colour.
const eggClimateVariant = "minecraft:climate_variant"

// eggVariant returns the egg variant whose climate variant is name, the white egg if there is none.
func eggVariant(name string) item.EggVariant {
	for _, v := range item.EggVariants() {
		if v.String() == name {
			return v
		}
	}
	return item.EggTemperate
}

// EggType is a world.EntityType implementation for Egg.
var EggType eggType

type eggType struct{}

func (t eggType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &Ent{tx: tx, handle: handle, data: data}
}

func (eggType) EncodeEntity() string { return "minecraft:egg" }
func (eggType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.125, 0, -0.125, 0.125, 0.25, 0.125)
}

// EntityProperties returns the properties of egg.json: minecraft:climate_variant, synced to clients.
func (eggType) EntityProperties() []world.EntityProperty {
	return []world.EntityProperty{{
		Name:       eggClimateVariant,
		Type:       world.EntityPropertyEnum,
		Enum:       []string{"temperate", "warm", "cold"},
		ClientSync: true,
		Default:    "temperate",
	}}
}

func (eggType) DecodeNBT(m map[string]any, data *world.EntityData) {
	conf := eggConf
	name, _ := m["ClimateVariant"].(string)
	conf.PickupItem = item.NewStack(item.Egg{Variant: eggVariant(name)}, 1)
	data.Data = conf.New()
}

func (eggType) EncodeNBT(data *world.EntityData) map[string]any {
	if v := eggOf(data).Variant; v != item.EggTemperate {
		return map[string]any{"ClimateVariant": v.String()}
	}
	return nil
}
