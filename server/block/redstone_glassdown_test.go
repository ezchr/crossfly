//go:build crossfly_full

package block

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Java Edition: dust on glass doesn't power dust one block lower beside the glass.
func TestRedstoneWireDoesNotStepDownOffGlass(t *testing.T) {
	for _, support := range []world.Block{Glass{}, Stone{}} {
		w := world.Config{Dim: world.End, Synchronous: true}.New()
		viewer := &redstoneWireTestBlockUpdateViewer{}
		loader := world.NewLoader(2, w, viewer)
		runWorld(w, func(tx *world.Tx) {
			loader.Move(tx, mgl64.Vec3{0, 64, 0})
			loader.Load(tx, 16)
		})
		floor := 63
		for x := -1; x <= 4; x++ {
			redstoneWireTestSetBlockAndWait(t, w, cube.Pos{x, floor, 0}, Stone{})
		}
		redstoneWireTestSetBlockAndWait(t, w, cube.Pos{1, 64, 0}, support)
		redstoneWireTestSetBlockAndWait(t, w, cube.Pos{1, 65, 0}, RedstoneWire{})
		redstoneWireTestSetBlockAndWait(t, w, cube.Pos{0, 65, 0}, RedstoneBlock{})
		redstoneWireTestSetBlockAndWait(t, w, cube.Pos{2, 64, 0}, RedstoneWire{}) // placed with the power already on
		time.Sleep(50 * time.Millisecond)
		var upper, lower world.Block
		runWorld(w, func(tx *world.Tx) {
			upper, lower = tx.Block(cube.Pos{1, 65, 0}), tx.Block(cube.Pos{2, 64, 0})
		})
		t.Logf("%T: upper %+v lower %+v", support, upper, lower)
		want := 0
		if _, stone := support.(Stone); stone {
			want = 14
		}
		if got := lower.(RedstoneWire).Power; got != want {
			t.Errorf("%T: lower dust power %d, want %d", support, got, want)
		}
		runWorld(w, func(tx *world.Tx) { loader.Close(tx) })
		_ = w.Close()
	}
}
