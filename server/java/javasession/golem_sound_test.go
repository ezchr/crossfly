package javasession

import (
	"testing"

	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
)

// The iron golem crack sound (server/mob, played by its Bedrock name) is the Java iron golem damage sound.
func TestIronGolemCrackSoundResolves(t *testing.T) {
	want := v777.BuiltinID("minecraft:sound_event", "minecraft:entity.iron_golem.damage")
	if id, ok := soundByName["mob.irongolem.crack"]; !ok || want < 0 || id != want {
		t.Fatalf("mob.irongolem.crack resolves to %d (%v), want %d", id, ok, want)
	}
}
