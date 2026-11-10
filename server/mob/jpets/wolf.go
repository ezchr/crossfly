//go:build crossfly_full

package jpets

import (
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// Wolf rules (animal/wolf/Wolf.java) that do not depend on the goal system:
// what a player's hand does to a wolf, which targets a tame wolf fights
// for its owner, how wolf armour takes damage, the wet shake.

// Wolf constants (Wolf.START_HEALTH, TAME_HEALTH, ARMOR_REPAIR_UNIT,
// DEFAULT_COLLAR_COLOR red).
const (
	wolfStartHealth     = 8.0
	wolfTameHealth      = 40.0
	wolfArmourRepair    = 0.125
	defaultCollarColour = DyeRed
)

// IsWolfFood is ItemTags.WOLF_FOOD: #meat, cod, salmon (raw and cooked),
// tropical fish, pufferfish and rabbit stew.
func IsWolfFood(name string) bool {
	switch name {
	case "minecraft:beef", "minecraft:chicken", "minecraft:cooked_beef", "minecraft:cooked_chicken",
		"minecraft:cooked_mutton", "minecraft:cooked_porkchop", "minecraft:cooked_rabbit", "minecraft:mutton",
		"minecraft:porkchop", "minecraft:rabbit", "minecraft:rotten_flesh",
		"minecraft:cod", "minecraft:cooked_cod", "minecraft:salmon", "minecraft:cooked_salmon",
		"minecraft:tropical_fish", "minecraft:pufferfish", "minecraft:rabbit_stew":
		return true
	}
	return false
}

// ScaresCreepersAndPhantoms reports whether a mob of type id makes creepers
// flee (Creeper's AvoidEntityGoal for Ocelot and Cat) and keeps phantoms
// from swooping (Phantom.PhantomSweepAttackGoal looks for cats).
func ScaresCreepersAndPhantoms(id string) bool {
	return id == "minecraft:cat" || id == "minecraft:ocelot"
}

// WolfWantsToAttack is Wolf.wantsToAttack (also used by the wolf's owner
// goals): never creepers, ghasts or armour stands, a wolf only when it is
// wild or not the owner's, a player only if the owner may hurt players, never
// a tamed horse or another tamed pet.
func WolfWantsToAttack(target, owner world.Entity) bool {
	id := entityID(target)
	switch id {
	case "minecraft:creeper", "minecraft:ghast", "minecraft:armor_stand":
		return false
	case "minecraft:wolf":
		if t, ok := javamob.Of(target); ok {
			if b, ok := t.Behaviour().(*Wolf); ok && b.Tame {
				return !isOwnedBy(t, owner)
			}
		}
		return true
	}
	if tp, ok := target.(*player.Player); ok {
		if op, ok := owner.(*player.Player); ok && !canHarmPlayer(op, tp) {
			return false
		}
	}
	if t, ok := javamob.Of(target); ok {
		switch b := t.Behaviour().(type) {
		case interface{ IsTamedHorse() bool }:
			if b.IsTamedHorse() {
				return false
			}
		case interface{ IsTame() bool }:
			if b.IsTame() {
				return false
			}
		}
	}
	return true
}

// canHarmPlayer is Player.canHarmPlayer: teams aside (Crossfly has none
// that forbid it), players may hurt each other unless PvP is off.
func canHarmPlayer(owner, target *player.Player) bool {
	return true
}

// entityID is the identifier of an entity's type.
func entityID(e world.Entity) string {
	if e == nil {
		return ""
	}
	return e.H().Type().EncodeEntity()
}

// bypassesWolfArmour is DamageTypeTags.BYPASSES_WOLF_ARMOR: drowning,
// starving, suffocating, the void and world border, magic (instant damage
// and poison), wither and thorns go past the armour.
func bypassesWolfArmour(src world.DamageSource) bool {
	switch src.(type) {
	case entity.DrowningDamageSource, entity.VoidDamageSource, entity.SuffocationDamageSource,
		player.StarvationDamageSource, player.BorderDamageSource,
		effect.InstantDamageSource, effect.PoisonDamageSource, effect.WitherDamageSource,
		enchantment.ThornsDamageSource:
		return true
	}
	return false
}

// crackLevel is Crackiness.WOLF_ARMOR.byDamage: 0 none, 1 low, 2 medium,
// 3 high, by the share of durability left (0.95, 0.69, 0.32).
func crackLevel(damage, max int) int {
	if max <= 0 {
		return 0
	}
	f := float32(max-damage) / float32(max)
	switch {
	case f < 0.32:
		return 3
	case f < 0.69:
		return 2
	case f < 0.95:
		return 1
	}
	return 0
}

// WolfArmourAbsorb is Wolf.actuallyHurt with wolf armour on: the armour
// loses ceil(damage) durability instead of the wolf losing health (unless
// the cause bypasses it). It returns the armour left (empty when it broke),
// whether the armour took the hit and whether its cracks grew (the crack
// sound and scute particles).
func WolfArmourAbsorb(armour item.Stack, dmg float64, src world.DamageSource) (left item.Stack, absorbed, cracked bool) {
	if itemName(armour) != "minecraft:wolf_armor" || bypassesWolfArmour(src) {
		return armour, false, false
	}
	max := armour.MaxDurability()
	before := max - armour.Durability()
	n := int(ceil32(dmg))
	left = armour.Damage(n)
	after := max
	if !left.Empty() {
		after = max - left.Durability()
	}
	return left, true, crackLevel(before, max) != crackLevel(after, max)
}

func ceil32(v float64) float32 {
	f := float32(v)
	i := float32(int32(f))
	if i < f {
		i++
	}
	return i
}

// wolfAmbientEvent is Wolf.getAmbientSound: growl when angry, else one in
// three a whine (tame below 20 health) or pant, else the normal bark. It is
// a Bedrock level sound event name (javasession plays the Java sound of the
// wolf's sound variant).
func (w *Wolf) wolfAmbientEvent(m *javamob.Mob) string {
	switch {
	case w.angry(m):
		return "growl"
	case m.Rand().IntN(3) == 0:
		if w.Tame && m.Health() < 20 {
			return "whine"
		}
		return "pant"
	}
	return mob.SoundAmbient
}

// angry is NeutralMob.isAngry, shown as Meta.Angry.
func (w *Wolf) angry(m *javamob.Mob) bool { return w.Anger.IsAngry(m) }

// setInterested is Wolf.setIsInterested (DATA_INTERESTED_ID): the head tilt
// of BegGoal, the interested actor flag on both clients.
func (w *Wolf) setInterested(m *javamob.Mob, v bool) {
	w.Interested = v
	if m.ActorFlag(protocol.EntityDataFlagInterested) != v {
		m.SetActorFlag(protocol.EntityDataFlagInterested, v)
	}
}
