//go:build crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"math"
	"testing"
)

func TestPistonEntityMovementClipsToWall(t *testing.T) {
	w := world.Config{Dim: world.End, Synchronous: true}.New()
	defer w.Close()
	runWorld(w, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{2, 64, 0}, Stone{}, nil)
		box := cube.Box(1.5, 64, .2, 1.9, 65.8, .8)
		moved := pistonClipMovement(box, mgl64.Vec3{.5, 0, 0}, tx, cube.Pos{0, 64, 0})
		if math.Abs(moved[0]-.1) > 1e-8 {
			t.Fatalf("piston pushed through wall: %v", moved)
		}
	})
}
func TestPistonDisplacementIsLimitedAcrossMovingBlocks(t *testing.T) {
	w := world.Config{Dim: world.End, Synchronous: true, Entities: redstoneBreakDropTestEntityRegistry()}.New()
	defer w.Close()
	runWorld(w, func(tx *world.Tx) {
		e := tx.AddEntity(world.EntitySpawnOpts{}.New(redstoneTNTTestEntityType{}, redstoneTNTTestEntityConfig{}))
		first := tx.LimitPistonDisplacement(e, mgl64.Vec3{.4, 0, 0})
		second := tx.LimitPistonDisplacement(e, mgl64.Vec3{.4, 0, 0})
		if math.Abs(first[0]-.4) > 1e-8 || math.Abs(second[0]-.11) > 1e-8 {
			t.Fatalf("multiple moving blocks exceeded per-tick cap: %v %v", first, second)
		}
	})
}
