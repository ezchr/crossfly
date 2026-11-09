package javasession

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// dataTypeDirection is EntityDataSerializers.DIRECTION.
const dataTypeDirection = 12

// shulkerData is Shulker's DATA_ATTACH_FACE_ID (16), DATA_PEEK_ID (17) and
// DATA_COLOR_ID (18), from the Bedrock attach face and peek actor data
// (server/mob/component/shulker) and the variant. Bedrock and Java number
// the faces alike: down, up, north, south, west, east.
func shulkerData(_ *Session, d *mobData, e world.Entity, _ string) {
	if v, ok := extraData(e, protocol.EntityDataKeyAttachFace); ok {
		if f, ok := v.(byte); ok && f < 6 {
			d.entry(16, dataTypeDirection)
			d.w.VarInt(int32(f))
		}
	}
	if v, ok := extraData(e, protocol.EntityDataKeyPeekID); ok {
		if n, ok := v.(int32); ok {
			d.byteVal(17, byte(min(max(n, 0), 100)))
		}
	}
	if v, ok := mobAs[mobVariant](e); ok {
		// Bedrock 16 is the default purple shulker (Java 16 too); the dyed ones run backwards.
		c := byte(16)
		if n := v.Variant(); n >= 0 && n < 16 {
			c = byte(15 - n)
		}
		d.byteVal(18, c)
	}
}
