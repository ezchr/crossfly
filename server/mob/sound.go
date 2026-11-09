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
}

// Play ...
func (Sound) Play(*world.World, mgl64.Vec3) {}

// LevelSoundEvent returns the event name, the entity type and whether the
// mob is a baby.
func (s Sound) LevelSoundEvent() (event, entityType string, baby bool) {
	return s.Event, s.Identifier, s.Baby
}

// PlaySound plays a sound event of the mob at its position.
func (m *Mob) PlaySound(event string) {
	if event == "" || m.Tx() == nil {
		return
	}
	m.Tx().PlaySound(m.Position(), Sound{Event: event, Identifier: m.Identifier(), Baby: m.s.meta.Baby})
}
