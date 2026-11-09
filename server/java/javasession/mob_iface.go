package javasession

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// The optional interfaces javasession reads mob state through, so that it does not depend on the
// mob package. Each is looked up on the entity first and then on its behaviour (an *entity.Ent's
// Behaviour()), the way the Bedrock session reads entity metadata. Every method is a plain getter
// called on the world goroutine. JAVA_MOBS.md lists what each one turns into on Java.
//
// The names and value ranges are the Bedrock ones (entity metadata and actor properties), so the
// Bedrock session can read the same methods.
type (
	// mobBaby: the minecraft:is_baby component (Bedrock flag Baby).
	mobBaby interface{ Baby() bool }
	// mobVariant: minecraft:variant (Bedrock key Variant).
	mobVariant interface{ Variant() int32 }
	// mobMarkVariant: minecraft:mark_variant (Bedrock key MarkVariant).
	mobMarkVariant interface{ MarkVariant() int32 }
	// mobColour: minecraft:color, a dye id 0 white .. 15 black (Bedrock key ColorIndex).
	mobColour interface{ Colour() int32 }
	// mobColour2: minecraft:color2 (Bedrock key ColorTwoIndex): the tropical fish pattern colour.
	mobColour2 interface{ Colour2() int32 }
	// mobSheared: minecraft:is_sheared (Bedrock flag Sheared). A snow golem without its pumpkin.
	mobSheared interface{ Sheared() bool }
	// mobSaddled: minecraft:is_saddled (Bedrock flag Saddled).
	mobSaddled interface{ Saddled() bool }
	// mobChested: minecraft:is_chested (Bedrock flag Chested).
	mobChested interface{ Chested() bool }
	// mobTamed: minecraft:is_tamed (Bedrock flag Tamed).
	mobTamed interface{ Tamed() bool }
	// mobSitting: minecraft:sittable sitting (Bedrock flag Sitting).
	mobSitting interface{ Sitting() bool }
	// mobCharged: a charged creeper (minecraft:is_charged, Bedrock flag Powered). Powered() bool,
	// which Dragonfly creeper behaviours have, is read as well.
	mobCharged interface{ Charged() bool }
	mobPowered interface{ Powered() bool }
	// mobIgnited: a creeper swelling (its fuse is lit: Bedrock flag Ignited).
	mobIgnited interface{ Ignited() bool }
	// mobClimbing: a spider on a wall (Bedrock flag WallClimbing).
	mobClimbing interface{ Climbing() bool }
	// mobShaking: Bedrock flag Shaking: a zombie turning into a drowned, a skeleton into a stray,
	// a strider out of lava.
	mobShaking interface{ Shaking() bool }
	// mobConverting: a zombie villager being cured (Bedrock flag Converting).
	mobConverting interface{ Converting() bool }
	// mobAngry: minecraft:angry (Bedrock flag Angry). Already implemented by entity.Ent.
	mobAngry interface{ Angry() bool }
	// mobAggressive: the mob is after a target (it has one and a goal is attacking it). Java draws
	// zombies with arms out, skeletons with the bow raised and illagers with weapons up.
	mobAggressive interface{ Aggressive() bool }
	// mobSneaking: a fox crouching to pounce (Bedrock flag Sneaking).
	mobSneaking interface{ Sneaking() bool }
	// mobUsing: the mob uses its held item (a skeleton drawing its bow, a witch drinking).
	mobUsing interface{ UsingItem() bool }
	// mobTradeTier: a villager's trade tier, 0 novice .. 4 master (Bedrock key TradeTier).
	mobTradeTier interface{ TradeTier() int32 }
	// mobProperties: the actor properties (description.properties). Enum values are strings, int
	// values int/int32, bools bool and floats float32/float64. minecraft:climate_variant and
	// minecraft:sound_variant are read.
	mobProperties interface{ ActorProperties() map[string]any }
	// mobBodyYaw and mobHeadYaw: separate body and head rotation, in degrees. Without them both
	// are Rotation().Yaw().
	mobBodyYaw interface{ BodyYaw() float64 }
	mobHeadYaw interface{ HeadYaw() float64 }
	// mobHealth: entity.Living's health, sent as LivingEntity health (iron golem cracks, wolf tail).
	mobHealth interface {
		Health() float64
		MaxHealth() float64
	}
	// mobBodyArmour: what the mob wears on its body (horse or wolf armour, a llama carpet, a happy
	// ghast harness): the Java BODY equipment slot. Hands and armour come from item.Carrier and
	// Armour() *inventory.Armour as for players.
	mobBodyArmour interface{ BodyArmour() item.Stack }
	// mobScale: minecraft:scale (Bedrock key Scale); salmon size is read from it.
	mobScale interface{ Scale() float64 }

	// mobActorEvent is a world.EntityAction that is a Bedrock actor event (packet.ActorEvent*,
	// such as ActorEventEatGrass): the Bedrock session sends it as an ActorEvent packet,
	// javasession as the matching Java entity_event.
	mobActorEvent interface{ ActorEvent() uint8 }

	// paintingFacing and paintingMotive: a painting entity's facing (the direction it faces, away
	// from the wall) and its Bedrock motive ("Kebab", "BurningSkull", "skull_and_roses"...).
	paintingFacing interface{ Facing() cube.Direction }
	paintingMotive interface{ Motive() string }
	// paintingBlock: the block a painting hangs in (Java sends it, not the centre). Without it the
	// block holding Position() is used.
	paintingBlock interface{ HangingBlock() cube.Pos }
	// projectileOwner: the entity that cast a fishing hook (and shot a projectile).
	projectileOwner interface{ Owner() *world.EntityHandle }
)

// mobAs returns the first of e and its behaviour that implements T.
func mobAs[T any](e world.Entity) (T, bool) {
	if v, ok := e.(T); ok {
		return v, true
	}
	if b, ok := e.(interface{ Behaviour() entity.Behaviour }); ok {
		if v, ok := b.Behaviour().(T); ok {
			return v, true
		}
	}
	var zero T
	return zero, false
}

// mobBool is the value of a bool getter of e, false if neither e nor its behaviour has it.
func mobBool[T any](e world.Entity, get func(T) bool) bool {
	v, ok := mobAs[T](e)
	return ok && get(v)
}

// mobInt is the value of an int32 getter of e, or def.
func mobInt[T any](e world.Entity, get func(T) int32, def int32) int32 {
	if v, ok := mobAs[T](e); ok {
		return get(v)
	}
	return def
}

// mobProperty is the actor property name of e as a string ("" if it has none).
func mobProperty(e world.Entity, name string) string {
	p, ok := mobAs[mobProperties](e)
	if !ok {
		return ""
	}
	v, _ := p.ActorProperties()[name].(string)
	return v
}
