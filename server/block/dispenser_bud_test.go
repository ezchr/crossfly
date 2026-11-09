//go:build crossfly_full

package block

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Java Edition: power two blocks above a dispenser (quasi-connectivity) is only noticed when a block next to the
// dispenser changes.
func TestDispenserQuasiConnectivityNeedsBlockUpdate(t *testing.T) {
	w := world.Config{Dim: world.End, Synchronous: true}.New()
	defer w.Close()
	viewer := &redstoneWireTestBlockUpdateViewer{}
	loader := world.NewLoader(2, w, viewer)
	runWorld(w, func(tx *world.Tx) {
		loader.Move(tx, mgl64.Vec3{0, 64, 0})
		loader.Load(tx, 16)
	})
	defer runWorld(w, func(tx *world.Tx) { loader.Close(tx) })

	at := cube.Pos{0, 64, 0}
	triggered := func() bool {
		var d Dispenser
		runWorld(w, func(tx *world.Tx) { d, _ = tx.Block(at).(Dispenser) })
		return d.Triggered
	}
	step := func(pos cube.Pos, b world.Block, want bool, what string) {
		t.Helper()
		redstoneWireTestSetBlockAndWait(t, w, pos, b)
		time.Sleep(60 * time.Millisecond)
		if got := triggered(); got != want {
			t.Fatalf("%s: triggered %t, want %t", what, got, want)
		}
	}
	d := NewDispenser()
	d.Facing = cube.FaceNorth
	step(at, d, false, "placed")
	step(at.Add(cube.Pos{0, 2, 0}), RedstoneBlock{}, false, "power two above")
	step(at.Add(cube.Pos{1, 0, 0}), Stone{}, true, "a block placed beside")
	step(at.Add(cube.Pos{0, 2, 0}), Air{}, true, "power two above removed")
	step(at.Add(cube.Pos{1, 0, 0}), Air{}, false, "the block beside removed")
}
