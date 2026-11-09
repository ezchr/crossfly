//go:build crossfly_full

package kit_test

import (
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/kit"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	_ "github.com/df-mc/dragonfly/server/mob/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestFindBlockInSameAsFindBlock: FindBlockIn, which skips blocks by runtime
// ID and whole sections by palette, finds what FindBlock finds testing every
// block, for the nearest and the random pick, sparse and absent blocks.
func TestFindBlockInSameAsFindBlock(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	r := rand.New(rand.NewPCG(5, 5))
	w.Do(func(tx *world.Tx) {
		w.Fill(tx, cube.Pos{-40, -2, -40}, cube.Pos{40, 0, 40}, block.Stone{})
		for range 40 {
			p := cube.Pos{r.IntN(80) - 40, 1 + r.IntN(20), r.IntN(80) - 40}
			tx.SetBlock(p, block.Flower{Type: block.Poppy()}, nil)
		}
		tx.SetBlock(cube.Pos{3, 1, 3}, block.Flower{Type: block.Dandelion()}, nil)
		tx.SetBlock(cube.Pos{-3, 1, 3}, block.Flower{Type: block.Dandelion()}, nil)
	})
	cow := w.Spawn("minecraft:cow", mgl64.Vec3{0.5, 1, 0.5})
	lists := []kit.BlockList{{"minecraft:poppy"}, {"minecraft:dandelion"}, {"minecraft:beehive", "minecraft:bee_nest"}, {"minecraft:stone"}}
	for _, l := range lists {
		for _, method := range []string{"nearest", "random"} {
			for _, rng := range [][2]int{{4, 2}, {16, 10}, {30, 6}} {
				for _, at := range []mgl64.Vec3{{0.5, 1, 0.5}, {17.5, 5, -9.5}, {-31.5, 12, 30.5}} {
					cow.With(func(m *mob.Mob) {
						m.SetPosition(at)
						m.SetRandSeed(1, 2)
						p1, ok1 := kit.FindBlock(m, rng[0], rng[1], method, func(_ cube.Pos, b world.Block) bool { return l.Has(b) })
						m.SetRandSeed(1, 2)
						p2, ok2 := kit.FindBlockIn(m, rng[0], rng[1], method, l, func(_ cube.Pos, b world.Block) bool { return l.Has(b) })
						if p1 != p2 || ok1 != ok2 {
							t.Errorf("%v %v range %v at %v: FindBlock %v %v, FindBlockIn %v %v", l, method, rng, at, p1, ok1, p2, ok2)
						}
					})
				}
			}
		}
	}
}
