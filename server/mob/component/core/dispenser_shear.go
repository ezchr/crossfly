//go:build crossfly_full

package core

import "github.com/df-mc/dragonfly/server/mob"

func init() { mob.SetDispenserShear(shearByDispenser) }

// dispenserShearing is what a dispenser shearing a mob does to it (26.60 ShearsItem::_tryShearActor): the event of
// the mob's own shear interaction and the loot table it drops. The mooshroom's table depends on its variant.
var dispenserShearing = map[string]struct{ event, table string }{
	"minecraft:sheep":       {"minecraft:on_sheared", "loot_tables/entities/sheep_shear.json"},
	"minecraft:snow_golem":  {"minecraft:on_sheared", "loot_tables/entities/snow_golem_shear.json"},
	"minecraft:bogged":      {"be_sheared", "loot_tables/entities/bogged_shear.json"},
	"minecraft:mooshroom":   {"become_cow", "loot_tables/entities/mooshroom_shear.json"},
	"minecraft:sulfur_cube": {"minecraft:on_sheared", ""},
}

// shearByDispenser shears m for a dispenser (ShearsItem::_tryShearActor): only the mobs BDS lists, never a baby or a
// mob already sheared. It runs what the mob's own shear interaction runs without a player: the shear sound, the
// event and the shear loot.
func shearByDispenser(m *mob.Mob) bool {
	s, ok := dispenserShearing[m.Identifier()]
	if !ok || m.Sheared() || m.Baby() {
		return false
	}
	table := s.table
	if m.Identifier() == "minecraft:mooshroom" {
		if m.HasComponent("minecraft:transformation") {
			// Already turning into a cow (the interaction refuses it too).
			return false
		}
		if m.Variant() != 0 {
			table = "loot_tables/entities/brown_mooshroom_shear.json"
		}
	}
	// BDS plays the shear sound at the dispenser's target block, with no entity type; the mob's own "shear" event
	// (the interaction's play_sounds) is the same sound and reaches Java viewers too.
	m.PlaySound("shear")
	m.Fire(s.event, nil)
	if table != "" {
		if t, err := LoadLootTable(table); err == nil {
			DropItems(m, t.Roll(&LootContext{Mob: m}), m.BBox().Height()*0.5)
		}
	}
	return true
}
