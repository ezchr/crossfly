//go:build crossfly_full

package core

import (
	"encoding/json"
	"fmt"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/loot"
	"github.com/df-mc/dragonfly/server/world"
)

// Equipment slots as BDS SlotWithDropChance numbers them (and as
// minecraft:equipment slot_drop_chance names them).
const (
	SlotMainHand = iota
	SlotOffHand
	SlotHead
	SlotChest
	SlotLegs
	SlotFeet
	slotCount
)

// SlotNames are the slot_drop_chance names of the slots, by Slot* index.
var SlotNames = [slotCount]string{"slot.weapon.mainhand", "slot.weapon.offhand", "slot.armor.head",
	"slot.armor.chest", "slot.armor.legs", "slot.armor.feet"}

// DefaultDropChance is the drop chance of a slot nothing set (BDS
// SlotDropChancesComponent default, 8.5%).
const DefaultDropChance = 0.085

// equipment is minecraft:equipment {"table", "slot_drop_chance"} (BDS
// EquipmentTableDefinition): when the component is added (spawn or a
// component group, not for mobs loaded from disk, whose gear was saved) the
// mob is equipped from the loot table (Actor::equipFromEquipmentDefinition),
// and the listed slots get their drop chances (SlotDropChanceUtility::
// setForSlots). The gear is rolled on the component's first tick.
type equipment struct {
	table  string
	chance map[int]float64
	rolled bool
}

func newEquipment(key string, raw json.RawMessage) (mob.Component, error) {
	var j struct {
		Table  string `json:"table"`
		Chance []struct {
			Slot   string  `json:"slot"`
			Chance float64 `json:"drop_chance"`
		} `json:"slot_drop_chance"`
	}
	if err := decode(raw, &j); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	if j.Table != "" {
		warnMissingTable(j.Table)
	}
	c := &equipment{table: j.Table, chance: map[int]float64{}}
	for _, s := range j.Chance {
		if i := slotIndex(s.Slot); i >= 0 {
			c.chance[i] = s.Chance
		}
	}
	return c, nil
}

func slotIndex(name string) int {
	for i, n := range SlotNames {
		if n == name {
			return i
		}
	}
	return -1
}

// Attach sets the slot drop chances.
func (c *equipment) Attach(m *mob.Mob) {
	for slot, ch := range c.chance {
		SetDropChance(m, slot, ch)
	}
}

func (*equipment) Detach(*mob.Mob) {}

// Tick rolls the gear on the first tick.
func (c *equipment) Tick(m *mob.Mob) {
	if c.rolled {
		return
	}
	c.rolled = true
	EquipFromTable(m, c.table)
}

// RollGear implements mob.GearRoller.
func (c *equipment) RollGear(m *mob.Mob) { c.Tick(m) }

// RollEquipment rolls m's minecraft:equipment table now if its first tick
// has not done so yet (for /mobdebug and tests). It reports whether m has
// the component.
func RollEquipment(m *mob.Mob) bool {
	c, ok := mob.ComponentOf[*equipment](m)
	if ok {
		c.Tick(m)
	}
	return ok
}

// EquipFromTable rolls the loot table at path for m and equips the items
// (BDS Actor::equip(string)). It does nothing for an empty or missing table.
func EquipFromTable(m *mob.Mob, path string) {
	if path == "" {
		return
	}
	t, err := LoadLootTable(path)
	if err != nil {
		return
	}
	Equip(m, t.Roll(&LootContext{Mob: m}))
}

// Equip puts stacks in the slots they belong to, replacing what is there
// (BDS Actor::equip): armour and items wearable on the head (skulls, carved
// pumpkins, banners: illager captains) in their armour slot, off-hand items
// (nautilus shells, shields, totems, ...) in the off hand, everything else
// in the main hand.
func Equip(m *mob.Mob, stacks []item.Stack) {
	if len(stacks) == 0 {
		return
	}
	a := m.Armour()
	main, off := m.HeldItems()
	hands := false
	for _, s := range stacks {
		if s.Empty() {
			continue
		}
		switch EquipSlotOf(s) {
		case SlotHead:
			a.SetHelmet(s)
		case SlotChest:
			a.SetChestplate(s)
		case SlotLegs:
			a.SetLeggings(s)
		case SlotFeet:
			a.SetBoots(s)
		case SlotOffHand:
			off, hands = s, true
		default:
			main, hands = s, true
		}
	}
	if hands {
		m.SetHeldItems(main, off)
	}
}

// EquipSlotOf returns the slot Equip puts s in.
func EquipSlotOf(s item.Stack) int {
	switch it := s.Item().(type) {
	case item.HelmetType:
		if it.Helmet() {
			return SlotHead
		}
	case item.ChestplateType:
		if it.Chestplate() {
			return SlotChest
		}
	case item.LeggingsType:
		if it.Leggings() {
			return SlotLegs
		}
	case item.BootsType:
		if it.Boots() {
			return SlotFeet
		}
	case block.Banner:
		return SlotHead
	}
	if o, ok := s.Item().(item.OffHand); ok && o.OffHand() {
		return SlotOffHand
	}
	return SlotMainHand
}

// EquipmentSlot returns what m has in slot (a Slot* constant).
func EquipmentSlot(m *mob.Mob, slot int) item.Stack {
	switch slot {
	case SlotMainHand:
		s, _ := m.HeldItems()
		return s
	case SlotOffHand:
		_, s := m.HeldItems()
		return s
	}
	a := m.Armour()
	switch slot {
	case SlotHead:
		return a.Helmet()
	case SlotChest:
		return a.Chestplate()
	case SlotLegs:
		return a.Leggings()
	case SlotFeet:
		return a.Boots()
	}
	return item.Stack{}
}

// SetEquipmentSlot sets slot (a Slot* constant) to s and shows it.
func SetEquipmentSlot(m *mob.Mob, slot int, s item.Stack) {
	main, off := m.HeldItems()
	a := m.Armour()
	switch slot {
	case SlotMainHand:
		m.SetHeldItems(s, off)
	case SlotOffHand:
		m.SetHeldItems(main, s)
	case SlotHead:
		a.SetHelmet(s)
	case SlotChest:
		a.SetChestplate(s)
	case SlotLegs:
		a.SetLeggings(s)
	case SlotFeet:
		a.SetBoots(s)
	}
}

// dropChances is the per-mob SlotDropChancesComponent.
type dropChances struct{ c [slotCount]float64 }

type dropChancesKey struct{}

func chancesOf(m *mob.Mob) *dropChances {
	if v, ok := m.Data(dropChancesKey{}); ok {
		return v.(*dropChances)
	}
	d := &dropChances{}
	for i := range d.c {
		d.c[i] = DefaultDropChance
	}
	// Vex::initializeComponents sets both hands to 0 when spawned.
	if m.Identifier() == "minecraft:vex" {
		d.c[SlotMainHand], d.c[SlotOffHand] = 0, 0
	}
	m.SetData(dropChancesKey{}, d)
	return d
}

// DropChance returns the chance slot drops when m dies (BDS
// SlotDropChanceUtility::getForSlot).
func DropChance(m *mob.Mob, slot int) float64 {
	if slot < 0 || slot >= slotCount {
		return DefaultDropChance
	}
	return chancesOf(m).c[slot]
}

// SetDropChance sets the drop chance of slot (SlotDropChanceUtility::
// setForSlot). minecraft:equipment sets the slots it lists when attached.
func SetDropChance(m *mob.Mob, slot int, chance float64) {
	if slot >= 0 && slot < slotCount {
		chancesOf(m).c[slot] = chance
	}
}

func init() {
	mob.RegisterDeathHook(mob.DeathHook{Name: "core/drop_equipment", Die: DropEquipmentOnDeath})
}

// DropEquipmentOnDeath is BDS Mob::dropEquipmentOnDeath(ActorDamageSource),
// run for every mob that dies. For each slot with an item whose drop chance
// is not 0 (or that was picked up): the chance grows by 1% per Looting level
// of the killer's hand items; picked-up items always drop, others drop on
// a chance roll when the killer is a player (or a player's pet) or the
// chance is at least 1. A drop that was not guaranteed is badly damaged
// (durability lost = min(max-r, max-25), r = rand(rand(max-25)+1)). The
// slot is emptied either way, so the corpse no longer shows it (and the
// death experience, evaluated 20 ticks later, no longer counts it).
// Items with Curse of Vanishing are removed first.
func DropEquipmentOnDeath(m *mob.Mob, src world.DamageSource) {
	killer := sourceEntity(src)
	byPlayer := isPlayerOrPet(m, killer)
	looting := loot.HandLooting(killer)
	r := m.Rand()
	var drops []item.Stack
	for slot := range slotCount {
		s := EquipmentSlot(m, slot)
		if s.Empty() {
			continue
		}
		if _, ok := s.Enchantment(enchantment.CurseOfVanishing); ok {
			// Mob::die clears vanishing items before anything drops.
			SetEquipmentSlot(m, slot, item.Stack{})
			continue
		}
		picked := loot.WasPickedUp(s)
		ch := DropChance(m, slot)
		if ch == 0 && !picked {
			continue
		}
		ch += 0.01 * float64(looting)
		guaranteed := picked || ch >= 1
		if picked || ((byPlayer || guaranteed) && float64(r.Float32()) < ch) {
			s = loot.ClearPickedUp(s)
			if !guaranteed {
				s = damageDrop(m, s)
			}
			drops = append(drops, s)
		}
		SetEquipmentSlot(m, slot, item.Stack{})
	}
	DropItems(m, drops, 0)
}

// damageDrop wears s as BDS does for an equipment drop that was not
// guaranteed.
func damageDrop(m *mob.Mob, s item.Stack) item.Stack {
	if _, ok := s.Item().(item.Durable); !ok {
		return s
	}
	maxD := s.MaxDurability()
	if maxD <= 0 {
		return s
	}
	r := m.Rand()
	limit := max(maxD-25, 1)
	r1 := r.IntN(limit)
	r2 := r.IntN(r1 + 1)
	dmg := limit
	if v := maxD - r2; v <= limit {
		dmg = max(v, 1)
	}
	return s.WithDurability(max(maxD-dmg, 1))
}

// sourceEntity is the entity a damage source names
// (ActorDamageSource::getDamagingEntityUniqueID): the attacker, the owner of
// a projectile, or the entity that exploded (a creeper).
func sourceEntity(src world.DamageSource) world.Entity {
	switch s := src.(type) {
	case entity.AttackDamageSource:
		return s.Attacker
	case entity.ProjectileDamageSource:
		if s.Owner != nil {
			return s.Owner
		}
		return s.Projectile
	case entity.ExplosionDamageSource:
		if es, ok := s.Source.(world.EntityExplosionSource); ok {
			return es.Entity
		}
	}
	return nil
}

// isPlayerOrPet reports whether e is a player, or a mob a player owns.
func isPlayerOrPet(m *mob.Mob, e world.Entity) bool {
	if e == nil {
		return false
	}
	if e.H().Type().EncodeEntity() == "minecraft:player" {
		return true
	}
	if o, ok := mob.Of(e); ok {
		if h := o.Owner(); h != nil {
			if oe, ok := h.Entity(m.Tx()); ok {
				return oe.H().Type().EncodeEntity() == "minecraft:player"
			}
			return true
		}
	}
	return false
}

func (c *equipment) EncodeNBT() map[string]any { return map[string]any{"Rolled": boolByte(c.rolled)} }
func (c *equipment) DecodeNBT(*mob.Mob, map[string]any) {
	// A saved mob kept its gear in the core's NBT: never roll again.
	c.rolled = true
}
