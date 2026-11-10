package javasession

import (
	"bytes"
	"testing"

	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/java/protocol/wire"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
)

// TestVibrationParticle: a vibration signal to a block listener is Java's vibration particle with a
// BlockPositionSource (type 0, the block) and the arrival time, sent with the limiter overridden; a
// shriek is level event 3007 at the shrieker.
func TestVibrationParticle(t *testing.T) {
	s := fxTestSession()
	s.ViewParticle(mgl64.Vec3{1.5, 70, 2.5}, particle.Vibration{Pos: mgl64.Vec3{5.5, 64.5, -3.5}, Ticks: 7})
	out := s.take()
	if len(out) != 1 || out[0].id != v777.ClientboundPlayLevelParticles {
		t.Fatalf("%d packets", len(out))
	}
	want := &wire.Writer{}
	want.VarInt(particleByName["vibration"])
	want.VarInt(positionSourceBlock)
	want.Position(5, 64, -4)
	want.VarInt(7)
	want.Bool(true)
	if !bytes.HasPrefix(out[0].w.B, want.B) {
		t.Errorf("vibration particle %x, want it to start with %x", out[0].w.B, want.B)
	}

	s.ViewParticle(mgl64.Vec3{-3, 20, 7}, particle.Shriek{})
	out = s.take()
	if len(out) != 1 || out[0].id != v777.ClientboundPlayLevelEvent || checkLevelEvent(t, out[0].w.B) != levelEventSculkShriek {
		t.Errorf("shriek: %d packets", len(out))
	}
}

// sculkLevelSound is a Bedrock level sound event, as the sculk blocks play them.
type sculkLevelSound struct{ event, entity string }

func (sculkLevelSound) Play(*world.World, mgl64.Vec3) {}
func (s sculkLevelSound) LevelSoundEvent() (string, string, bool) {
	return s.event, s.entity, false
}

// TestSculkSounds: the sculk sensor's clicks, the shriek and the warden's warnings reach Java as its
// sounds of the same things.
func TestSculkSounds(t *testing.T) {
	for _, c := range []struct {
		event, entity, java string
	}{
		{"power.on.sculk_sensor", "", "minecraft:block.sculk_sensor.clicking"},
		{"power.off.sculk_sensor", "", "minecraft:block.sculk_sensor.clicking_stop"},
		{"block.sculk_shrieker.shriek", "", "minecraft:block.sculk_shrieker.shriek"},
		{"nearby_closer", "minecraft:warden", "minecraft:entity.warden.nearby_closer"},
		{"agitated", "minecraft:warden", "minecraft:entity.warden.agitated"},
	} {
		got, ok := javaMobSoundName(c.event, c.entity)
		if !ok || got != c.java {
			t.Errorf("%v %v: %q %v, want %q", c.event, c.entity, got, ok, c.java)
		}
		s := fxTestSession()
		s.ViewSound(mgl64.Vec3{1, 2, 3}, sculkLevelSound{event: c.event, entity: c.entity})
		if out := s.take(); len(out) != 1 {
			t.Errorf("%v: %d packets", c.event, len(out))
		}
	}
}
