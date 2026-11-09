package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
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

// ambientDataMob carries the ambient actor data server/mob sets on every mob.
type ambientDataMob struct{}

func (ambientDataMob) ExtraActorData() map[uint32]any {
	return map[uint32]any{
		protocol.EntityDataKeyAmbientSoundInterval:      float32(6),
		protocol.EntityDataKeyAmbientSoundIntervalRange: float32(16),
		protocol.EntityDataKeyAmbientSoundEventName:     "ambient.baby",
	}
}

// TestAmbientActorDataSent: actor data 108-110, from which the Bedrock
// client plays ambient sounds, reach the client as float, float and string.
func TestAmbientActorDataSent(t *testing.T) {
	s := newTestSession()
	md := protocol.NewEntityMetadata()
	s.addMobMetadata(ambientDataMob{}, md)
	for key, want := range map[uint32]any{
		protocol.EntityDataKeyAmbientSoundInterval:      float32(6),
		protocol.EntityDataKeyAmbientSoundIntervalRange: float32(16),
		protocol.EntityDataKeyAmbientSoundEventName:     "ambient.baby",
	} {
		if md[key] != want {
			t.Errorf("data %d = %#v, want %#v", key, md[key], want)
		}
	}
}
