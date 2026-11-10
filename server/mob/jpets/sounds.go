//go:build crossfly_full

package jpets

import (
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// Ambient sounds: getAmbientSoundInterval and getAmbientSound of each class,
// as Bedrock level sound event names (both clients map them to their own
// sound). The base plays them once it asks the behaviour (AmbientSounder).

// AmbientSounder is what the base's ambient sound step asks a behaviour.
type AmbientSounder interface {
	AmbientSoundInterval() int
	AmbientSound(m *javamob.Mob) string
}

func (w *Wolf) AmbientSoundInterval() int          { return 80 }
func (w *Wolf) AmbientSound(m *javamob.Mob) string { return w.wolfAmbientEvent(m) }

// AmbientSound is Cat.getAmbientSound: a stray meows; a tame cat purrs in
// love, else purreows one time in four.
func (c *Cat) AmbientSoundInterval() int { return 120 }
func (c *Cat) AmbientSound(m *javamob.Mob) string {
	switch {
	case !c.Tame:
		return "ambient.stray"
	case c.Animal.InLove():
		return "purr"
	case m.Rand().IntN(4) == 0:
		return "purreow"
	}
	return mob.SoundAmbient
}

func (o *Ocelot) AmbientSoundInterval() int           { return 900 }
func (o *Ocelot) AmbientSound(*javamob.Mob) string    { return mob.SoundAmbient }
func (h *Equine) AmbientSoundInterval() int           { return 400 }
func (h *Equine) AmbientSound(*javamob.Mob) string    { return mob.SoundAmbient }
func (b *PolarBear) AmbientSoundInterval() int        { return 80 }
func (b *PolarBear) AmbientSound(*javamob.Mob) string { return mob.SoundAmbient }

// AmbientSound is Panda.getAmbientSound: aggressive and worried pandas
// sound their own way.
func (p *Panda) AmbientSoundInterval() int { return 80 }
func (p *Panda) AmbientSound(*javamob.Mob) string {
	switch p.personality() {
	case GeneAggressive:
		return "ambient.aggressive"
	case GeneWorried:
		return "ambient.worried"
	}
	return mob.SoundAmbient
}

// AmbientSound is Fox.getAmbientSound: snoring asleep, a screech at night
// one time in ten with no player within 16 blocks.
func (f *Fox) AmbientSoundInterval() int { return 80 }
func (f *Fox) AmbientSound(m *javamob.Mob) string {
	if f.flag(foxSleeping) {
		return "sleep"
	}
	if !brightOutside(m.Tx()) && m.Rand().Float32() < 0.1 {
		alone := true
		for _, e := range entitiesTouching(m.Tx(), m.BBox().Grow(16)) {
			if p, ok := e.(*player.Player); ok && p.GameMode() != world.GameModeSpectator {
				alone = false
				break
			}
		}
		if alone {
			return "screech"
		}
	}
	return mob.SoundAmbient
}
