//go:build crossfly_full

package misc

import (
	"encoding/json"
	"fmt"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// attackerOf returns the entity responsible for src (the attacker, or a
// projectile's owner).
func attackerOf(src world.DamageSource) world.Entity {
	switch s := src.(type) {
	case entity.AttackDamageSource:
		return s.Attacker
	case entity.ProjectileDamageSource:
		if s.Owner != nil {
			return s.Owner
		}
		return s.Projectile
	}
	return nil
}

// OnHurt is minecraft:on_hurt and minecraft:on_hurt_by_player (BDS
// OnHurtDefinition/OnHurtByPlayerDefinition, trigger definitions): the event
// fires whenever the mob takes damage (by a player, for the second), with
// the attacker as "other" (and "player").
type OnHurt struct {
	trigger  sense.Trigger
	byPlayer bool
}

func newOnHurt(key string, raw json.RawMessage) (mob.Component, error) {
	t, err := sense.ParseTrigger(raw)
	if err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	return &OnHurt{trigger: t, byPlayer: key == "minecraft:on_hurt_by_player"}, nil
}

// Attach ...
func (*OnHurt) Attach(*mob.Mob) {}

// Detach ...
func (*OnHurt) Detach(*mob.Mob) {}

// OnHurt fires the trigger.
func (c *OnHurt) OnHurt(m *mob.Mob, _ float64, src world.DamageSource) {
	a := attackerOf(src)
	if c.byPlayer {
		if _, ok := a.(*player.Player); !ok {
			return
		}
	}
	ctx := m.Ctx()
	ctx.Damage = src
	if a != nil {
		ctx.Other, ctx.Damager = a, a
		if _, ok := a.(*player.Player); ok {
			ctx.Player = a
		}
	}
	c.trigger.Run(m, ctx)
}

// OnDeath is minecraft:on_death (BDS OnDeathDefinition): the event fires
// when the mob dies (the ender dragon starts its death sequence with it),
// with the killer as "other".
type OnDeath struct{ trigger sense.Trigger }

func newOnDeath(key string, raw json.RawMessage) (mob.Component, error) {
	t, err := sense.ParseTrigger(raw)
	if err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	return &OnDeath{trigger: t}, nil
}

// Attach ...
func (*OnDeath) Attach(*mob.Mob) {}

// Detach ...
func (*OnDeath) Detach(*mob.Mob) {}

// OnDeath fires the trigger.
func (c *OnDeath) OnDeath(m *mob.Mob, src world.DamageSource) {
	ctx := m.Ctx()
	ctx.Damage = src
	if a := attackerOf(src); a != nil {
		ctx.Other, ctx.Damager = a, a
	}
	// sense.Trigger.Fire skips dead subjects; the dying mob itself must
	// still get its own on_death event.
	if c.trigger.Empty() || !c.trigger.Test(m, ctx) {
		return
	}
	if c.trigger.Target == "self" || c.trigger.Target == "" {
		m.Fire(c.trigger.Event, ctx)
		return
	}
	c.trigger.Fire(m, ctx)
}

// OnFriendlyAnger is minecraft:on_friendly_anger (BDS
// OnFriendlyAngerDefinition): the event fires when a nearby mob of the same
// type becomes angry. "Nearby" is the angry mob's minecraft:angry
// broadcast_range (20 when it has none).
type OnFriendlyAnger struct{ trigger sense.Trigger }

func newOnFriendlyAnger(key string, raw json.RawMessage) (mob.Component, error) {
	t, err := sense.ParseTrigger(raw)
	if err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	return &OnFriendlyAnger{trigger: t}, nil
}

// Attach ...
func (*OnFriendlyAnger) Attach(*mob.Mob) {}

// Detach ...
func (*OnFriendlyAnger) Detach(*mob.Mob) {}

type angerKey struct{}

// friendlyAngerTick runs for every mob: when its Angry state turns on, the
// mobs of its type nearby with on_friendly_anger fire their event.
func friendlyAngerTick(m *mob.Mob) {
	angry := m.Meta().Angry
	was, _ := m.Data(angerKey{})
	if angry == (was == true) {
		return
	}
	if angry {
		m.SetData(angerKey{}, true)
	} else {
		m.SetData(angerKey{}, nil)
		return
	}
	r := 20.0
	if raw, ok := m.ComponentJSON("minecraft:angry"); ok {
		var v struct {
			BroadcastRange *float64 `json:"broadcast_range"`
		}
		if json.Unmarshal(raw, &v) == nil && v.BroadcastRange != nil {
			r = *v.BroadcastRange
		}
	}
	p := m.Position()
	target, _ := m.Target()
	for _, e := range entitiesIn(m, p[0]-r, p[1]-r, p[2]-r, p[0]+r, p[1]+r, p[2]+r) {
		om, ok := mob.Of(e)
		if !ok || om.Identifier() != m.Identifier() || om.Dead() || om.Pos().Sub(p).LenSqr() > r*r {
			continue
		}
		c, ok := mob.ComponentOf[*OnFriendlyAnger](om)
		if !ok {
			continue
		}
		ctx := om.Ctx()
		ctx.Other = m
		ctx.Target = target
		c.trigger.Run(om, ctx)
	}
}

// OnEquipmentChanged is minecraft:on_equipment_changed (1.26, sulfur cube):
// for each slot, on_equip fires when the slot goes from empty to filled and
// on_unequip when it empties.
type OnEquipmentChanged struct {
	slots []equipSlot
}

type equipSlot struct {
	slot       string
	equip      sense.Trigger
	unequip    sense.Trigger
	had, known bool
}

func newOnEquipmentChanged(key string, raw json.RawMessage) (mob.Component, error) {
	v := struct {
		Slots []struct {
			Slot      string          `json:"slot"`
			OnEquip   json.RawMessage `json:"on_equip"`
			OnUnequip json.RawMessage `json:"on_unequip"`
		} `json:"slots"`
	}{}
	if err := decode(raw, &v); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	c := &OnEquipmentChanged{}
	for _, s := range v.Slots {
		e, err := sense.ParseTrigger(s.OnEquip)
		if err != nil {
			return nil, fmt.Errorf("%v: on_equip: %w", key, err)
		}
		u, err := sense.ParseTrigger(s.OnUnequip)
		if err != nil {
			return nil, fmt.Errorf("%v: on_unequip: %w", key, err)
		}
		c.slots = append(c.slots, equipSlot{slot: s.Slot, equip: e, unequip: u})
	}
	return c, nil
}

// Attach records the current equipment, so only later changes fire.
func (c *OnEquipmentChanged) Attach(m *mob.Mob) {
	for i := range c.slots {
		c.slots[i].had, c.slots[i].known = !SlotItem(m, c.slots[i].slot).Empty(), true
	}
}

// Detach ...
func (*OnEquipmentChanged) Detach(*mob.Mob) {}

func (c *OnEquipmentChanged) tick(m *mob.Mob) {
	for i := range c.slots {
		s := &c.slots[i]
		has := !SlotItem(m, s.slot).Empty()
		if s.known && has == s.had {
			continue
		}
		s.had, s.known = has, true
		if has {
			s.equip.Fire(m, nil)
		} else {
			s.unequip.Fire(m, nil)
		}
	}
}

// SlotItem returns the item in a Bedrock equipment slot name
// (slot.weapon.mainhand, slot.armor.head, ...).
func SlotItem(m *mob.Mob, slot string) item.Stack {
	main, off := m.HeldItems()
	a := m.Armour()
	switch slot {
	case "slot.weapon.mainhand":
		return main
	case "slot.weapon.offhand":
		return off
	case "slot.armor.head":
		return a.Helmet()
	case "slot.armor.chest":
		return a.Chestplate()
	case "slot.armor.legs":
		return a.Leggings()
	case "slot.armor.feet":
		return a.Boots()
	case "slot.armor.body":
		return m.BodyArmour()
	}
	return item.Stack{}
}
