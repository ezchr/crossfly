//go:build crossfly_full

package javamob

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
)

// ConvertTo is Mob.convertTo with ConversionParams.single: the mob is replaced by a new mob of the type id at its
// place, with its rotation, name, baby state, persistence, NoAI and (keepEquipment) equipment and drop chances. A
// Java type is used if there is one, the Bedrock-engine type otherwise (a mob not ported yet). after runs on the
// new Java mob before it is added (nil for a Bedrock-engine mob). It returns the new entity.
func (m *Mob) ConvertTo(id string, keepEquipment bool, after func(n *Mob)) (world.Entity, bool) {
	if m.gone() {
		return nil, false
	}
	tx := m.Tx()
	pos, rot := m.Position(), m.Rotation()
	name := m.NameTag()
	baby := m.Baby()
	var equip [slotCount]item.Stack
	chances := m.s.dropChances
	if keepEquipment {
		for s := Slot(0); s < slotCount; s++ {
			equip[s] = m.ItemBySlot(s)
		}
	}
	persistent := m.PersistenceRequired()
	noAI := m.NoAI()
	if _, java := TypeOf(id); java && installed {
		n, err := Spawn(tx, id, pos, SpawnConversion, SpawnOptions{Rotation: rot, NameTag: name, NoFinalize: true, Configure: func(n *Mob) {
			n.SetBaby(baby)
			if keepEquipment {
				for s := Slot(0); s < slotCount; s++ {
					n.SetItemSlot(s, equip[s])
				}
				n.s.dropChances = chances
			}
			if persistent {
				n.SetPersistenceRequired()
			}
			n.SetNoAI(noAI)
			n.s.canPickUpLoot = m.s.canPickUpLoot
			if after != nil {
				after(n)
			}
		}})
		if err != nil {
			return nil, false
		}
		_ = m.Close()
		return n, true
	}
	bm, err := mob.Spawn(tx, id, pos, mob.SpawnOptions{Rotation: rot, NameTag: name, Event: mob.EventTransformed})
	if err != nil {
		return nil, false
	}
	if keepEquipment {
		main, off := equip[SlotMainHand], equip[SlotOffHand]
		bm.SetHeldItems(main, off)
		a := bm.Armour()
		a.SetHelmet(equip[SlotHead])
		a.SetChestplate(equip[SlotChest])
		a.SetLeggings(equip[SlotLegs])
		a.SetBoots(equip[SlotFeet])
	}
	if persistent {
		bm.SetPersistent(true)
	}
	_ = m.Close()
	return bm, true
}
