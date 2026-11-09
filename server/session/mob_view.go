package session

import (
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// The interfaces below are optional. An entity (or, for an *entity.Ent, its
// Behaviour) implements the ones it needs and the Session sends what they
// report the way BDS sends it. See SESSION_MOBS.md.

// PropertiedEntityType is a world.EntityType with entity properties. The
// client-synced ones are sent in a SyncActorProperty packet when a player
// joins, and their values go out in AddActor and SetActorData.
type PropertiedEntityType interface {
	world.EntityType
	// EntityProperties returns every property of the type, synced or not.
	EntityProperties() []world.EntityProperty
}

// PropertiedEntity is an entity with property values. Values are an int
// type, a float type or a bool, and an enum is the name of its value (an
// integer index works too). Properties missing from the map use the
// constant default of the world.EntityProperty, if there is one.
type PropertiedEntity interface {
	ActorProperties() map[string]any
}

// HeadYawEntity is an entity whose head turns apart from its yaw. Without
// it the head yaw sent is the yaw of Rotation.
type HeadYawEntity interface {
	HeadYaw() float64
}

// BodyYawEntity is an entity whose body yaw differs from its yaw. Bedrock
// only takes the body yaw in AddActor; clients turn the body themselves after
// that.
type BodyYawEntity interface {
	BodyYaw() float64
}

// TargetingEntity is a mob that may have a target (minecraft:target in
// actor data), such as a creeper looking at the player it follows. A
// Target() *world.EntityHandle method works as well.
type TargetingEntity interface {
	TargetEntity() *world.EntityHandle
}

// ColouredEntity is an entity with a colour, such as a sheep's wool or a
// wolf's collar.
type ColouredEntity interface {
	MobColour() item.Colour
}

// SecondColouredEntity is an entity with a second colour, such as a
// tropical fish's pattern.
type SecondColouredEntity interface {
	MobColour2() item.Colour
}

// FusedEntity is an ignited entity with a fuse other than the 1.5 seconds
// of a creeper.
type FusedEntity interface {
	FuseLength() time.Duration
}

// BedrockMetadataEntity edits the actor data last, after everything else
// is set, for data no other interface covers.
type BedrockMetadataEntity interface {
	BedrockMetadata(m protocol.EntityMetadata)
}

// BedrockAttributesEntity is a living entity with attributes besides health,
// movement and absorption, such as minecraft:horse.jump_strength.
type BedrockAttributesEntity interface {
	BedrockAttributes() []protocol.AttributeValue
}

// BodyArmouredEntity is an entity wearing body armour, such as horse armour.
type BodyArmouredEntity interface {
	BodyArmour() item.Stack
}

// mobFlag maps a single bool method of an entity to an actor data flag.
type mobFlag struct {
	get  func(any) (bool, bool)
	flag uint8
}

func flagOf[T any](f func(T) bool) func(any) (bool, bool) {
	return func(e any) (bool, bool) {
		v, ok := e.(T)
		if !ok {
			return false, false
		}
		return f(v), true
	}
}

// mobFlags are the flags of mobs that come from one method each.
var mobFlags = []mobFlag{
	{flagOf(func(e interface{ Sheared() bool }) bool { return e.Sheared() }), protocol.EntityDataFlagSheared},
	{flagOf(func(e interface{ Saddled() bool }) bool { return e.Saddled() }), protocol.EntityDataFlagSaddled},
	{flagOf(func(e interface{ Tamed() bool }) bool { return e.Tamed() }), protocol.EntityDataFlagTamed},
	{flagOf(func(e interface{ Sitting() bool }) bool { return e.Sitting() }), protocol.EntityDataFlagSitting},
	{flagOf(func(e interface{ WallClimbing() bool }) bool { return e.WallClimbing() }), protocol.EntityDataFlagWallClimbing},
	{flagOf(func(e interface{ Shaking() bool }) bool { return e.Shaking() }), protocol.EntityDataFlagShaking},
	{flagOf(func(e interface{ Chested() bool }) bool { return e.Chested() }), protocol.EntityDataFlagChested},
	{flagOf(func(e interface{ InLove() bool }) bool { return e.InLove() }), protocol.EntityDataFlagInLove},
}

// addMobMetadata writes the mob data of e, an entity or a behaviour.
func (s *Session) addMobMetadata(e any, m protocol.EntityMetadata) {
	for _, f := range mobFlags {
		if v, ok := f.get(e); ok {
			if v {
				m.SetFlag(protocol.EntityDataKeyFlags, f.flag)
			} else {
				m.UnsetFlag(protocol.EntityDataKeyFlags, f.flag)
			}
		}
	}
	// Extra actor flags goals turn on (timer_flag_1, ram_attack, rolling,
	// ...): mob.Mob.ExtraActorFlags, one bit per protocol.EntityDataFlag*.
	if x, ok := e.(interface{ ExtraActorFlags() [3]uint64 }); ok {
		for i, word := range x.ExtraActorFlags() {
			for b := 0; word != 0 && b < 64; b++ {
				if word&(1<<b) != 0 {
					word &^= 1 << b
					switch f := i*64 + b; {
					case f < 64:
						m.SetFlag(protocol.EntityDataKeyFlags, uint8(f))
					case f < protocol.EntityDataFlagCount:
						m.SetFlag(protocol.EntityDataKeyFlagsTwo, uint8(f&63))
					}
				}
			}
		}
	}
	// Extra actor data (mob.Mob.ExtraActorData): the wither's head targets,
	// the warden's heartbeat, ... An entity handle is sent as its runtime id.
	if x, ok := e.(interface{ ExtraActorData() map[uint32]any }); ok {
		for k, v := range x.ExtraActorData() {
			if k == protocol.EntityDataKeyLeashHolder {
				s.noteLeashHolder(e, v) // mob_leash.go
			}
			if h, ok := v.(*world.EntityHandle); ok {
				id := int64(-1)
				if h != nil {
					id = int64(s.handleRuntimeID(h))
				}
				m[k] = id
				continue
			}
			m[k] = v
		}
	}
	var target func() *world.EntityHandle
	if t, ok := e.(TargetingEntity); ok {
		target = t.TargetEntity
	} else if t, ok := e.(interface{ Target() *world.EntityHandle }); ok {
		target = t.Target
	}
	if target != nil {
		var id int64
		if h := target(); h != nil {
			id = int64(s.handleRuntimeID(h))
		}
		m[protocol.EntityDataKeyTarget] = id
	}
	// Villager trade level (server/mob villagers).
	if t, ok := e.(interface{ TradeTier() int32 }); ok {
		m[protocol.EntityDataKeyTradeTier] = t.TradeTier()
	}
	if t, ok := e.(interface{ MaxTradeTier() int32 }); ok {
		m[protocol.EntityDataKeyMaxTradeTier] = t.MaxTradeTier()
	}
	if t, ok := e.(interface{ TradeExperience() int32 }); ok {
		m[protocol.EntityDataKeyTradeExperience] = t.TradeExperience()
	}
	if c, ok := e.(ColouredEntity); ok {
		m[protocol.EntityDataKeyColorIndex] = c.MobColour().Uint8()
	}
	if c, ok := e.(SecondColouredEntity); ok {
		m[protocol.EntityDataKeyColorTwoIndex] = c.MobColour2().Uint8()
	}
}

// lookupEntity returns e or the behaviour of e as T.
func lookupEntity[T any](e world.Entity) (T, bool) {
	if v, ok := e.(T); ok {
		return v, true
	}
	if ent, ok := e.(interface{ Behaviour() entity.Behaviour }); ok {
		if v, ok := ent.Behaviour().(T); ok {
			return v, true
		}
	}
	var zero T
	return zero, false
}

// entityHeadYaw returns the head yaw of e, which is yaw unless e turns its
// head apart.
func entityHeadYaw(e world.Entity, yaw float64) float64 {
	if h, ok := lookupEntity[HeadYawEntity](e); ok {
		return h.HeadYaw()
	}
	return yaw
}

// entityBodyYaw returns the body yaw of e, which is yaw unless e reports a
// body yaw.
func entityBodyYaw(e world.Entity, yaw float64) float64 {
	if b, ok := lookupEntity[BodyYawEntity](e); ok {
		return b.BodyYaw()
	}
	return yaw
}

// networkEntityID returns the identifier of t on the network.
func networkEntityID(t world.EntityType) string {
	if v, ok := t.(NetworkEncodeableEntity); ok {
		return v.NetworkEncodeEntity()
	}
	return t.EncodeEntity()
}

// maxEntityProperties is the most properties BDS loads for one entity type.
// A type with more has none at all.
const maxEntityProperties = 32

// propertySchema holds the client-synced properties of an entity type in
// network order.
type propertySchema struct {
	props []world.EntityProperty
	index map[string]int
}

// propertySchemas caches the schema of each entity type identifier.
var propertySchemas sync.Map

// entityPropertySchema returns the synced properties of t, or nil if it has
// none. BDS reads the property names of a definition in order (json objects
// are sorted maps) and puts the client-synced ones first: the index of a
// synced property is its place among the synced names.
func entityPropertySchema(t world.EntityType) *propertySchema {
	pt, ok := t.(PropertiedEntityType)
	if !ok {
		return nil
	}
	id := networkEntityID(t)
	if v, ok := propertySchemas.Load(id); ok {
		return v.(*propertySchema)
	}
	var schema *propertySchema
	if all := pt.EntityProperties(); len(all) <= maxEntityProperties {
		var synced []world.EntityProperty
		for _, p := range all {
			if p.ClientSync {
				synced = append(synced, p)
			}
		}
		if len(synced) > 0 {
			slices.SortFunc(synced, func(a, b world.EntityProperty) int { return strings.Compare(a.Name, b.Name) })
			schema = &propertySchema{props: synced, index: make(map[string]int, len(synced))}
			for i, p := range synced {
				schema.index[p.Name] = i
			}
		}
	}
	v, _ := propertySchemas.LoadOrStore(id, schema)
	return v.(*propertySchema)
}

// syncTag returns the SyncActorProperty data of the schema, as built by
// PropertyGroupManager::getActorPropertyDataTag.
func (schema *propertySchema) syncTag(id string) map[string]any {
	props := make([]map[string]any, 0, len(schema.props))
	for _, p := range schema.props {
		tag := map[string]any{"name": p.Name, "type": int32(p.Type)}
		switch p.Type {
		case world.EntityPropertyInt:
			tag["min"], tag["max"] = int32(p.Min), int32(p.Max)
		case world.EntityPropertyFloat:
			tag["min"], tag["max"] = float32(p.Min), float32(p.Max)
		case world.EntityPropertyEnum:
			tag["enum"] = slices.Clone(p.Enum)
		}
		props = append(props, tag)
	}
	return map[string]any{"type": id, "properties": props}
}

// sendActorPropertySchemas sends a SyncActorProperty packet for every
// registered entity type with client-synced properties, as BDS does after
// StartGame.
func (s *Session) sendActorPropertySchemas(types []world.EntityType) {
	for _, t := range types {
		if schema := entityPropertySchema(t); schema != nil {
			s.writePacket(&packet.SyncActorProperty{PropertyData: schema.syncTag(networkEntityID(t))})
		}
	}
}

// entityProperties returns the values of all synced properties of e, as
// PropertyComponent::packAllSyncData packs them: ints, bools and enums in
// the int list, floats in the float list, both in index order.
func entityProperties(e world.Entity) protocol.EntityProperties {
	var props protocol.EntityProperties
	schema := entityPropertySchema(e.H().Type())
	if schema == nil {
		return props
	}
	var values map[string]any
	if p, ok := lookupEntity[PropertiedEntity](e); ok {
		values = p.ActorProperties()
	}
	for i, p := range schema.props {
		v, ok := values[p.Name]
		if !ok {
			v = p.Default
		}
		if p.Type == world.EntityPropertyFloat {
			if f, ok := propertyFloat(v); ok {
				props.FloatProperties = append(props.FloatProperties, protocol.FloatEntityProperty{Index: uint32(i), Value: f})
			}
			continue
		}
		if n, ok := propertyInt(p, v); ok {
			props.IntegerProperties = append(props.IntegerProperties, protocol.IntegerEntityProperty{Index: uint32(i), Value: n})
		}
	}
	return props
}

// propertyFloat converts the value of a float property.
func propertyFloat(v any) (float32, bool) {
	switch v := v.(type) {
	case float32:
		return v, true
	case float64:
		return float32(v), true
	}
	if n, ok := anyInt(v); ok {
		return float32(n), true
	}
	return 0, false
}

// propertyInt converts the value of an int, bool or enum property.
func propertyInt(p world.EntityProperty, v any) (int32, bool) {
	switch p.Type {
	case world.EntityPropertyBool:
		b, ok := v.(bool)
		if !ok {
			return 0, false
		}
		if b {
			return 1, true
		}
		return 0, true
	case world.EntityPropertyEnum:
		if name, ok := v.(string); ok {
			i := slices.Index(p.Enum, name)
			return int32(i), i >= 0
		}
		n, ok := anyInt(v)
		return int32(n), ok && n >= 0 && n < int64(len(p.Enum))
	default:
		if f, ok := v.(float64); ok {
			return int32(f), true
		}
		n, ok := anyInt(v)
		return int32(n), ok
	}
}

// anyInt returns v as an int64 if it is an integer type.
func anyInt(v any) (int64, bool) {
	switch v := v.(type) {
	case int:
		return int64(v), true
	case int8:
		return int64(v), true
	case int16:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	case uint8:
		return int64(v), true
	case uint16:
		return int64(v), true
	case uint32:
		return int64(v), true
	case uint64:
		return int64(v), true
	case uint:
		return int64(v), true
	}
	return 0, false
}

// entityHealth is a living entity that is not a player.
type entityHealth interface {
	Health() float64
	MaxHealth() float64
}

// entityAttributes returns the attributes BDS syncs for a mob: health,
// movement and absorption where e has them, then any others it reports.
// They are nil for entities without health and for players, whose
// attributes go out another way.
func entityAttributes(e world.Entity) []protocol.AttributeValue {
	if _, ok := e.(Controllable); ok {
		return nil
	}
	h, ok := lookupEntity[entityHealth](e)
	if !ok {
		return nil
	}
	attrs := []protocol.AttributeValue{{Name: "minecraft:health", Value: float32(h.Health()), Max: float32(h.MaxHealth())}}
	if sp, ok := lookupEntity[interface{ Speed() float64 }](e); ok {
		attrs = append(attrs, protocol.AttributeValue{Name: "minecraft:movement", Value: float32(sp.Speed()), Max: math.MaxFloat32})
	}
	if ab, ok := lookupEntity[interface{ Absorption() float64 }](e); ok {
		attrs = append(attrs, protocol.AttributeValue{Name: "minecraft:absorption", Value: float32(ab.Absorption()), Max: math.MaxFloat32})
	}
	if extra, ok := lookupEntity[BedrockAttributesEntity](e); ok {
		attrs = append(attrs, extra.BedrockAttributes()...)
	}
	return attrs
}

// ViewEntityHealth sends the health and other attributes of a mob, as BDS
// does when they change. It does nothing for players and entities without
// health. Mobs call it on viewers that have it after their health changes.
func (s *Session) ViewEntityHealth(e world.Entity) {
	if s.entityHidden(e) {
		return
	}
	id := s.entityRuntimeID(e)
	if id == 0 || id == selfEntityRuntimeID {
		return
	}
	attrs := entityAttributes(e)
	if len(attrs) == 0 {
		return
	}
	pk := &packet.UpdateAttributes{EntityRuntimeID: id, Attributes: make([]protocol.Attribute, 0, len(attrs))}
	for _, a := range attrs {
		def := a.Value
		if a.Name == "minecraft:health" {
			def = a.Max
		}
		pk.Attributes = append(pk.Attributes, protocol.Attribute{AttributeValue: a, DefaultMin: a.Min, DefaultMax: a.Max, Default: def})
	}
	s.writePacket(pk)
}

// mobFuseTicks returns the fuse shown for an ignited creeper-like entity.
func mobFuseTicks(e any) int32 {
	if f, ok := e.(FusedEntity); ok {
		return int32(f.FuseLength().Milliseconds() / 50)
	}
	return 30
}
