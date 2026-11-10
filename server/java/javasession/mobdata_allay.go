package javasession

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// The allay's own data runs after mobdata.go's init (files init in name order) set its kind.
func init() {
	if k := mobKinds["minecraft:allay"]; k != nil {
		k.extra = allayData
	}
}

// allayData is Allay.DATA_DANCING (16), from the Bedrock dancing actor flag (server/mob
// goal/misc/social: an allay near a playing jukebox). Allay.DATA_CAN_DUPLICATE (17) keeps its
// default true: the client only uses it to predict the amethyst interaction.
func allayData(_ *Session, d *mobData, e world.Entity, _ string) {
	d.boolean(16, mobActorFlag(e, protocol.EntityDataFlagDancing))
}
