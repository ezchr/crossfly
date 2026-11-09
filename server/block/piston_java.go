//go:build crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func (p Piston) BlockStateKey() uint64 { return uint64(p.JavaState()) }
func (p Piston) JavaState() int32 {
	if p.State == 3 {
		return pistonMovingJava[boolByte(p.Sticky)][p.Facing]
	}
	return pistonBaseJava[boolByte(p.Sticky)][p.Facing][boolByte(p.Extended)]
}
func (p Piston) JavaBlockEntity() (string, []byte) {
	return "", nil
}
func (p Piston) JavaMovingBlock() (world.Block, cube.Face, bool, bool, float64) {
	if p.State != 3 {
		return nil, p.Facing, false, true, 0
	}
	base := p
	base.State = 0
	base.Extended = false
	return base, p.Facing, false, true, 1 - p.LastProgress
}
func (h PistonHead) JavaState() int32                { return pistonHeadJava[boolByte(h.Sticky)][h.Facing] }
func (PistonHead) JavaBlockEntity() (string, []byte) { return "", nil }
func (m MovingPiston) JavaState() int32 {
	sticky := false
	if head, ok := m.Moving.(PistonHead); ok {
		sticky = head.Sticky
	}
	return pistonMovingJava[boolByte(sticky)][m.Facing]
}
func (MovingPiston) JavaBlockEntity() (string, []byte) { return "", nil }
func (m MovingPiston) JavaMovingBlock() (world.Block, cube.Face, bool, bool, float64) {
	return m.Moving, m.Facing, m.Extending, m.Source, m.LastProgress
}
