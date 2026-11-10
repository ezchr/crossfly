package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// genericData decodes the fields of a LevelEventGeneric, which carries a compound without its name and
// end tag.
func genericData(t *testing.T, pk *packet.LevelEventGeneric) map[string]any {
	t.Helper()
	raw := append(append([]byte{10, 0}, pk.SerialisedEventData...), 0)
	var m map[string]any
	if err := nbt.UnmarshalEncoding(raw, &m, nbt.NetworkLittleEndian); err != nil {
		t.Fatalf("event data %x: %v", pk.SerialisedEventData, err)
	}
	return m
}

// TestVibrationParticle: a vibration signal is LevelEventGeneric 2027 from the vibration to the listener
// (a block centre, or an actor's head), at speed 20, living as many twentieths of a second as it takes
// ticks; a shriek is 2035 at the shrieker's block.
func TestVibrationParticle(t *testing.T) {
	s := newTestSession()
	s.ViewParticle(mgl64.Vec3{1.5, 64.5, 2.5}, particle.Vibration{Pos: mgl64.Vec3{5.5, 64.5, -3.5}, Ticks: 6})
	pk := only[*packet.LevelEventGeneric](t, s)
	if pk.EventID != packet.LevelEventParticlesVibrationSignal {
		t.Fatalf("event %d, want 2027", pk.EventID)
	}
	d := genericData(t, pk)
	origin, _ := d["origin"].(map[string]any)
	target, _ := d["target"].(map[string]any)
	if origin["type"] != "vec3" || origin["x"] != float32(1.5) || origin["z"] != float32(2.5) {
		t.Errorf("origin %v", origin)
	}
	if target["type"] != "vec3" || target["x"] != float32(5.5) || target["z"] != float32(-3.5) {
		t.Errorf("target %v", target)
	}
	if d["speed"] != float32(20) || d["timeToLive"] != float32(6)*0.05 {
		t.Errorf("speed %v timeToLive %v, want 20 and 0.3", d["speed"], d["timeToLive"])
	}

	s.ViewParticle(mgl64.Vec3{1.5, 64.5, 2.5}, particle.Vibration{Entity: newFakeMob(t).H(), Ticks: 3})
	target, _ = genericData(t, only[*packet.LevelEventGeneric](t, s))["target"].(map[string]any)
	if target["type"] != "actor" || target["attachPos"] != int32(3) {
		t.Errorf("actor target %v", target)
	}

	s.ViewParticle(mgl64.Vec3{-3, 20, 7}, particle.Shriek{})
	pk = only[*packet.LevelEventGeneric](t, s)
	d = genericData(t, pk)
	if pk.EventID != packet.LevelEventParticleSculkShriek || d["originX"] != int32(-3) || d["originY"] != int32(20) || d["originZ"] != int32(7) {
		t.Errorf("shriek event %d %v", pk.EventID, d)
	}
}
