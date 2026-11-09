//go:build crossfly_full

package block

import "github.com/df-mc/dragonfly/server/world"

// RedstoneShape (world.RedstoneShaper) of the redstone components whose power changes all the time: their states
// differ in power only (with the same model, conduction and connections) unless the keys returned differ. With Java
// redstone the world keeps what it worked out about the blocks around them when only their power changes.

// RedstoneShape ...
func (r RedstoneWire) RedstoneShape() uint64 { return uint64(boolByte(r.Dot)) }

// RedstoneShape ...
func (RedstoneLamp) RedstoneShape() uint64 { return 0 }

// RedstoneShape ...
func (t RedstoneTorch) RedstoneShape() uint64 { return uint64(t.Facing) }

// RedstoneShape ...
func (r RedstoneRepeater) RedstoneShape() uint64 { return uint64(r.Facing) }

// RedstoneShape ...
func (c RedstoneComparator) RedstoneShape() uint64 { return uint64(c.Facing) }

// RedstoneShape ...
func (o Observer) RedstoneShape() uint64 { return uint64(o.Facing) }

// RedstoneShape ...
func (l Lever) RedstoneShape() uint64 { return uint64(l.Facing) | uint64(l.Direction)<<8 }

// RedstoneShape of a moving block: what it moves, and whether it is the source or has arrived (from then on it has
// the faces of the block it moves, movingPistonModel). Its progress changes every tick.
func (m MovingPiston) RedstoneShape() uint64 {
	key := uint64(boolByte(m.Source)) | uint64(boolByte(m.Progress >= 1))<<1
	if m.Moving != nil {
		key |= (uint64(world.BlockRuntimeID(m.Moving)) + 1) << 2
	}
	return key
}

// RedstoneShape of a piston: which way it faces, whether it is sticky and its model (extended, retracting). Its
// progress and the blocks it moves change every tick while it moves.
func (p Piston) RedstoneShape() uint64 {
	return uint64(p.Facing) | uint64(boolByte(p.Sticky))<<3 | uint64(boolByte(p.Extended))<<4 | uint64(boolByte(p.State == 3))<<5
}

// RedstoneShape of a hopper: its facing. Its cooldowns, items and lock change all the time.
func (h Hopper) RedstoneShape() uint64 { return uint64(h.Facing) }

// RedstoneShape of a dispenser or dropper: its facing and kind. Whether it is triggered changes with its power.
func (d Dispenser) RedstoneShape() uint64 { return uint64(d.Facing) | uint64(boolByte(d.Dropper))<<3 }
