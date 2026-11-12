package javasession

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/world"
)

// JavaBlock is a custom block that names the Java block state it is shown as (a 26.3 state id) and,
// if Java clients draw it through a block entity, that block entity's type and network NBT (0x0a
// and the nameless compound payload; nil for none).
type JavaBlock interface {
	world.Block
	JavaState() int32
	JavaBlockEntity() (typ string, nbt []byte)
}

// sendBlockEntity sends the block entity of a JavaBlock at pos, for a block that just changed.
func (s *Session) sendBlockEntity(pos cube.Pos, jb JavaBlock) {
	typ, data := javaBlockEntityData(jb)
	s.sendBlockEntityData(pos, typ, data)
}

// sendBlockEntityData sends the block entity of type typ with the network NBT data at pos.
func (s *Session) sendBlockEntityData(pos cube.Pos, typ string, data []byte) {
	if typ == "" || data == nil {
		return
	}
	id := s.ver.BuiltinID("minecraft:block_entity_type", typ)
	if id < 0 {
		return
	}
	w := s.packet()
	w.Position(pos[0], pos[1], pos[2])
	w.VarInt(id)
	w.Raw(data)
	s.queue(v777.ClientboundPlayBlockEntityData, w)
}
