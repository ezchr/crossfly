//go:build crossfly_full

package mob

// ambientOverrides replace the ambient sound the server plays for Java viewers of a mob whose
// class picks its own (BDS Parrot::playAmbientSound imitating nearby monsters, package
// mob/goal/misc/social).
var ambientOverrides []func(m *Mob, event string) (Sound, bool)

// RegisterAmbientOverride registers f, asked for each ambient sound the server plays for a mob
// in place of the Bedrock client (mob.Sound.ClientSide): when f reports true, its sound is played
// instead of event. Call it from an init function.
func RegisterAmbientOverride(f func(m *Mob, event string) (Sound, bool)) {
	ambientOverrides = append(ambientOverrides, f)
}

// AmbientOverride returns the sound m plays in place of the ambient sound event, if a registered
// override picks one. The code playing client-side ambient sounds for Java viewers asks it first.
func AmbientOverride(m *Mob, event string) (Sound, bool) {
	for _, f := range ambientOverrides {
		if s, ok := f(m, event); ok {
			return s, true
		}
	}
	return Sound{}, false
}
