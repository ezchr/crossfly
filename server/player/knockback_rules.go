package player

import "sync/atomic"

// KnockBackRules changes how knockback adds to a player's motion. The zero value keeps Dragonfly's
// behaviour: a knockback replaces the player's velocity.
type KnockBackRules struct {
	// Momentum is the share of the player's velocity a knockback keeps, the knockback being added on
	// top. Vanilla Bedrock keeps half (Mob::knockback halves the velocity, then adds the impulse).
	Momentum float64
	// HeightCap, when over 0, is the most upward velocity a knockback leaves (Mob::knockback caps it).
	HeightCap float64
}

var knockBackRules atomic.Pointer[KnockBackRules]

// SetKnockBackRules sets the KnockBackRules for every player. It may be called at any time.
func SetKnockBackRules(r KnockBackRules) { knockBackRules.Store(&r) }
