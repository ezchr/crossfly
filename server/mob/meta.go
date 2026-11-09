//go:build crossfly_full

package mob

import (
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Meta is the client-visible state of a mob that components set and the
// client packages (server/session, server/java/javasession) read through the
// Mob methods below. Change it with Mob.UpdateMeta so viewers are told.
type Meta struct {
	// Width and Height are the collision box (minecraft:collision_box)
	// before Scale.
	Width, Height float64
	// Scale is minecraft:scale (1 by default).
	Scale float64

	Baby        bool
	Variant     int32 // minecraft:variant
	MarkVariant int32 // minecraft:mark_variant
	SkinID      int32 // minecraft:skin_id
	Color       uint8 // minecraft:color
	Color2      uint8 // minecraft:color2
	Sheared     bool
	Saddled     bool
	Tamed       bool
	Sitting     bool
	Chested     bool
	Charged     bool // powered creeper
	Ignited     bool // lit fuse
	// FuseTicks is the fuse length shown while Ignited (BDS: explode
	// fuse_length in ticks).
	FuseTicks int32
	// Swell is the creeper swell direction (-1, 0, 1).
	Swell    int32
	Climbing bool
	Shaking  bool
	// Converting: a zombie villager being cured.
	Converting bool
	// InLove: breeding love mode (Bedrock actor flag in_love; Java has no
	// flag and shows the love-hearts entity event instead).
	InLove bool
	Angry  bool
	// Aggressive: after a target with an attack goal running (Java arms
	// raised / bow drawn).
	Aggressive bool
	Sneaking   bool
	// Sprinting: a mob running (an ocelot or cat closing in on its prey);
	// Bedrock actor flag sprinting, Java shared flag 0x08.
	Sprinting bool
	// UsingItem: using the held item (bow drawn, potion drunk).
	UsingItem bool
	// TradeTier: villager trade tier 0..4.
	TradeTier int32
	// TradeExperience and MaxTradeTier: a villager trader's experience and
	// highest tier (Bedrock actor data trade_experience, max_trade_tier).
	TradeExperience int32
	MaxTradeTier    int32
	Invisible       bool
	NoGravity       bool
	// NoAI shows the mob immobile (BDS NoAI flag).
	NoAI bool
	// Owner is the tamer/owner, shown as the owner id.
	Owner *world.EntityHandle
	// ActorFlags are extra Bedrock actor flags set by goals/components
	// (actor_flags.go, Mob.SetActorFlag).
	ActorFlags ActorFlags
}

// Meta returns a copy of the mob's client-visible state.
func (m *Mob) Meta() Meta { return m.s.meta }

// UpdateMeta changes the client-visible state and shows it to viewers when
// it changed.
func (m *Mob) UpdateMeta(f func(meta *Meta)) {
	before := m.s.meta
	f(&m.s.meta)
	if before != m.s.meta {
		m.RefreshState()
	}
}

// RefreshState shows the mob's state again to its viewers.
func (m *Mob) RefreshState() {
	tx := m.Tx()
	if tx == nil || tx.Closed() {
		return
	}
	for _, v := range tx.Viewers(m.data.Pos) {
		v.ViewEntityState(m)
	}
}

// Metadata accessors. They match the optional interfaces server/session
// (mob_view.go) and server/java/javasession (mob_iface.go) probe, and are
// defined on the state (the entity.Behaviour) too, because both probe the
// behaviour of an entity, and entity.Ent passes itself, not the *Mob, when it
// refreshes the state (name tag, fire). Ent also forwards Scale, Invisible,
// Angry, HeldItems and Armour to the behaviour.
func (s *state) Scale() float64                    { return s.meta.Scale }
func (s *state) Baby() bool                        { return s.meta.Baby }
func (s *state) Variant() int32                    { return s.meta.Variant }
func (s *state) MarkVariant() int32                { return s.meta.MarkVariant }
func (s *state) SkinID() int32                     { return s.meta.SkinID }
func (s *state) Colour() int32                     { return int32(s.meta.Color) }
func (s *state) Colour2() int32                    { return int32(s.meta.Color2) }
func (s *state) MobColour() item.Colour            { return colourOf(s.meta.Color) }
func (s *state) MobColour2() item.Colour           { return colourOf(s.meta.Color2) }
func (s *state) Sheared() bool                     { return s.meta.Sheared }
func (s *state) Saddled() bool                     { return s.meta.Saddled }
func (s *state) Tamed() bool                       { return s.meta.Tamed }
func (s *state) Sitting() bool                     { return s.meta.Sitting }
func (s *state) Chested() bool                     { return s.meta.Chested }
func (s *state) Charged() bool                     { return s.meta.Charged }
func (s *state) Powered() bool                     { return s.meta.Charged }
func (s *state) Ignited() bool                     { return s.meta.Ignited }
func (s *state) FuseTicks() int32                  { return s.meta.FuseTicks }
func (s *state) Swell() int32                      { return s.meta.Swell }
func (s *state) Climbing() bool                    { return s.meta.Climbing }
func (s *state) WallClimbing() bool                { return s.meta.Climbing }
func (s *state) Shaking() bool                     { return s.meta.Shaking }
func (s *state) Converting() bool                  { return s.meta.Converting }
func (s *state) InLove() bool                      { return s.meta.InLove }
func (s *state) Angry() bool                       { return s.meta.Angry }
func (s *state) Aggressive() bool                  { return s.meta.Aggressive }
func (s *state) Sneaking() bool                    { return s.meta.Sneaking }
func (s *state) Sprinting() bool                   { return s.meta.Sprinting }
func (s *state) UsingItem() bool                   { return s.meta.UsingItem }
func (s *state) TradeTier() int32                  { return s.meta.TradeTier }
func (s *state) TradeExperience() int32            { return s.meta.TradeExperience }
func (s *state) MaxTradeTier() int32               { return s.meta.MaxTradeTier }
func (s *state) Invisible() bool                   { return s.meta.Invisible }
func (s *state) Gravityless() bool                 { return s.meta.NoGravity }
func (s *state) Immobile() bool                    { return s.meta.NoAI }
func (s *state) Owner() *world.EntityHandle        { return s.meta.Owner }
func (s *state) HeadYaw() float64                  { return s.headYaw }
func (s *state) BodyYaw() float64                  { return s.bodyYaw }
func (s *state) TargetEntity() *world.EntityHandle { return s.target }
func (s *state) BodyArmour() item.Stack            { return s.bodyArmour }
func (s *state) Absorption() float64               { return s.attrs.Value(AttrAbsorption) }

func (m *Mob) Scale() float64                    { return m.s.Scale() }
func (m *Mob) Baby() bool                        { return m.s.Baby() }
func (m *Mob) Variant() int32                    { return m.s.Variant() }
func (m *Mob) MarkVariant() int32                { return m.s.MarkVariant() }
func (m *Mob) SkinID() int32                     { return m.s.SkinID() }
func (m *Mob) Colour() int32                     { return m.s.Colour() }
func (m *Mob) Colour2() int32                    { return m.s.Colour2() }
func (m *Mob) MobColour() item.Colour            { return m.s.MobColour() }
func (m *Mob) MobColour2() item.Colour           { return m.s.MobColour2() }
func (m *Mob) Sheared() bool                     { return m.s.Sheared() }
func (m *Mob) Saddled() bool                     { return m.s.Saddled() }
func (m *Mob) Tamed() bool                       { return m.s.Tamed() }
func (m *Mob) Sitting() bool                     { return m.s.Sitting() }
func (m *Mob) Chested() bool                     { return m.s.Chested() }
func (m *Mob) Charged() bool                     { return m.s.Charged() }
func (m *Mob) Powered() bool                     { return m.s.Powered() }
func (m *Mob) Ignited() bool                     { return m.s.Ignited() }
func (m *Mob) FuseTicks() int32                  { return m.s.FuseTicks() }
func (m *Mob) Swell() int32                      { return m.s.Swell() }
func (m *Mob) Climbing() bool                    { return m.s.Climbing() }
func (m *Mob) WallClimbing() bool                { return m.s.WallClimbing() }
func (m *Mob) Shaking() bool                     { return m.s.Shaking() }
func (m *Mob) Converting() bool                  { return m.s.Converting() }
func (m *Mob) InLove() bool                      { return m.s.InLove() }
func (m *Mob) Angry() bool                       { return m.s.Angry() }
func (m *Mob) Aggressive() bool                  { return m.s.Aggressive() }
func (m *Mob) Sneaking() bool                    { return m.s.Sneaking() }
func (m *Mob) Sprinting() bool                   { return m.s.Sprinting() }
func (m *Mob) UsingItem() bool                   { return m.s.UsingItem() }
func (m *Mob) TradeTier() int32                  { return m.s.TradeTier() }
func (m *Mob) TradeExperience() int32            { return m.s.TradeExperience() }
func (m *Mob) MaxTradeTier() int32               { return m.s.MaxTradeTier() }
func (m *Mob) Invisible() bool                   { return m.s.Invisible() }
func (m *Mob) Gravityless() bool                 { return m.s.Gravityless() }
func (m *Mob) Immobile() bool                    { return m.s.Immobile() }
func (m *Mob) Owner() *world.EntityHandle        { return m.s.Owner() }
func (m *Mob) HeadYaw() float64                  { return m.s.HeadYaw() }
func (m *Mob) BodyYaw() float64                  { return m.s.BodyYaw() }
func (m *Mob) TargetEntity() *world.EntityHandle { return m.s.TargetEntity() }
func (m *Mob) BodyArmour() item.Stack            { return m.s.BodyArmour() }
func (m *Mob) Absorption() float64               { return m.s.Absorption() }

// FuseLength is the fuse shown while Ignited (FuseTicks, 1.5s when unset).
func (s *state) FuseLength() time.Duration {
	if s.meta.FuseTicks <= 0 {
		return time.Second * 3 / 2
	}
	return time.Duration(s.meta.FuseTicks) * time.Second / 20
}

// FuseLength is the fuse shown while Ignited (FuseTicks, 1.5s when unset).
func (m *Mob) FuseLength() time.Duration { return m.s.FuseLength() }

// ActorProperties returns the property values for the clients (enum values
// as their names, int properties as int).
func (s *state) ActorProperties() map[string]any {
	if s.inst == nil {
		return nil
	}
	props := s.inst.Properties()
	out := make(map[string]any, len(props))
	for _, p := range props {
		v := p.Value
		if f, ok := v.(float64); ok && p.Decl.Type == "int" {
			v = int(f)
		}
		out[p.Decl.Name] = v
	}
	return out
}

// ActorProperties returns the property values for the clients.
func (m *Mob) ActorProperties() map[string]any { return m.s.ActorProperties() }

// SetBodyArmour changes what the mob wears on its body (horse armour,
// carpet, harness) and shows it.
func (m *Mob) SetBodyArmour(s item.Stack) {
	m.s.bodyArmour = s
	m.RefreshState()
	m.viewEquipment(world.Viewer.ViewEntityArmour)
}

func colourOf(c uint8) item.Colour {
	cs := item.Colours()
	if int(c) < len(cs) {
		return cs[c]
	}
	return cs[0]
}

// HeldItems returns the items in the mob's hands.
func (s *state) HeldItems() (mainHand, offHand item.Stack) { return s.mainHand, s.offHand }

// Armour returns the mob's armour slots (nil before the mob initialised).
func (s *state) Armour() *inventory.Armour { return s.armour }

// NameTaggable reports whether a name tag may rename the mob
// (minecraft:nameable).
func (s *state) NameTaggable() bool {
	for _, c := range s.comps {
		if c.key == "minecraft:nameable" {
			return true
		}
	}
	return false
}

// Interact lets entity.Ent.Interact reach Mob.Interact.
func (s *state) Interact(e *entity.Ent, user item.User, held item.Stack, clickPos mgl64.Vec3) (item.Stack, bool) {
	return (&Mob{Ent: e, data: s.data, s: s}).Interact(user, held, clickPos)
}
