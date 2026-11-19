package player

import (
	"math"
	"sync/atomic"
)

// lungeExhaustion is the exhaustion a Lunge jab costs per level, as float bits; negative: Bedrock's
// (4 per level).
var lungeExhaustion atomic.Uint64

func init() { lungeExhaustion.Store(math.Float64bits(-1)) }

// SetLungeExhaustion sets the exhaustion a jab with Lunge costs per level of the enchantment, for
// every player: 0 makes Lunge free, a negative value brings back Bedrock's 4 per level. It may be
// called at any time.
func SetLungeExhaustion(perLevel float64) { lungeExhaustion.Store(math.Float64bits(perLevel)) }

// noLungeWithElytra, when set, keeps Lunge from propelling a player who wears an elytra (a server
// rule, not Bedrock's: a jab before or between glides carries its speed into the glide). The jab
// itself still hits, and charge attacks still use the player's own speed.
var noLungeWithElytra atomic.Bool

// SetNoLungeWithElytra sets whether Lunge does nothing for a player wearing an elytra, for every
// player. Off by default (Bedrock). It may be called at any time.
func SetNoLungeWithElytra(on bool) { noLungeWithElytra.Store(on) }
