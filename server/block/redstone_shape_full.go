//go:build crossfly_full

package block

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
