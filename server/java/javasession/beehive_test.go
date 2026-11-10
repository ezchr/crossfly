package javasession

import "testing"

// Beehives and bee nests are listed as Java beehive block entities, and the block.beehive.* sound events play as
// the Java sounds of the same name.
func TestBeehiveJava(t *testing.T) {
	for _, name := range []string{"minecraft:beehive", "minecraft:bee_nest"} {
		if got := javaBlockEntityName(name); got != "minecraft:beehive" {
			t.Errorf("%v -> block entity %q", name, got)
		}
	}
	for _, ev := range []string{"block.beehive.enter", "block.beehive.exit", "block.beehive.work", "block.beehive.shear"} {
		if got, ok := javaMobSoundName(ev, ":"); !ok || got != "minecraft:"+ev {
			t.Errorf("%v -> %q %v", ev, got, ok)
		}
	}
}
