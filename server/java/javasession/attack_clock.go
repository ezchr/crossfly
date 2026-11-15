package javasession

import (
	"math"

	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// attackClock is the player's attack strength (the attack cooldown) as a vanilla server keeps it
// (Player.attackStrengthTicker): the ticks since the last attack, punch or change of the held item's
// type. Dragonfly's attacks don't depend on it; the attack sounds Java players hear do (attack.weak,
// .knockback, .sweep). Only used in player tasks.
type attackClock struct {
	ticks   int
	item    string     // the held item's type in the last tick
	last    mgl64.Vec3 // the position in the last tick
	moved   float64    // how far the player moved horizontally in the last tick
	started bool
}

// tick advances the clock by a tick (tickLoop), as Player.tick does: a different item in the main hand
// starts the cooldown over.
func (a *attackClock) tick(c session.Controllable, pos mgl64.Vec3) {
	a.ticks++
	held, _ := c.HeldItems()
	name := itemTypeName(held)
	if a.started {
		if name != a.item {
			a.ticks = 0
		}
		a.moved = math.Hypot(pos[0]-a.last[0], pos[2]-a.last[2])
	}
	a.item, a.last, a.started = name, pos, true
}

// reset starts the cooldown over: a 26.3 punch (ServerGamePacketListenerImpl.handlePunch).
func (a *attackClock) reset() { a.ticks = 0 }

// attack gives the player's next attack its strength (Player.getAttackStrengthScale(0.5)) and starts the
// cooldown over, as Player.attack does (onAttack).
func (a *attackClock) attack(c session.Controllable) {
	held, _ := c.HeldItems()
	strength := attackStrength(a.ticks, javaAttackSpeed(c, held))
	a.ticks = 0
	if p, ok := c.(interface{ SetNextJavaAttack(player.JavaAttack) }); ok {
		p.SetNextJavaAttack(player.JavaAttack{Strength: strength, Movement: a.moved})
	}
}

// attackStrength is the attack strength after ticks ticks with an attack speed: from 0 to 1, full once
// 20/speed ticks passed (Player.getAttackStrengthScale(0.5)).
func attackStrength(ticks int, speed float64) float64 {
	if speed <= 0 {
		return 0
	}
	return min(max((float64(ticks)+0.5)/(20/speed), 0), 1)
}

// itemTypeName is the name of a stack's item type ("" for an empty hand): vanilla's ItemStack.isSameItem
// compares only that.
func itemTypeName(s item.Stack) string {
	if s.Empty() {
		return ""
	}
	name, _ := s.Item().EncodeItem()
	return name
}

// javaAttackSpeed is the player's Java attack speed attribute with an item in the main hand: 4, plus the
// item's base_attack_speed modifier, times 1.1 per Haste level and 0.9 per Mining Fatigue level
// (ADD_MULTIPLIED_TOTAL modifiers).
func javaAttackSpeed(c session.Controllable, held item.Stack) float64 {
	speed := 4 + javaAttackSpeedModifier(held.Item())
	if e, ok := c.(interface {
		Effect(effect.Type) (effect.Effect, bool)
	}); ok {
		if h, ok := e.Effect(effect.Haste); ok {
			speed *= 1 + 0.1*float64(h.Level())
		}
		if f, ok := e.Effect(effect.MiningFatigue); ok {
			speed *= 1 - 0.1*float64(f.Level())
		}
	}
	return speed
}

// javaAttackSpeedModifier is the attack speed modifier of a Java weapon or tool (Items.java of 26.3).
func javaAttackSpeedModifier(it world.Item) float64 {
	switch it := it.(type) {
	case item.Sword:
		return -2.4
	case item.Pickaxe:
		return -2.8
	case item.Shovel:
		return -3
	case item.Axe:
		switch it.Tier.Name {
		case "wooden", "stone", "copper":
			return -3.2
		case "iron":
			return -3.1
		}
		return -3
	case item.Hoe:
		switch it.Tier.Name {
		case "wooden", "golden":
			return -3
		case "stone", "copper":
			return -2
		case "iron":
			return -1
		}
		return 0
	case item.Mace:
		return -3.4
	}
	return 0
}
