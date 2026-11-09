package javasession

import (
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

// Java clients before 26.3 have no cushions. Behind a proxy, ViaBackwards shows them a falling carpet instead,
// which the client aims at with a falling block's box: 0.98 high where the cushion is a quarter of a block, so
// they hit a cushion they cannot see when aiming just past one. Natively they would not see cushions at all.
// These clients get a stand-in of their own: an interaction entity exactly as big as the cushion, which they
// aim at, attack and sit on like the cushion (it has the cushion's id), and a block display of a carpet of its
// colour that they see.

// Entity data of an interaction (Interaction 8-10) and of a block display's block (BlockDisplay 23).
const (
	dataInteractionWidth    = 8
	dataInteractionHeight   = 9
	dataInteractionResponse = 10
	dataBlockDisplayBlock   = 23
	dataTypeBlockState      = 14
)

// cushionStandIn reports if the client has no cushions: An older protocol, or an older client whose packets a
// proxy translates (its core pack says which version it is).
func (s *Session) cushionStandIn() bool {
	if s.ver.Protocol < 777 {
		return true
	}
	return s.jp != nil && javaVersionBefore(s.jp.Info.CoreVersion, 26, 3)
}

// javaVersionBefore reports if the Java version v ("26.2", "26.2.1", "1.21.11") is older than major.minor. A
// version it cannot read counts as new.
func javaVersionBefore(v string, major, minor int) bool {
	parts := strings.FieldsFunc(v, func(r rune) bool { return r == '.' || r == '-' || r == ' ' })
	if len(parts) < 2 {
		return false
	}
	ma, err1 := strconv.Atoi(parts[0])
	mi, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	return ma < major || (ma == major && mi < minor)
}

// viewCushionStandIn spawns the stand-in of a cushion: an interaction entity of its size with the cushion's id,
// and a carpet of its colour as a block display.
func (s *Session) viewCushionStandIn(c *entity.Cushion) {
	interaction := s.ver.Builtin("minecraft:entity_type", v777.BuiltinID("minecraft:entity_type", "minecraft:interaction"))
	display := s.ver.Builtin("minecraft:entity_type", v777.BuiltinID("minecraft:entity_type", "minecraft:block_display"))
	if interaction < 0 || display < 0 {
		return
	}
	id := s.addEntityID(c)
	pos, yaw := c.Position(), c.Rotation().Yaw()
	w := s.packet()
	w.VarInt(id)
	w.UUID(c.H().UUID())
	w.VarInt(interaction)
	w.Float64(pos[0])
	w.Float64(pos[1])
	w.Float64(pos[2])
	w.LpVec3(0, 0, 0)
	w.Angle(0)
	w.Angle(float32(yaw))
	w.Angle(float32(yaw))
	w.VarInt(0)
	s.queue(v777.ClientboundPlayAddEntity, w)
	w = s.packet()
	w.VarInt(id)
	w.Byte(dataInteractionWidth)
	w.VarInt(dataTypeFloat)
	w.Float32(1)
	w.Byte(dataInteractionHeight)
	w.VarInt(dataTypeFloat)
	w.Float32(0.25)
	w.Byte(dataInteractionResponse) // the arm swings on a right click, as on a cushion
	w.VarInt(dataTypeBoolean)
	w.Bool(true)
	w.Byte(0xff)
	s.queue(v777.ClientboundPlaySetEntityData, w)

	did := s.clientEntityID()
	w = s.packet()
	w.VarInt(did)
	w.UUID(uuid.New())
	w.VarInt(display)
	w.Float64(pos[0])
	w.Float64(pos[1])
	w.Float64(pos[2])
	w.LpVec3(0, 0, 0)
	w.Angle(0)
	w.Angle(0)
	w.Angle(0)
	w.VarInt(0)
	s.queue(v777.ClientboundPlayAddEntity, w)
	state := int32(0)
	if rid := world.BlockRuntimeID(block.Carpet{Colour: c.Colour()}); int(rid) < len(blocks().java) {
		state = s.ver.BlockState(int32(blocks().java[rid]))
	}
	w = s.packet()
	w.VarInt(did)
	w.Byte(dataDisplayTranslation) // a block display draws the block from its corner: centre it
	w.VarInt(dataTypeVector3)
	w.Float32(-0.5)
	w.Float32(0)
	w.Float32(-0.5)
	w.Byte(dataBlockDisplayBlock)
	w.VarInt(dataTypeBlockState)
	w.VarInt(state)
	w.Byte(0xff)
	s.queue(v777.ClientboundPlaySetEntityData, w)
	s.entMu.Lock()
	s.modelParts[id] = []int32{did} // removed with the cushion (hideModel)
	s.entMu.Unlock()

	s.ViewEntityState(c) // its name, if it has one
	s.noteRiding(c)
}
