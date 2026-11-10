package javasession

import (
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	jitem "github.com/df-mc/dragonfly/server/java/protocol/item"
	"github.com/df-mc/dragonfly/server/java/protocol/v777"
)

// viewThrownEgg sends the item of a thrown brown or blue egg (ThrowableItemProjectile.DATA_ITEM_STACK), so
// the client draws it in its colour; a white egg is what it draws without. It reports whether e is a thrown
// egg.
func (s *Session) viewThrownEgg(e *entity.Ent, id int32) bool {
	egg, ok := entity.ThrownEggItem(e)
	if !ok {
		return false
	}
	if e, _ := egg.Item().(item.Egg); e.Variant == item.EggTemperate {
		return true
	}
	var js jitem.Stack
	javaStack(egg, &js)
	w := s.packet()
	w.VarInt(id)
	w.Byte(8)                        // ThrowableItemProjectile.DATA_ITEM_STACK
	w.VarInt(7)                      // ITEM_STACK serializer
	js.EncodeFor(w, s.items().proto) // item and component ids of the client version
	w.Byte(0xff)
	s.queue(v777.ClientboundPlaySetEntityData, w)
	return true
}
