//go:build crossfly_full

package item

import (
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
)

// OminousBottle is the ominous bottle of Java Edition 26.3 (Items.OMINOUS_BOTTLE with OminousBottleAmplifier): drinking
// it gives bad omen of its amplifier plus one for 100 minutes, without particles. Pillager captains drop it. It is
// registered only by Crossfly Full's Java mob mode (RegisterOminousBottle).
type OminousBottle struct {
	// Amplifier is the bottle's ominous_bottle_amplifier, 0 to 4: the bad omen is level Amplifier + 1.
	Amplifier int
}

// MaxCount ...
func (OminousBottle) MaxCount() int { return 64 }

// AlwaysConsumable ...
func (OminousBottle) AlwaysConsumable() bool { return true }

// ConsumeDuration is the default consumable time of 1.6 seconds.
func (OminousBottle) ConsumeDuration() time.Duration { return DefaultConsumeDuration }

// Consume is OminousBottleAmplifier.onConsume: bad omen for 120000 ticks. The bottle leaves nothing behind.
func (o OminousBottle) Consume(_ *world.Tx, c Consumer) Stack {
	if t, ok := effect.ByID(badOmenID); ok {
		if lt, ok := t.(effect.LastingType); ok {
			c.AddEffect(effect.New(lt, min(max(o.Amplifier, 0), 4)+1, 120000*time.Second/20).WithoutParticles())
		}
	}
	return Stack{}
}

// EncodeItem ...
func (o OminousBottle) EncodeItem() (name string, meta int16) {
	return "minecraft:ominous_bottle", int16(min(max(o.Amplifier, 0), 4))
}

// badOmenID is the Bedrock effect id of bad omen.
const badOmenID = 28

var registerOminousBottle sync.Once

// RegisterOminousBottle registers the ominous bottles (amplifier 0 to 4). Crossfly Full's Java mob mode calls it
// before the server is configured; the Bedrock mob engine has no ominous bottle.
func RegisterOminousBottle() {
	registerOminousBottle.Do(func() {
		for a := 0; a <= 4; a++ {
			world.RegisterItem(OminousBottle{Amplifier: a})
		}
	})
}
