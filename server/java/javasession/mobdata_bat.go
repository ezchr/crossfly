package javasession

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// This init runs after the one in mobdata.go (files initialise in name
// order) and replaces the plain bat entry.
func init() {
	mobKinds["minecraft:bat"] = &mobKind{extra: batData}
}

// batData is Bat.DATA_ID_FLAGS (16): 0x01 while the bat hangs from the
// ceiling, from the Bedrock RESTING actor flag (server/mob/goal/misc/animal
// bat.go, Bat::setResting).
func batData(_ *Session, d *mobData, e world.Entity, _ string) {
	var f byte
	if mobFlag(e, protocol.EntityDataFlagResting) {
		f = 0x01
	}
	d.byteVal(16, f)
}
