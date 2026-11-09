//go:build crossfly_full

package filter

import (
	"encoding/json"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// This file adapts Dragonfly entities and *world.Tx to the filter
// interfaces, so filters compiled through mob.CompileFilter run on real mobs
// and players.

// FromMob converts a mob.FilterContext.
func FromMob(c *mob.FilterContext) *Context {
	if c == nil {
		return &Context{}
	}
	tx := c.Tx
	if tx == nil && c.Self != nil {
		tx = c.Self.Tx()
	}
	ctx := &Context{
		Other:   Wrap(tx, c.Other),
		Target:  Wrap(tx, c.Target),
		Player:  Wrap(tx, c.Player),
		Parent:  Wrap(tx, c.Parent),
		Baby:    Wrap(tx, c.Baby),
		Damager: Wrap(tx, c.Damager),
	}
	if c.Self != nil {
		ctx.Self = Wrap(tx, c.Self)
		ctx.Rand = c.Self.Rand()
	}
	if tx != nil {
		ctx.World = TxWorld{Tx: tx}
	}
	if c.HasBlock && c.Block != nil {
		ctx.Block = TxBlock{B: c.Block, Pos: c.BlockPos, Tx: tx}
	}
	if c.Damage != nil {
		ctx.Damage = &Damage{Cause: DamageCause(c.Damage)}
	}
	return ctx
}

// Wrap returns the filter Actor for e (nil for a nil entity).
func Wrap(tx *world.Tx, e world.Entity) Actor {
	if e == nil {
		return nil
	}
	if m, ok := e.(*mob.Mob); ok && m == nil {
		return nil
	}
	if p, ok := e.(*player.Player); ok && p == nil {
		return nil
	}
	return EntityActor{Tx: tx, E: e}
}

// Identity is implemented by actors that wrap another value. Tests that ask
// "is the subject the calling entity's target/owner/..." compare identities,
// so two wrappers of the same entity are the same subject.
type Identity interface {
	Identity() any
}

func identity(a Actor) any {
	if a == nil {
		return nil
	}
	if i, ok := a.(Identity); ok {
		return i.Identity()
	}
	return a
}

// same reports whether a and b are the same actor.
func same(a, b Actor) bool {
	if a == nil || b == nil {
		return false
	}
	return identity(a) == identity(b)
}

// FlagSource answers a Flag for a mob. Component packages register sources
// for state they own (is_sitting, is_leashed, is_panicking ...): ok=false
// means "not mine, ask the next source / the default".
type FlagSource func(m *mob.Mob, f Flag) (v, ok bool)

// NumberSource answers a Number for a mob.
type NumberSource func(m *mob.Mob, n Number) (v float64, ok bool)

var (
	flagSources   []FlagSource
	numberSources []NumberSource
)

// RegisterFlagSource adds a FlagSource. Call it from init only.
func RegisterFlagSource(s FlagSource) { flagSources = append(flagSources, s) }

// RegisterNumberSource adds a NumberSource. Call it from init only.
func RegisterNumberSource(s NumberSource) { numberSources = append(numberSources, s) }

// liveComponents is implemented by the mob.Instance of package def: the
// component set including group changes of the event being run.
type liveComponents interface {
	HasComponent(name string) bool
	ComponentRaw(name string) (json.RawMessage, bool)
}

// EntityActor adapts a world.Entity.
type EntityActor struct {
	Tx *world.Tx
	E  world.Entity
}

// Identity is the entity handle: Dragonfly opens a new entity value per
// transaction lookup, so the handle is what stays the same.
func (a EntityActor) Identity() any { return a.E.H() }

// Position ...
func (a EntityActor) Position() mgl64.Vec3 { return a.E.Position() }

func (a EntityActor) mob() *mob.Mob {
	m, _ := a.E.(*mob.Mob)
	return m
}

func (a EntityActor) live() liveComponents {
	if m := a.mob(); m != nil {
		if inst := m.Instance(); inst != nil {
			if l, ok := inst.(liveComponents); ok {
				return l
			}
		}
	}
	return nil
}

// HasFamily ...
func (a EntityActor) HasFamily(f string) bool {
	if m := a.mob(); m != nil {
		if m.HasFamily(f) {
			return true
		}
		if m.ComponentsApplied() {
			// The attached type_family is the active one.
			return false
		}
		// type_family may not have been attached yet (the event that adds
		// the components is still running): read the JSON.
		if raw, ok := a.componentRaw("minecraft:type_family"); ok {
			return familyListHas(raw, f)
		}
		return false
	}
	return entityFamilyHas(a.E, f)
}

// HasComponent ...
func (a EntityActor) HasComponent(name string) bool {
	if l := a.live(); l != nil {
		return l.HasComponent(name)
	}
	if m := a.mob(); m != nil {
		return m.HasComponent(name)
	}
	return false
}

func (a EntityActor) componentRaw(name string) (json.RawMessage, bool) {
	if l := a.live(); l != nil {
		return l.ComponentRaw(name)
	}
	if m := a.mob(); m != nil {
		return m.ComponentJSON(name)
	}
	return nil, false
}

func (a EntityActor) block(p mgl64.Vec3) world.Block {
	if a.Tx == nil {
		return nil
	}
	return a.Tx.Block(cube.PosFromVec3(p))
}

func (a EntityActor) liquidAt(p mgl64.Vec3) (water, lava bool) {
	if a.Tx == nil {
		return false, false
	}
	l, ok := a.Tx.Liquid(cube.PosFromVec3(p))
	if !ok {
		return false, false
	}
	switch l.(type) {
	case block.Water:
		return true, false
	case block.Lava:
		return false, true
	}
	return false, false
}

// Flag ...
func (a EntityActor) Flag(f Flag) bool {
	m := a.mob()
	if m != nil {
		for _, s := range flagSources {
			if v, ok := s(m, f); ok {
				return v
			}
		}
	}
	pos := a.E.Position()
	switch f {
	case FlagLeashed:
		// Actor::isLeashed: the LEASHED actor flag (package mob/component/core).
		return m != nil && m.ActorFlag(protocol.EntityDataFlagLeashed)
	case FlagInWater:
		if m != nil {
			return m.Physics().InWater
		}
		w, _ := a.liquidAt(pos)
		return w
	case FlagInLava:
		if m != nil {
			return m.Physics().InLava
		}
		_, l := a.liquidAt(pos)
		return l
	case FlagUnderwater:
		if m != nil {
			return m.Physics().Underwater
		}
		eye := pos
		if p, ok := a.E.(*player.Player); ok {
			eye = p.Position().Add(mgl64.Vec3{0, p.EyeHeight()})
		}
		w, _ := a.liquidAt(eye)
		return w
	case FlagInWaterOrRain, FlagInContactWithWater:
		if a.Flag(FlagInWater) {
			return true
		}
		return a.Tx != nil && a.Tx.RainingAt(cube.PosFromVec3(pos))
	case FlagOnGround:
		if m != nil {
			return m.OnGround()
		}
		if g, ok := a.E.(interface{ OnGround() bool }); ok {
			return g.OnGround()
		}
	case FlagOnFire, FlagTakingFireDamage:
		if fd, ok := a.E.(interface{ OnFireDuration() time.Duration }); ok {
			return fd.OnFireDuration() > 0
		}
	case FlagOnHotBlock:
		_, ok := a.block(pos.Sub(mgl64.Vec3{0, 0.2})).(block.Magma)
		return ok
	case FlagBaby:
		return a.HasComponent("minecraft:is_baby")
	case FlagSneaking, FlagSneakHeld:
		if s, ok := a.E.(interface{ Sneaking() bool }); ok {
			return s.Sneaking()
		}
	case FlagSprinting:
		if s, ok := a.E.(interface{ Sprinting() bool }); ok {
			return s.Sprinting()
		}
	case FlagSleeping:
		if s, ok := a.E.(interface{ Sleeping() (cube.Pos, bool) }); ok {
			_, sl := s.Sleeping()
			return sl
		}
	case FlagSitting:
		if m != nil {
			return m.Sitting()
		}
	case FlagRiding:
		if m != nil {
			return m.Riding()
		}
		if r, ok := entity.RiderOf(a.E); ok {
			return r.RidingEntityHandle() != nil
		}
	case FlagMoving:
		if v, ok := a.E.(interface{ Velocity() mgl64.Vec3 }); ok {
			vel := v.Velocity()
			return vel[0]*vel[0]+vel[2]*vel[2] > 1e-6 || math.Abs(vel[1]) > 0.1
		}
	case FlagClimbing, FlagOnLadder:
		if m != nil && f == FlagClimbing {
			return m.Climbing()
		}
		_, ok := a.block(pos).(block.Ladder)
		return ok
	case FlagImmobile:
		if s, ok := a.E.(interface{ Immobile() bool }); ok {
			return s.Immobile()
		}
	case FlagTamed:
		if m != nil {
			return m.Tamed() || a.HasComponent("minecraft:is_tamed")
		}
	case FlagPersistent:
		if m != nil {
			return m.Persistent()
		}
	case FlagPanicking:
		return m != nil && m.Goals().Running("minecraft:behavior.panic")
	case FlagAvoidingMobs:
		return m != nil && m.Goals().Running("minecraft:behavior.avoid_mob_type")
	case FlagRaider:
		return a.HasFamily("illager") || a.HasFamily("witch") || a.HasFamily("ravager")
	case FlagVisible:
		if s, ok := a.E.(interface{ Invisible() bool }); ok {
			return !s.Invisible()
		}
		return true
	case FlagNavigating:
		return m != nil && m.Navigation() != nil && !m.Navigation().Done()
	case FlagNameTag:
		if s, ok := a.E.(interface{ NameTag() string }); ok {
			return s.NameTag() != ""
		}
	case FlagRangedWeapon:
		for _, it := range a.Equipment(DomainHand) {
			switch it.Name {
			case "minecraft:bow", "minecraft:crossbow", "minecraft:trident":
				return true
			}
		}
	case FlagInClouds:
		return pos[1] >= 192
	}
	return false
}

// Number ...
func (a EntityActor) Number(n Number) (float64, bool) {
	m := a.mob()
	if m != nil {
		for _, s := range numberSources {
			if v, ok := s(m, n); ok {
				return v, true
			}
		}
	}
	switch n {
	case NumberHealth:
		if h, ok := a.E.(interface{ Health() float64 }); ok {
			return h.Health(), true
		}
	case NumberMaxHealth:
		if h, ok := a.E.(interface{ MaxHealth() float64 }); ok {
			return h.MaxHealth(), true
		}
	case NumberRiderCount:
		if m != nil {
			return float64(m.RiderCount()), true
		}
		return 0, true
	case NumberVariant:
		return a.variant("minecraft:variant", func(m *mob.Mob) int32 { return m.Variant() })
	case NumberMarkVariant:
		return a.variant("minecraft:mark_variant", func(m *mob.Mob) int32 { return m.MarkVariant() })
	case NumberSkinID:
		return a.variant("minecraft:skin_id", func(m *mob.Mob) int32 { return m.SkinID() })
	case NumberYRotation:
		if m != nil {
			return m.BodyYaw(), true
		}
		if r, ok := a.E.(interface{ Rotation() cube.Rotation }); ok {
			return r.Rotation()[0], true
		}
	case NumberInactivitySeconds:
		if m != nil {
			return float64(m.InactivityTicks()) / 20, true
		}
	case NumberHomeDistance:
		if m != nil {
			if h, ok := m.Home(); ok {
				return h.Vec3Centre().Sub(m.Pos()).Len(), true
			}
		}
	}
	return 0, false
}

// variant reads {"value": n} of a variant-like component (BDS reads the
// component), falling back to the mob metadata.
func (a EntityActor) variant(key string, meta func(m *mob.Mob) int32) (float64, bool) {
	if raw, ok := a.componentRaw(key); ok {
		if v, ok := variantJSON.get(raw, func(raw json.RawMessage) (float64, bool) {
			var v struct {
				Value float64 `json:"value"`
			}
			if json.Unmarshal(raw, &v) != nil {
				return 0, false
			}
			return v.Value, true
		}); ok {
			return v, true
		}
	}
	if m := a.mob(); m != nil {
		return float64(meta(m)), true
	}
	return 0, false
}

// Related ...
func (a EntityActor) Related(r Relation) Actor {
	switch r {
	case RelationTarget:
		if m := a.mob(); m != nil {
			if t, ok := m.Target(); ok {
				return Wrap(a.Tx, t)
			}
		}
	case RelationVehicle:
		if r, ok := entity.RiderOf(a.E); ok && a.Tx != nil && r.RidingEntityHandle() != nil {
			if v, ok := r.RidingEntityHandle().Entity(a.Tx); ok {
				return Wrap(a.Tx, v)
			}
		}
	case RelationControllingPassenger:
		if m := a.mob(); m != nil {
			if p, ok := m.ControllingPassenger(); ok {
				return Wrap(a.Tx, p)
			}
		}
	case RelationOwner:
		if m := a.mob(); m != nil && m.Owner() != nil && a.Tx != nil {
			if o, ok := m.Owner().Entity(a.Tx); ok {
				return Wrap(a.Tx, o)
			}
		}
	case RelationLeashHolder:
		// The leash holder actor data (package mob/component/core).
		if m := a.mob(); m != nil && a.Tx != nil && m.ActorFlag(protocol.EntityDataFlagLeashed) {
			if v, ok := m.ActorData(protocol.EntityDataKeyLeashHolder); ok {
				if h, ok := v.(*world.EntityHandle); ok {
					if e, ok := h.Entity(a.Tx); ok {
						return Wrap(a.Tx, e)
					}
				}
			}
		}
	case RelationLastHurtBy:
		if m := a.mob(); m != nil {
			// BDS: the last player within 5 s or mob within 3 s.
			if e, tick, ok := m.LastHurtBy(); ok {
				limit := int64(60)
				if _, isPlayer := e.(*player.Player); isPlayer {
					limit = 100
				}
				if m.Ticks()-tick <= limit {
					return Wrap(a.Tx, e)
				}
			}
		}
	}
	return nil
}

// Equipment ...
func (a EntityActor) Equipment(d Domain) []Item {
	var main, off item.Stack
	var armour []item.Stack
	var inv []item.Stack
	var body item.Stack
	switch e := a.E.(type) {
	case *mob.Mob:
		main, off = e.HeldItems()
		if ar := e.Armour(); ar != nil {
			armour = ar.Slots()
		}
		body = e.BodyArmour()
		inv = e.InventoryItems()
	case *player.Player:
		main, off = e.HeldItems()
		armour = e.Armour().Slots()
		inv = e.Inventory().Slots()
	default:
		return nil
	}
	pick := func(s ...item.Stack) []Item {
		var out []Item
		for _, st := range s {
			if !st.Empty() {
				out = append(out, ItemOf(st))
			}
		}
		return out
	}
	at := func(i int) item.Stack {
		if i < len(armour) {
			return armour[i]
		}
		return item.Stack{}
	}
	switch d {
	case DomainAny:
		return pick(append([]item.Stack{main, off, body}, armour...)...)
	case DomainHand:
		return pick(main, off)
	case DomainMainHand:
		return pick(main)
	case DomainOffHand:
		return pick(off)
	case DomainArmor:
		return pick(append([]item.Stack{body}, armour...)...)
	case DomainHead:
		return pick(at(0))
	case DomainTorso:
		return pick(at(1))
	case DomainLeg:
		return pick(at(2))
	case DomainFeet:
		return pick(at(3))
	case DomainBody:
		return pick(body)
	case DomainInventory:
		return pick(inv...)
	}
	return nil
}

// PropertyValue ...
func (a EntityActor) PropertyValue(name string) (any, bool) {
	if m := a.mob(); m != nil && m.Instance() != nil {
		return m.Instance().Property(name)
	}
	return nil, false
}

// HasEffect ...
func (a EntityActor) HasEffect(name string) bool {
	l, ok := a.E.(interface{ Effects() []effect.Effect })
	if !ok {
		return false
	}
	for _, e := range l.Effects() {
		if name == "" || EffectName(e.Type()) == name {
			return true
		}
	}
	return false
}

// HasAbility ...
func (a EntityActor) HasAbility(name string) bool {
	p, ok := a.E.(*player.Player)
	if !ok {
		return false
	}
	gm := p.GameMode()
	switch name {
	case "instabuild":
		return gm.CreativeInventory()
	case "mayfly":
		return gm.AllowsFlying()
	case "flying":
		return p.Flying()
	case "invulnerable":
		return !gm.AllowsTakingDamage()
	case "build", "mine":
		return gm.AllowsEditing()
	case "attack_mobs", "attack_players", "doors_and_switches", "open_containers":
		return gm.AllowsInteraction()
	case "noclip":
		return !gm.HasCollision()
	}
	return false
}

// Colour ...
func (a EntityActor) Colour() string {
	if m := a.mob(); m != nil {
		return m.MobColour().String()
	}
	return ""
}

// ItemOf converts a stack.
func ItemOf(s item.Stack) Item {
	name, meta := s.Item().EncodeItem()
	it := Item{Name: name, Aux: int(meta)}
	if d := s.Durability(); d >= 0 {
		it.Damage = s.MaxDurability() - d
	}
	for _, e := range s.Enchantments() {
		if id, ok := item.EnchantmentID(e.Type()); ok && id >= 0 && id < len(enchantmentNames) {
			it.Enchantments = append(it.Enchantments, enchantmentNames[id])
		}
	}
	it.Tags = itemTags(name)
	return it
}

// itemTags derives the vanilla item tags the entity files test for from the
// item name (Dragonfly has no item tag registry).
func itemTags(name string) []string {
	n := stripNS(name)
	var tags []string
	switch {
	case strings.HasSuffix(n, "_axe"):
		tags = append(tags, "minecraft:is_axe", "minecraft:is_tool")
	case strings.HasSuffix(n, "_pickaxe"):
		tags = append(tags, "minecraft:is_pickaxe", "minecraft:is_tool")
	case strings.HasSuffix(n, "_shovel"):
		tags = append(tags, "minecraft:is_shovel", "minecraft:is_tool")
	case strings.HasSuffix(n, "_hoe"):
		tags = append(tags, "minecraft:is_hoe", "minecraft:is_tool")
	case strings.HasSuffix(n, "_sword"):
		tags = append(tags, "minecraft:is_sword", "minecraft:is_tool")
	case strings.HasSuffix(n, "_spear"):
		tags = append(tags, "minecraft:is_spear")
	case strings.HasSuffix(n, "_horse_armor"):
		tags = append(tags, "minecraft:horse_armor")
	case strings.HasSuffix(n, "_nautilus_armor"):
		tags = append(tags, "minecraft:nautilus_armor")
	}
	return tags
}

// enchantmentNames by Bedrock enchantment id.
var enchantmentNames = []string{
	"protection", "fire_protection", "feather_falling", "blast_protection", "projectile_protection",
	"thorns", "respiration", "depth_strider", "aqua_affinity", "sharpness", "smite",
	"bane_of_arthropods", "knockback", "fire_aspect", "looting", "efficiency", "silk_touch",
	"unbreaking", "fortune", "power", "punch", "flame", "infinity", "luck_of_the_sea", "lure",
	"frost_walker", "mending", "binding", "vanishing", "impaling", "riptide", "loyalty",
	"channeling", "multishot", "piercing", "quick_charge", "soul_speed", "swift_sneak",
	"wind_burst", "density", "breach", "lunge",
}

// effectNames by Bedrock effect id.
var effectNames = []string{
	"", "speed", "slowness", "haste", "mining_fatigue", "strength", "instant_health",
	"instant_damage", "jump_boost", "nausea", "regeneration", "resistance", "fire_resistance",
	"water_breathing", "invisibility", "blindness", "night_vision", "hunger", "weakness",
	"poison", "wither", "health_boost", "absorption", "saturation", "levitation", "fatal_poison",
	"conduit_power", "slow_falling", "bad_omen", "village_hero", "darkness", "trial_omen",
	"wind_charged", "weaving", "oozing", "infested", "raid_omen", "breath_of_the_nautilus",
}

// EffectName returns the Bedrock name of an effect type ("" if unknown).
func EffectName(t effect.Type) string {
	id, ok := effect.ID(t)
	if !ok || id < 0 || id >= len(effectNames) {
		return ""
	}
	return effectNames[id]
}

// DamageCause maps a damage source to its Bedrock cause name. Sources may
// implement interface{ BedrockCause() string } to override.
func DamageCause(src world.DamageSource) string {
	if c, ok := src.(interface{ BedrockCause() string }); ok {
		return c.BedrockCause()
	}
	switch s := src.(type) {
	case entity.AttackDamageSource:
		return "entity_attack"
	case entity.ProjectileDamageSource:
		return "projectile"
	case entity.ExplosionDamageSource:
		if s.Source == nil {
			return "block_explosion"
		}
		return "entity_explosion"
	case entity.FallDamageSource:
		return "fall"
	case entity.GlideDamageSource:
		return "fly_into_wall"
	case entity.VoidDamageSource:
		return "void"
	case entity.SuffocationDamageSource:
		return "suffocation"
	case entity.DrowningDamageSource:
		return "drowning"
	case entity.LightningDamageSource:
		return "lightning"
	case block.FireDamageSource:
		return "fire"
	case block.LavaDamageSource:
		return "lava"
	case block.MagmaDamageSource:
		return "magma"
	case block.DamageSource:
		return "contact"
	case effect.InstantDamageSource:
		return "magic"
	case effect.PoisonDamageSource:
		return "magic"
	case effect.WitherDamageSource:
		return "wither"
	}
	return "none"
}

// familyListHas reads {"family": [...]}.
func familyListHas(raw json.RawMessage, f string) bool {
	fams, ok := familyJSON.get(raw, func(raw json.RawMessage) ([]string, bool) {
		var tf struct {
			Family []string `json:"family"`
		}
		if json.Unmarshal(raw, &tf) != nil {
			return nil, false
		}
		return tf.Family, true
	})
	return ok && contains(fams, f)
}

var (
	familyJSON  rawCache[[]string]
	variantJSON rawCache[float64]
)

var familyCache sync.Map // identifier -> []string

// entityFamilyHas answers is_family for non-mob entities from the vanilla
// definition of their type (player: ["player"]; lightning_bolt: ["lightning"]; ...).
func entityFamilyHas(e world.Entity, f string) bool {
	if _, ok := e.(*player.Player); ok {
		return f == "player"
	}
	id := e.H().Type().EncodeEntity()
	if v, ok := familyCache.Load(id); ok {
		return contains(v.([]string), f)
	}
	var fams []string
	if t, ok := mob.TypeFor(id); ok {
		for _, c := range t.Definition().NewInstance().Components() {
			if c.Key == "minecraft:type_family" {
				var tf struct {
					Family []string `json:"family"`
				}
				_ = json.Unmarshal(c.Raw, &tf)
				fams = tf.Family
			}
		}
	} else if fs, ok := nonMobFamilies[id]; ok {
		fams = fs
	}
	familyCache.Store(id, fams)
	return contains(fams, f)
}

// nonMobFamilies are type_family values of vanilla non-mob entities that
// filters test for (other_with_families "lightning", arrows in damage filters).
var nonMobFamilies = map[string][]string{
	"minecraft:lightning_bolt": {"lightning"},
	"minecraft:arrow":          {"arrow"},
	"minecraft:thrown_trident": {"trident"},
	"minecraft:tnt":            {"tnt", "inanimate"},
	"minecraft:boat":           {"boat", "inanimate"},
	"minecraft:chest_boat":     {"boat", "inanimate"},
	"minecraft:minecart":       {"minecart", "inanimate"},
	"minecraft:armor_stand":    {"armor_stand", "inanimate", "mob"},
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// TxBlock adapts a block subject.
type TxBlock struct {
	B   world.Block
	Pos cube.Pos
	Tx  *world.Tx
}

// Name ...
func (b TxBlock) Name() string {
	n, _ := b.B.EncodeBlock()
	return n
}

// Waterlogged ...
func (b TxBlock) Waterlogged() bool {
	if b.Tx == nil {
		return false
	}
	_, ok := b.Tx.Liquid(b.Pos)
	if !ok {
		return false
	}
	_, isLiquid := b.B.(world.Liquid)
	return !isLiquid
}

// Position ...
func (b TxBlock) Position() mgl64.Vec3 { return b.Pos.Vec3Centre() }

// GameRules answers is_game_rule. It defaults to Bedrock's defaults for the
// rules vanilla entity files test (all true); set it to read real rules.
var GameRules = func(w *world.World, name string) bool {
	switch name {
	case "keepinventory", "doimmediaterespawn", "showcoordinates":
		return false
	}
	return true
}

// TxWorld adapts a transaction.
type TxWorld struct{ Tx *world.Tx }

// Time ...
func (w TxWorld) Time() int64 { return int64(w.Tx.World().Time()) }

// skyDarken is Dimension::getSkyDarken (0 day .. 11 night), with rain and
// thunder darkening the sky.
func (w TxWorld) skyDarken() int {
	a := celestialAngle(w.Time())
	d := 1 - (math.Cos(a*2*math.Pi)*2 + 0.5)
	d = math.Max(0, math.Min(1, d))
	d = 1 - d
	if w.Tx.Raining() {
		d *= 1 - 5.0/16
	}
	if w.Tx.Thundering() {
		d *= 1 - 5.0/16
	}
	return int((1 - d) * 11)
}

// Daytime ...
func (w TxWorld) Daytime() bool {
	if w.Tx.World().Dimension() != world.Overworld {
		return false
	}
	return w.skyDarken() < 4
}

// Difficulty ...
func (w TxWorld) Difficulty() int {
	switch w.Tx.World().Difficulty() {
	case world.DifficultyPeaceful:
		return 0
	case world.DifficultyEasy:
		return 1
	case world.DifficultyHard:
		return 3
	}
	return 2
}

// GameRule ...
func (w TxWorld) GameRule(name string) bool { return GameRules(w.Tx.World(), name) }

// Dimension ...
func (w TxWorld) Dimension() string {
	switch w.Tx.World().Dimension() {
	case world.Nether:
		return "nether"
	case world.End:
		return "the_end"
	}
	return "overworld"
}

// Brightness is BlockSource::getBrightness: the dimension brightness ramp
// applied to max(sky light - sky darkening, block light).
func (w TxWorld) Brightness(pos mgl64.Vec3) float64 {
	p := cube.PosFromVec3(pos)
	sky := int(w.Tx.SkyLight(p))
	if w.Tx.World().Dimension() == world.Overworld {
		sky -= w.skyDarken()
	}
	l := max(sky, int(w.Tx.BlockLight(p)), 0)
	f := float64(l) / 15
	ambient := 0.0
	if w.Tx.World().Dimension() == world.Nether {
		ambient = 0.1
	}
	return f/(4-3*f)*(1-ambient) + ambient
}

// LightLevel ...
func (w TxWorld) LightLevel(pos mgl64.Vec3) int { return int(w.Tx.Light(cube.PosFromVec3(pos))) }

// BiomeHasTag ...
func (w TxWorld) BiomeHasTag(pos mgl64.Vec3, tag string) bool {
	return contains(w.Tx.Biome(cube.PosFromVec3(pos)).Tags(), tag)
}

// Temperature ...
func (w TxWorld) Temperature(pos mgl64.Vec3) float64 {
	return w.Tx.Temperature(cube.PosFromVec3(pos))
}

// SnowCovered ...
func (w TxWorld) SnowCovered(pos mgl64.Vec3) bool { return w.Temperature(pos) < 0.15 }

// Humid ...
func (w TxWorld) Humid(pos mgl64.Vec3) bool {
	return w.Tx.Biome(cube.PosFromVec3(pos)).Rainfall() > 0.85
}

// Underground ...
func (w TxWorld) Underground(pos mgl64.Vec3) bool {
	p := cube.PosFromVec3(pos)
	return w.Tx.HighestLightBlocker(p[0], p[2]) > p[1]
}

// WeatherAt ...
func (w TxWorld) WeatherAt(pos mgl64.Vec3) Weather {
	p := cube.PosFromVec3(pos)
	rain, snow := w.Tx.RainingAt(p), w.Tx.SnowingAt(p)
	return Weather{Precipitation: rain || snow, Snow: snow, Thunder: w.Tx.ThunderingAt(p)}
}

// DimensionWeather ...
func (w TxWorld) DimensionWeather() Weather {
	return Weather{Precipitation: w.Tx.Raining(), Thunder: w.Tx.Thundering()}
}

// BlockName ...
func (w TxWorld) BlockName(pos mgl64.Vec3) string {
	n, _ := w.Tx.Block(cube.PosFromVec3(pos)).EncodeBlock()
	return n
}

// NearestPlayerDistance ...
func (w TxWorld) NearestPlayerDistance(pos mgl64.Vec3) (float64, bool) {
	best, ok := 0.0, false
	for p := range w.Tx.Players() {
		d := p.Position().Sub(pos).Len()
		if !ok || d < best {
			best, ok = d, true
		}
	}
	return best, ok
}

// RedstonePower ...
func (w TxWorld) RedstonePower(pos mgl64.Vec3) int {
	return w.Tx.RedstonePower(cube.PosFromVec3(pos))
}
