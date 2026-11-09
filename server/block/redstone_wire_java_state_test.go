package block

import "testing"

func TestRedstoneWireJavaState(t *testing.T) {
	none, side, up := NoWireConnection(), SideWireConnection(), UpWireConnection()
	// Ids from the Java Edition 26.3 blocks report.
	for _, c := range []struct {
		w    RedstoneWire
		want int32
	}{
		{RedstoneWire{}, 6727},
		{RedstoneWire{Power: 13, Connections: WireConnections{north: none, east: side, south: none, west: side}}, 6411},
		{RedstoneWire{Power: 15, Connections: WireConnections{north: side, east: side, south: side, west: side}}, 6282},
		{RedstoneWire{Power: 7, Connections: WireConnections{north: none, east: up, south: side, west: none}}, 5923},
	} {
		if got := c.w.JavaState(); got != c.want {
			t.Errorf("%+v: Java state %d, want %d", c.w, got, c.want)
		}
	}
}
