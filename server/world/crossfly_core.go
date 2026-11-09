//go:build !crossfly_full

package world

import "github.com/df-mc/dragonfly/server/block/cube"

// fullBuild is true in Crossfly Full builds.
const fullBuild = false

// Crossfly Core: Java Edition redstone is a Crossfly Full feature. javaRedstone is a constant here, so the
// compiler leaves out every code path that depends on it.
const javaRedstone = false

// JavaRedstone reports whether worlds follow Java Edition's redstone rules (Crossfly Full). It is always
// false in Crossfly Core.
func JavaRedstone() bool { return false }

// The Java Edition conductor tables (redstone_conductors_gen.go) are part of Crossfly Full only.
var (
	javaConductorOverride map[string]int8
	javaConductorKnown    map[string]bool
)

// The parts of the world for Java redstone. Never called in Crossfly Core.

type pendingWork struct{}

func (w *World) pending() pendingWork                  { return pendingWork{} }
func (w *World) settle(pendingWork)                    {}
func (w *World) updateNeighboursOfNeighbours(cube.Pos) {}
func (w *World) invalidateCurrentSky(ChunkPos)         {}
func (w *World) updateWeatherIntensity()               {}
func (w *World) runBlockEvents(*Tx, func())            {}
func (t ticker) flushUpdates(*Tx, int64)               {}

// blockEntityTicks lists the block entities ticked in a tick with Java redstone (Crossfly Full).
type blockEntityTicks struct{ entries []cube.Pos }

func (*blockEntityTicks) begin()                                   {}
func (*blockEntityTicks) end()                                     {}
func (*blockEntityTicks) collect(ChunkPos, *Column)                {}
func (*blockEntityTicks) sort(*World)                              {}
func (*blockEntityTicks) analogList() []cube.Pos                   { return nil }
func (*blockEntityTicks) noteWrite(*Column, cube.Pos, Block, bool) {}
func (*blockEntityTicks) run(l []cube.Pos, _ func(cube.Pos)) []cube.Pos {
	return l
}
