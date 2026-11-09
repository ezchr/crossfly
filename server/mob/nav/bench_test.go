//go:build crossfly_full

package nav

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// BenchmarkWorstCase runs a search that uses all 140 expansions (an unreachable goal behind a
// 2 block cliff).
func BenchmarkWorstCase(b *testing.B) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	w.Do(func(tx *world.Tx) {
		floorAt(tx, 16)
		fill(tx, 3, 1, -16, 16, 2, 16, stone)
		f := NewFinder()
		r := zombie(mgl64.Vec3{0.5, 1, 0.5})
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			f.FindPathToBlock(tx, r, cube.Pos{6, 3, 0}, 35)
		}
		b.StopTimer()
		b.ReportMetric(float64(f.Expansions), "expansions")
		b.ReportMetric(float64(f.Lookups), "blocks")
	})
}

// BenchmarkAroundWall is a typical chase: a 13 node path around a wall.
func BenchmarkAroundWall(b *testing.B) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	w.Do(func(tx *world.Tx) {
		floorAt(tx, 16)
		fill(tx, 4, 1, -4, 4, 3, 4, stone)
		f := NewFinder()
		r := zombie(mgl64.Vec3{0.5, 1, 0.5})
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			f.FindPathToBlock(tx, r, cube.Pos{8, 1, 0}, 35)
		}
	})
}
