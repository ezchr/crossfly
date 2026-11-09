//go:build crossfly_full

package entity

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func countTNT(w *world.World) (n int) {
	doWorld(w, func(tx *world.Tx) {
		for e := range tx.Entities() {
			if e.H().Type() == TNTType {
				n++
			}
		}
	})
	return n
}

// Java's PrimedTnt with fuse 80 explodes and is removed on its 80th tick (checked on a vanilla 26.3 server: present
// after 79 ticks, gone after 80).
func TestTNTExplodesOnLastFuseTick(t *testing.T) {
	w := minecartWorld(t)
	doWorld(w, func(tx *world.Tx) {
		tx.AddEntity(NewTNT(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 200, 0.5}}, time.Second*4))
	})
	for i := 1; i <= 80; i++ {
		w.AdvanceTick()
		n := countTNT(w)
		if i < 80 && n != 1 {
			t.Fatalf("after %d ticks: %d primed TNT, want 1", i, n)
		}
		if i == 80 && n != 0 {
			t.Fatalf("after 80 ticks: %d primed TNT, want 0 (exploded and removed)", n)
		}
	}
}

// Java ticks entities after block ticks and block events: TNT primed during a tick (here by an observer's
// scheduled pulse) burns its first fuse tick in that same tick and explodes on the 80th, so it is seen at 79 tick
// boundaries (with entities ticked first it lived a tick longer).
func TestTNTPrimedDuringTickBurnsFuseSameTick(t *testing.T) {
	w := minecartWorld(t)
	tntPos := cube.Pos{0, 200, 0}
	obsPos := tntPos.Side(cube.FaceEast)
	doWorld(w, func(tx *world.Tx) {
		tx.SetBlock(tntPos, block.TNT{}, &world.SetOpts{DisableBlockUpdates: true})
		tx.SetBlock(obsPos, block.Observer{Facing: cube.FaceEast}, &world.SetOpts{DisableBlockUpdates: true})
	})
	w.AdvanceTick()
	doWorld(w, func(tx *world.Tx) {
		tx.SetBlock(obsPos.Side(cube.FaceEast), block.Stone{}, nil)
	})
	present := 0
	for i := 0; i < 100; i++ {
		if countTNT(w) > 0 {
			present++
		}
		w.AdvanceTick()
	}
	if present != 79 {
		t.Fatalf("primed TNT seen at %d tick boundaries, want 79", present)
	}
}
