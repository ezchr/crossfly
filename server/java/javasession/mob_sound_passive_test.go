package javasession

import (
	"testing"

	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
)

// TestMobSoundNamePassive: the sounds of the passive mob features whose Java names differ from
// entity.<type>.<event> map to sounds of the Java registry.
func TestMobSoundNamePassive(t *testing.T) {
	for _, c := range []struct{ event, typ, want string }{
		{"ambient", "minecraft:cat", "minecraft:entity.cat.stray_ambient"},
		{"ambient.tame", "minecraft:cat", "minecraft:entity.cat.ambient"},
		{"armor.equip_generic", "minecraft:happy_ghast", "minecraft:entity.happy_ghast.equip"},
		{"armor.unequip_generic", "minecraft:happy_ghast", "minecraft:entity.happy_ghast.unequip"},
		{"attach", "minecraft:happy_ghast", "minecraft:entity.happy_ghast.harness_goggles_down"},
		{"detach", "minecraft:happy_ghast", "minecraft:entity.happy_ghast.harness_goggles_up"},
		{"armor.equip_wolf", "minecraft:wolf", "minecraft:item.armor.equip_wolf"},
		{"armor.unequip_wolf", "minecraft:wolf", "minecraft:item.armor.unequip_wolf"},
		{"armor.crack_wolf", "minecraft:wolf", "minecraft:item.wolf_armor.crack"},
		{"armor.break_wolf", "minecraft:wolf", "minecraft:item.wolf_armor.break"},
		{"armor.repair_wolf", "minecraft:wolf", "minecraft:item.wolf_armor.repair"},
		{"mob.armadillo.brush", "minecraft:armadillo", "minecraft:entity.armadillo.brush"},
		{"mob.armadillo.scute_drop", "minecraft:armadillo", "minecraft:entity.armadillo.scute_drop"},
		{"irongolem.repair", "minecraft:iron_golem", "minecraft:entity.iron_golem.repair"},
		{"pushed_by_player", "minecraft:sulfur_cube", "minecraft:entity.sulfur_cube.regular.push"},
		{"absorb_block", "minecraft:sulfur_cube", "minecraft:entity.sulfur_cube.absorb"},
		{"eject_block", "minecraft:sulfur_cube", "minecraft:entity.sulfur_cube.eject"},
	} {
		got, ok := javaMobSoundName(c.event, c.typ)
		if !ok || got != c.want {
			t.Errorf("%v %v -> %q %v, want %q", c.typ, c.event, got, ok, c.want)
			continue
		}
		if v777.BuiltinID("minecraft:sound_event", got) < 0 {
			t.Errorf("%v is not a Java sound", got)
		}
	}
}
