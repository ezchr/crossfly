package javasession

import (
	"testing"

	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
)

// TestDanceSoundNames: every parrot imitation server/mob/goal/misc/social plays (its imitateList,
// the BDS and 26.60 imitate.* sounds) and the allay's duplication chime map to a sound of the
// Java registry, or to nothing for the mobs Java has no imitation of.
func TestDanceSoundNames(t *testing.T) {
	imitations := []string{
		"blaze", "bogged", "breeze", "cave_spider", "creeper", "elder_guardian", "ender_dragon", "enderman",
		"endermite", "evocation_illager", "ghast", "husk", "magma_cube", "zombie_pigman", "polar_bear",
		"shulker", "silverfish", "skeleton", "slime", "spider", "stray", "vex", "vindication_illager", "witch",
		"wither", "wither_skeleton", "wolf", "zombie", "zombie_villager", "warden", "creaking", "drowned",
		"guardian", "happy_ghast", "parched", "phantom", "pillager", "ravager", "zoglin", "camel_husk",
	}
	silent := map[string]bool{"zombie_pigman": true, "enderman": true, "polar_bear": true, "wolf": true, "happy_ghast": true}
	for _, m := range imitations {
		name, handled := javaDanceSoundName("imitate."+m, "minecraft:parrot")
		if !handled {
			t.Errorf("imitate.%v not handled", m)
			continue
		}
		if silent[m] {
			if name != "" {
				t.Errorf("imitate.%v -> %q, want no Java sound", m, name)
			}
			continue
		}
		if v777.BuiltinID("minecraft:sound_event", name) < 0 {
			t.Errorf("imitate.%v -> %q, not a Java sound", m, name)
		}
	}
	if name, ok := javaDanceSoundName("chime.amethyst_block", "minecraft:allay"); !ok || v777.BuiltinID("minecraft:sound_event", name) < 0 {
		t.Errorf("chime.amethyst_block -> %q (%v), want a Java sound", name, ok)
	}
	if _, ok := javaDanceSoundName("ambient", "minecraft:parrot"); ok {
		t.Error("plain parrot ambient handled")
	}
}

// TestMobSoundNameDance: javaMobSoundName, which every mob sound goes through, gives the parrot
// imitations and the allay chime their Java names, and nothing for silent imitations.
func TestMobSoundNameDance(t *testing.T) {
	if n, ok := javaMobSoundName("imitate.zombie", "minecraft:parrot"); !ok || n != "minecraft:entity.parrot.imitate.zombie" {
		t.Errorf("imitate.zombie -> %q, %v", n, ok)
	}
	if n, ok := javaMobSoundName("imitate.wolf", "minecraft:parrot"); ok {
		t.Errorf("imitate.wolf -> %q, want no sound", n)
	}
	if n, ok := javaMobSoundName("chime.amethyst_block", "minecraft:allay"); !ok || n != "minecraft:block.amethyst_block.chime" {
		t.Errorf("chime.amethyst_block -> %q, %v", n, ok)
	}
}
