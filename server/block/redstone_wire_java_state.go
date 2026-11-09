package block

import "github.com/df-mc/dragonfly/server/world"

// redstoneWireJavaBase is the Java Edition 26.3 state id of redstone_wire[east=up,north=up,power=0,south=up,west=up],
// the first of its 1296 states: they are ordered by east, north, power, south and west, each side up, side, none.
const redstoneWireJavaBase = 5567

// JavaState returns the Java Edition block state of the wire, with its shape and power. Java clients draw wire as the
// server sends it and do not work its connections out themselves: without the shape every wire is a dot to them.
func (r RedstoneWire) JavaState() int32 {
	side := func(c WireConnection) int32 { return 2 - int32(c.Uint8()) } // none 0, side 1, up 2 -> Java up 0, side 1, none 2
	c := r.Connections
	return redstoneWireJavaBase + side(c.east)*432 + side(c.north)*144 + int32(world.ClampRedstonePower(r.Power))*9 +
		side(c.south)*3 + side(c.west)
}

// JavaBlockEntity reports that Java clients draw wire without a block entity.
func (RedstoneWire) JavaBlockEntity() (string, []byte) { return "", nil }
