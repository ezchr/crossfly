//go:build crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// JavaRedstoneExtension: a playing jukebox powers redstone only with Java redstone.
func (Jukebox) JavaRedstoneExtension() {}

// Playing reports whether the jukebox plays its disc: a disc went in and its song is not over yet.
func (j Jukebox) Playing() bool {
	_, ok := j.Disc()
	return ok && j.handle != 0
}

// RedstonePower gives a signal of 15 to every side while a disc plays (JukeboxBlock::_installCircuit makes
// it a producer; the strength while playing is Java's JukeboxBlock.ownSignal, as BDS sets it in code the
// dump leaves out).
func (j Jukebox) RedstonePower(cube.Pos, *world.Tx, cube.Face) int {
	if j.Playing() {
		return 15
	}
	return 0
}
