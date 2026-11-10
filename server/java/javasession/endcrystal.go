package javasession

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/world"
)

// endCrystalView is the state of an End crystal that a Java client draws: the beam (EndCrystal
// DATA_BEAM_TARGET, 8) and the bedrock base (DATA_SHOW_BOTTOM, 9, true by default).
type endCrystalView interface {
	ShowBase() bool
	BeamTarget() (cube.Pos, bool)
}

// viewEndCrystal sends the beam target and base of an End crystal, as the Bedrock session's
// entity metadata does. It reports whether e was an End crystal.
func (s *Session) viewEndCrystal(e world.Entity, id int32) bool {
	ent, ok := e.(*entity.Ent)
	if !ok {
		return false
	}
	b, ok := ent.Behaviour().(endCrystalView)
	if !ok {
		return false
	}
	target, hasTarget := b.BeamTarget()
	w := s.packet()
	w.VarInt(id)
	w.Byte(8)
	w.VarInt(dataTypeOptBlockPos)
	w.Bool(hasTarget)
	if hasTarget {
		w.Position(target[0], target[1], target[2])
	}
	w.Byte(9)
	w.VarInt(dataTypeBoolean)
	w.Bool(b.ShowBase())
	w.Byte(0xff)
	s.queue(v777.ClientboundPlaySetEntityData, w)
	return true
}
