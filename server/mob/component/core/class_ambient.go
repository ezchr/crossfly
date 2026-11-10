//go:build crossfly_full

package core

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/mob"
)

// ClassAmbientEvent is the ambient sound a mob class picks over the one its
// definition names (the getAmbientSound overrides of the BDS classes). The
// Bedrock client runs the same override itself, so this only changes what
// Java viewers hear (playClientAmbient). Its rolls use the shared generator,
// like the ambient timers: they are cosmetic.
func ClassAmbientEvent(m *mob.Mob, event string) string {
	switch m.Identifier() {
	case "minecraft:wolf":
		// Wolf::getAmbientSound: angry wolves growl; otherwise 2 in 3
		// times the usual sound, else tame wolves under 10 health whine and
		// the others pant.
		if m.Angry() {
			return "growl"
		}
		if rand.IntN(3) != 0 {
			return event
		}
		if m.Tamed() && m.Health() < 10 {
			return "whine"
		}
		return "pant"
	case "minecraft:cat":
		// Cat::getAmbientSound: tame cats purr in love, else purreow 1 in 4
		// times and meow (ambient.tame) otherwise; stray cats keep theirs.
		if !m.Tamed() {
			return event
		}
		if m.InLove() {
			return "purr"
		}
		if rand.IntN(4) == 0 {
			return "purreow"
		}
		return "ambient.tame"
	}
	return event
}
