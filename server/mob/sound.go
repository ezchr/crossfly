//go:build crossfly_full

package mob

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Level sound event names (Bedrock LevelSoundEvent, as entity JSON writes
// them: ambient_sound_interval event_name, interact play_sounds, play_sound
// responses). Any name the client knows may be used.
const (
	SoundAmbient = "ambient"
	SoundHurt    = "hurt"
	SoundDeath   = "death"
)

// Sound is a mob sound event: the Bedrock session sends it as a
// LevelSoundEvent with the entity type (the client picks the sound from the
// type, e.g. mob.cow.say for ambient), the Java session as the matching
// minecraft:entity.<type>.<event> sound. Neither imports this package: they
// read LevelSoundEvent.
type Sound struct {
	// Event is the level sound event name ("ambient", "hurt", "plop", ...).
	Event string
	// Identifier is the entity identifier ("minecraft:chicken").
	Identifier string
	// Baby makes the client pick the baby variant/pitch.
	Baby bool
	// ClientSide marks a sound the Bedrock client plays by itself, so the
	// Bedrock session drops it and only Java hears it: ambient sounds of
	// mobs without ambient_sound_interval (BDS sends no LevelSoundEvent,
	// the client plays them from actor data 108-110).
	ClientSide bool
}

// Play ...
func (Sound) Play(*world.World, mgl64.Vec3) {}

// LevelSoundEvent returns the event name, the entity type and whether the
// mob is a baby.
func (s Sound) LevelSoundEvent() (event, entityType string, baby bool) {
	return s.Event, s.Identifier, s.Baby
}

// BedrockClientSide reports whether the Bedrock client plays the sound by
// itself (see ClientSide).
func (s Sound) BedrockClientSide() bool { return s.ClientSide }

// PlaySound plays a sound event of the mob at its position.
func (m *Mob) PlaySound(event string) {
	if event == "" || m.Tx() == nil {
		return
	}
	m.Tx().PlaySound(m.Position(), Sound{Event: event, Identifier: m.Identifier(), Baby: m.s.meta.Baby})
}
