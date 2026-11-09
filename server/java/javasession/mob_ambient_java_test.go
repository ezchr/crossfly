package javasession

import "testing"

// TestHardcodedAmbientJavaNames: the ambient sounds Crossfly sends only to
// Java (mobs without ambient_sound_interval) map to Java sound events.
func TestHardcodedAmbientJavaNames(t *testing.T) {
	for _, c := range []struct{ event, typ, want string }{
		{"ambient", "minecraft:cow", "minecraft:entity.cow.ambient"},
		{"ambient.baby", "minecraft:cow", "minecraft:entity.cow.ambient"},
		{"ambient", "minecraft:mooshroom", "minecraft:entity.cow.ambient"},
		{"ambient.baby", "minecraft:mooshroom", "minecraft:entity.cow.ambient"},
		{"ambient", "minecraft:sheep", "minecraft:entity.sheep.ambient"},
		{"ambient.baby", "minecraft:sheep", "minecraft:entity.sheep.ambient"},
		{"ambient", "minecraft:goat", "minecraft:entity.goat.ambient"},
		{"ambient.screamer", "minecraft:goat", "minecraft:entity.goat.screaming.ambient"},
		{"ambient", "minecraft:rabbit", "minecraft:entity.rabbit.ambient"},
		{"ambient", "minecraft:parrot", "minecraft:entity.parrot.ambient"},
	} {
		got, ok := javaMobSoundName(c.event, c.typ)
		if !ok || got != c.want || !validSoundName(got) {
			t.Errorf("%v of %v: %q (%v), want %q", c.event, c.typ, got, ok, c.want)
		}
	}
}
