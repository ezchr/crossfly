package effect

import (
	"github.com/df-mc/dragonfly/server/world"
	"image/color"
)

// InstantHealth is an instant effect that causes the player that it is applied
// to immediately regain some health. The amount of health regained depends on
// the effect level and potency.
var InstantHealth instantHealth

type instantHealth struct{}

// Apply instantly heals the world.Entity passed for a bit of health, depending on the effect level and
// potency.
func (i instantHealth) Apply(e world.Entity, eff Effect) {
	base := 2 << eff.Level()
	if l, ok := e.(living); ok {
		if invertedHealAndHarm(e) {
			l.Hurt(float64(base)*eff.potency, InstantDamageSource{})
			return
		}
		l.Heal(float64(base)*eff.potency, InstantHealingSource{})
	}
}

// RGBA ...
func (instantHealth) RGBA() color.RGBA {
	return color.RGBA{R: 0xf8, G: 0x24, B: 0x23, A: 0xff}
}

// invertedHealAndHarm reports whether instant health hurts e and instant
// damage heals it, by the same amounts (BDS Actor::isInvertedHealAndHarm,
// MobEffect::applyInstantaneousEffect swapping the buff type): undead mobs
// implement InvertedHealAndHarm() true.
func invertedHealAndHarm(e world.Entity) bool {
	i, ok := e.(interface{ InvertedHealAndHarm() bool })
	return ok && i.InvertedHealAndHarm()
}

// InstantHealingSource is a healing source used when an entity regains
// health from an effect.instantHealth.
type InstantHealingSource struct{}

func (InstantHealingSource) HealingSource() {}
