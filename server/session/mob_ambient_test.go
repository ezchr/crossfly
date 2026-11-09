package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// ambientTestSound is a mob sound event (as server/mob.Sound) that the
// Bedrock client may play by itself.
type ambientTestSound struct{ clientSide bool }

func (ambientTestSound) Play(*world.World, mgl64.Vec3) {}
func (ambientTestSound) LevelSoundEvent() (string, string, bool) {
	return "ambient", "minecraft:cow", false
}
func (s ambientTestSound) BedrockClientSide() bool { return s.clientSide }

// TestClientSideMobSoundNotSent: a mob sound the Bedrock client plays by
// itself (ambient sounds of mobs without ambient_sound_interval) sends no
// LevelSoundEvent, so Bedrock players do not hear it twice; other mob
// sounds still do.
func TestClientSideMobSoundNotSent(t *testing.T) {
	s := newTestSession()
	s.ViewSound(mgl64.Vec3{1, 2, 3}, ambientTestSound{clientSide: true})
	if pks := written(s); len(pks) != 0 {
		t.Fatalf("client-side sound sent %v", pks)
	}
	s.ViewSound(mgl64.Vec3{1, 2, 3}, ambientTestSound{})
	pk := only[*packet.LevelSoundEvent](t, s)
	if pk.SoundType != "ambient" || pk.EntityType != "minecraft:cow" {
		t.Fatalf("sound %q of %q", pk.SoundType, pk.EntityType)
	}
}
