//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestSulfurCubeKickedByPlayer: a player walking into a sulfur cube holding a block kicks it away like a ball
// (pushable_by_entity preset push_mode ball); a standing player does not, and a cube without a block (no preset)
// is only pushed softly.
func TestSulfurCubeKickedByPlayer(t *testing.T) {
	needs(t, true, true, "minecraft:interact", "minecraft:pushable_by_entity")
	kicks := map[bool]float64{}
	for _, withBlock := range []bool{true, false} {
		w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
		w.Do(func(tx *world.Tx) { w.Arena(tx, 12, 2) })
		p := w.AddPlayer("kicker", mgl64.Vec3{-3.5, 1, 0.5})
		cube := w.Spawn("minecraft:sulfur_cube", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_medium"})
		w.Tick(2)
		if withBlock {
			if _, ok := p.Interact(cube, item.NewStack(block.Planks{Wood: block.OakWood()}, 1)); !ok {
				t.Fatalf("giving planks was not handled:\n%v", w.Dump(cube))
			}
		}
		w.Tick(5)
		start := cube.Pos()
		// Stand next to the cube for a few ticks: no kick.
		p.Teleport(mgl64.Vec3{start[0] - 0.9, start[1], start[2]})
		w.Tick(3)
		if d := cube.Pos().Sub(start).Len(); withBlock && d > 0.05 {
			t.Errorf("block %v: a standing player moved the cube %.2f", withBlock, d)
		}
		// Run into it at 0.2 blocks a tick.
		var kick float64
		for i := 0; i < 4; i++ {
			c := cube.Pos()
			p.Teleport(mgl64.Vec3{c[0] - 0.9 + 0.2*float64(i%2), c[1], c[2]})
			w.Tick(1)
			cube.With(func(m *mob.Mob) { kick = max(kick, m.Velocity()[0]) })
		}
		w.Tick(10)
		moved := cube.Pos()[0] - start[0]
		if withBlock && (kick < 0.2 || moved < 0.5) {
			t.Errorf("a cube with a block was not kicked: velocity %.2f, moved %.2f\n%v", kick, moved, w.Dump(cube))
		}
		kicks[withBlock] = kick
	}
	if kicks[false] > kicks[true]/2 {
		t.Errorf("a cube without a block was pushed about as hard (%.2f) as a kicked one (%.2f)", kicks[false], kicks[true])
	}
}
