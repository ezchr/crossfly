//go:build crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// Redstone dust with Java redstone (Crossfly Full): it updates the neighbours of its neighbours, connects to observers
// and repeaters as Java Edition's shouldConnectTo, and a player toggles a lone dot between a dot and a cross.

// NotifiesNeighboursOfNeighbours makes the redstone engine update the neighbours of each of the wire's neighbours
// when its power changes, as Java Edition's dust does.
func (RedstoneWire) NotifiesNeighboursOfNeighbours() {}

// JavaRedstoneNBT: the dot is kept as server-only block entity data, only with Java redstone.
func (RedstoneWire) JavaRedstoneNBT() {}

// javaWireConnection is Java's shouldConnectTo for an observer (only at its output) and a repeater (only along its
// axis); face is the side of the block that faces the wire. ok is false for other blocks.
func javaWireConnection(b world.Block, face cube.Face) (connects, ok bool) {
	switch v := b.(type) {
	case Observer:
		return v.Facing == face.Opposite(), true
	case RedstoneRepeater:
		return v.Facing.Face().Axis() == face.Axis(), true
	}
	return false, false
}

// bedrockWireConnection reports whether Bedrock Edition's dust connects to b through face, the face of b towards the
// dust, if b is a component that connects only on some faces: a repeater along its axis, an observer at its output.
// Pistons count as dust connections in Bedrock Edition (a PistonConsumer is a power flow direction for dust).
func bedrockWireConnection(b world.Block, face cube.Face) (connects, ok bool) {
	switch v := b.(type) {
	case Observer:
		return v.Facing == face.Opposite(), true
	case RedstoneRepeater:
		return v.Facing.Face().Axis() == face.Axis(), true
	case Piston:
		return true, true
	}
	return false, false
}

// EncodeNBT ...
func (r RedstoneWire) EncodeNBT() map[string]any {
	return map[string]any{"df_dust_dot": boolByte(r.Dot)}
}
func (r RedstoneWire) DecodeNBT(m map[string]any) any { r.Dot = m["df_dust_dot"] == uint8(1); return r }
func (RedstoneWire) EncodeNetworkNBT() map[string]any { return nil }
func (r RedstoneWire) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, user item.User, _ *item.UseContext) bool {
	if !world.JavaRedstone() {
		return false
	}
	if game, ok := user.(interface{ GameMode() world.GameMode }); ok && !game.GameMode().AllowsEditing() {
		return false
	}
	current := redstoneWireConnections(pos, loadedSource{tx: tx})
	if current.Uint8() != 0 && current.Uint8() != 40 {
		return false
	}
	next := r
	next.Dot = current.Uint8() == 40
	links := redstoneWireLinksAround(pos, loadedSource{tx: tx})
	if next.Dot {
		for _, link := range links {
			if link.drawn() {
				return false
			}
		}
	}
	if next.Dot {
		next.Connections = WireConnections{}
	} else {
		next.Connections = WireConnections{north: SideWireConnection(), east: SideWireConnection(), south: SideWireConnection(), west: SideWireConnection()}
	}
	tx.SetBlock(pos, next, nil)
	// Java notifies neighbours of each conductor whose connected dust side changed.
	for _, face := range cube.HorizontalFaces() {
		side := pos.Side(face)
		if world.RedstoneFullPowerConductor(side, tx.Block(side), tx) {
			for _, out := range cube.Faces() {
				if out != face.Opposite() {
					tx.NotifyNeighbour(side.Side(out), side)
				}
			}
		}
	}
	return true
}
