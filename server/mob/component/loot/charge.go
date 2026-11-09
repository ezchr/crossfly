//go:build crossfly_full

package loot

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// NeedsCharge reports whether m holds a crossbow that is not loaded
// (ItemStackBase::canBeCharged && !hasChargedItem): such a mob cannot use
// its ranged attack until minecraft:behavior.charge_held_item (package
// mob/goal/equip) loaded it. Mobs holding anything else, or nothing, never
// wait: BDS RangedAttackGoal does not ask for a bow, so skeletons without
// one still shoot.
func NeedsCharge(m *mob.Mob) bool {
	main, _ := m.HeldItems()
	c, ok := main.Item().(item.Crossbow)
	return ok && c.Item.Empty()
}

// Discharge empties the crossbow m holds after a shot (RangedAttackGoal::
// handleAttackBehavior: clearChargedItem) and clears the charged flag, so
// the loaded crossbow pose ends.
func Discharge(m *mob.Mob) {
	main, off := m.HeldItems()
	c, ok := main.Item().(item.Crossbow)
	if !ok || c.Item.Empty() {
		return
	}
	c.Item = item.Stack{}
	m.SetHeldItems(WithItem(main, c), off)
	m.SetActorFlag(protocol.EntityDataFlagCharged, false)
}

// WithItem returns s with its item replaced by it, keeping count,
// durability, enchantments, name and values.
func WithItem(s item.Stack, it world.Item) item.Stack {
	n := item.NewStack(it, s.Count()).WithEnchantments(s.Enchantments()...)
	if d := s.Durability(); d > 0 {
		n = n.WithDurability(d)
	}
	if name := s.CustomName(); name != "" {
		n = n.WithCustomName(name)
	}
	for k, v := range s.Values() {
		n = n.WithValue(k, v)
	}
	return n
}
