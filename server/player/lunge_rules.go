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
