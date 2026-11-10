//go:build crossfly_full

package janimal

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// Event is a Java entity event (EntityEvent id, the byte Level.broadcastEntityEvent sends) as a
// world.EntityAction: viewers get it as the Bedrock actor event of ActorEvent (the Bedrock session sends
// an ActorEvent packet, javasession maps it back to the Java entity event).
type Event byte

// EntityAction ...
func (Event) EntityAction() {}

// JavaEvent is the Java entity event id.
func (e Event) JavaEvent() byte { return byte(e) }

// ActorEvent is the Bedrock actor event shown for the Java event (0 if Bedrock has none).
func (e Event) ActorEvent() uint8 { return bedrockEvents[byte(e)] }

// bedrockEvents maps the Java entity events animals send to Bedrock actor events.
var bedrockEvents = map[byte]uint8{
	EventLoveHearts: packet.ActorEventLoveHearts,
	EventEatGrass:   packet.ActorEventEatGrass,
	6:               packet.ActorEventTamingFailed,
	7:               packet.ActorEventTamingSucceeded,
	8:               packet.ActorEventShakeWetness,
}

var _ world.EntityAction = Event(0)
