package javasession

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/item/inventory"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/java/protocol/version"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// A Java client applies Efficiency, Aqua Affinity, Depth Strider, Swift Sneak, Respiration and Soul
// Speed to its own mining and movement only through the player's attributes: a vanilla server adds
// them as attribute modifiers while the item is equipped (the enchantments' minecraft:attributes
// and minecraft:location_changed effects) and syncs them. Without them a Java player mines, swims,
// sneaks and walks on soul sand as if the items had no enchantments. syncEnchantAttributes works
// them out from what the player holds and wears every tick and sends those that changed, as base
// values without modifiers, for any level up to the attribute's range.
//
// The armour bar comes from attributes the same way: a vanilla server adds the armour's
// minecraft:armor and minecraft:armor_toughness modifiers to the player, and the client draws the
// bar from the armor attribute (Player.getArmorValue). They are sent here as well, the sums of
// what the worn armour gives, as Dragonfly adds them up for its damage reduction
// (inventory.Armour.DamageReduction).

// enchantAttr is one of the attributes syncEnchantAttributes sends.
type enchantAttr int

const (
	attrMiningEfficiency enchantAttr = iota
	attrSubmergedMiningSpeed
	attrWaterMovementEfficiency
	attrSneakingSpeed
	attrOxygenBonus
	attrMovementEfficiency
	attrArmor
	attrArmorToughness
	enchantAttrCount
)

var (
	enchantAttrNames = [enchantAttrCount]string{
		"mining_efficiency", "submerged_mining_speed", "water_movement_efficiency",
		"sneaking_speed", "oxygen_bonus", "movement_efficiency", "armor", "armor_toughness",
	}
	// enchantAttrDefaults are the values of a new Java player entity (Attributes, 26.3).
	enchantAttrDefaults = [enchantAttrCount]float64{0, 0.2, 0, 0.3, 0, 0, 0, 0}
	// enchantAttrMax are the attributes' upper bounds.
	enchantAttrMax = [enchantAttrCount]float64{1024, 20, 1, 1, 1024, 1, 30, 20}
	enchantAttrIDs = func() (ids [enchantAttrCount]int32) {
		for i, n := range enchantAttrNames {
			ids[i] = v777.BuiltinID("minecraft:attribute", "minecraft:"+n)
		}
		return ids
	}()
)

// enchantAttrState is what the session last sent of these attributes and of the movement speed.
type enchantAttrState struct {
	// sent are the values the client has; valid is false while it has the defaults.
	sent  [enchantAttrCount]float64
	valid bool
	// speed is the movement speed SendSpeed was last given (speedSet: at all).
	speed    float64
	speedSet bool
	// soulSpeed is the level of Soul Speed in effect, 0 if none.
	soulSpeed int
}

// respawned is called when the client gets a new player entity, which has the default attributes.
func (st *enchantAttrState) respawned() {
	st.valid, st.soulSpeed = false, 0
}

// enchantAttributeValues returns the attributes that held (the main hand item) and armour (the
// helmet, chestplate, leggings and boots, empty stacks for free slots) give a Java player, with
// Soul Speed's movement_efficiency if soulSpeed is in effect. The formulas are those of the
// enchantments' data in 26.3; Aqua Affinity is the one exception: Dragonfly, following Bedrock,
// lifts the underwater mining slowdown with any level of it and no more (block.BreakDuration),
// which is a submerged_mining_speed of 1.
func enchantAttributeValues(held item.Stack, armour [4]item.Stack, soulSpeed int) [enchantAttrCount]float64 {
	v := enchantAttrDefaults
	if e, ok := held.Enchantment(enchantment.Efficiency); ok {
		v[attrMiningEfficiency] = enchantment.Efficiency.Addend(e.Level())
	}
	if _, ok := armour[0].Enchantment(enchantment.AquaAffinity); ok {
		v[attrSubmergedMiningSpeed] = 1
	}
	if e, ok := armour[0].Enchantment(enchantment.Respiration); ok {
		v[attrOxygenBonus] = float64(e.Level())
	}
	if e, ok := armour[2].Enchantment(enchantment.SwiftSneak); ok {
		v[attrSneakingSpeed] += 0.15 * float64(e.Level())
	}
	// Bedrock takes the highest Depth Strider level of any armour piece.
	depthStrider := 0
	for _, it := range armour {
		if e, ok := it.Enchantment(enchantment.DepthStrider); ok {
			depthStrider = max(depthStrider, e.Level())
		}
	}
	v[attrWaterMovementEfficiency] = float64(depthStrider) / 3
	if soulSpeed > 0 {
		v[attrMovementEfficiency] = 1
	}
	for _, it := range armour {
		if a, ok := it.Item().(item.Armour); ok {
			v[attrArmor] += a.DefencePoints()
			v[attrArmorToughness] += a.Toughness()
		}
	}
	for i := range v {
		v[i] = min(v[i], enchantAttrMax[i])
	}
	return v
}

// soulSpeedLevel returns the level of Soul Speed in effect for a player whose boots are boots:
// standing on soul sand or soul soil, and still while in the air after that (vanilla's
// enchantment_active_check), but not while flying or riding.
func soulSpeedLevel(boots item.Stack, onSoulBlock, onGround, wasActive, flying, riding bool) int {
	e, ok := boots.Enchantment(enchantment.SoulSpeed)
	if !ok || flying || riding {
		return 0
	}
	if onSoulBlock || (wasActive && !onGround) {
		return e.Level()
	}
	return 0
}

// soulSpeedFactor is what Soul Speed of level multiplies the movement speed by. Bedrock and Java
// both add 0.03+0.0105*level to the base movement speed (0.1) before sprinting and effects
// multiply it, which Dragonfly's speed already holds: the boost is scaled the same way.
func soulSpeedFactor(level int) float64 {
	if level <= 0 {
		return 1
	}
	return 1 + (0.03+0.0105*float64(level))/0.1
}

// syncEnchantAttributes sends the enchantment attributes of c's equipment that changed, and the
// movement speed when Soul Speed comes into or goes out of effect. It is called every tick.
func (s *Session) syncEnchantAttributes(tx *world.Tx, c session.Controllable) {
	st := &s.enchAttrs
	held, _ := c.HeldItems()
	var armour [4]item.Stack
	if a, ok := c.(interface{ Armour() *inventory.Armour }); ok {
		copy(armour[:], a.Armour().Slots())
	}
	onGround := false
	if g, ok := c.(interface{ OnGround() bool }); ok {
		onGround = g.OnGround()
	}
	_, riding := c.RidingEntity(tx)
	// The block that affects movement (Entity.getBlockPosBelowThatAffectsMyMovement).
	below := tx.Block(cube.PosFromVec3(c.Position().Sub(mgl64.Vec3{0, 0.500001, 0})))
	_, soulSand := below.(block.SoulSand)
	_, soulSoil := below.(block.SoulSoil)
	soul := soulSpeedLevel(armour[3], soulSand || soulSoil, onGround, st.soulSpeed > 0, c.Flying(), riding)

	v := enchantAttributeValues(held, armour, soul)
	prev := enchantAttrDefaults
	if st.valid {
		prev = st.sent
	}
	st.sent, st.valid = v, true
	var ids [enchantAttrCount]int32
	n := 0
	for i := range v {
		if v[i] == prev[i] {
			ids[i] = -1
			continue
		}
		ids[i] = enchantAttrIDs[i]
		if l := s.legacy(); l != nil && ids[i] >= 0 {
			ids[i] = version.Map(l.attribute, ids[i])
		}
		if ids[i] >= 0 {
			n++
		}
	}
	if n > 0 {
		w := s.packet()
		w.VarInt(selfEntityID)
		w.VarInt(int32(n))
		for i, id := range ids {
			if id >= 0 {
				w.VarInt(id)
				w.Float64(v[i])
				w.VarInt(0) // modifiers
			}
		}
		s.queue(v777.ClientboundPlayUpdateAttributes, w)
	}
	if soul != st.soulSpeed {
		st.soulSpeed = soul
		if !st.speedSet {
			st.speed, st.speedSet = c.Speed(), true
		}
		s.writeSpeed()
	}
}
