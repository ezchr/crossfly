//go:build !crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Crossfly Core has no Java redstone: world.JavaRedstone is always false, so the calls below are never made and the
// compiler leaves them out. They exist so that the shared code compiles; Crossfly Full has the real ones
// (*_java.go).

func (t RedstoneTorch) javaUseOnBlock(cube.Pos, cube.Face, mgl64.Vec3, *world.Tx, item.User, *item.UseContext) bool {
	return false
}
func (RedstoneTorch) javaNeighbourUpdateTick(cube.Pos, *world.Tx)   {}
func (RedstoneTorch) javaScheduledTick(cube.Pos, *world.Tx)         {}
func (RedstoneTorch) check(cube.Pos, *world.Tx)                     {}
func (Chest) updateTrappedPower(cube.Pos, *world.Tx)                {}
func (Chest) notifyTrappedBelow(cube.Pos, *world.Tx)                {}
func javaWireConnection(world.Block, cube.Face) (connects, ok bool) { return false, false }
func bedrockWireConnection(world.Block, cube.Face) (connects, ok bool) {
	return false, false
}
func tripwireHookFacing(world.Block, cube.Face) bool                                { return false }
func (Hopper) javaTick(int64, cube.Pos, *world.Tx)                                  {}
func (WoodDoor) javaRedstoneUpdate(cube.Pos, *world.Tx, world.RedstoneUpdate)       {}
func (CopperDoor) javaRedstoneUpdate(cube.Pos, *world.Tx, world.RedstoneUpdate)     {}
func (WoodTrapdoor) javaRedstoneUpdate(cube.Pos, *world.Tx, world.RedstoneUpdate)   {}
func (CopperTrapdoor) javaRedstoneUpdate(cube.Pos, *world.Tx, world.RedstoneUpdate) {}
func (WoodFenceGate) javaRedstoneUpdate(cube.Pos, *world.Tx, world.RedstoneUpdate)  {}
