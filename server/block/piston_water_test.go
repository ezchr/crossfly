//go:build crossfly_full

package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// The expected results below are vanilla Java 26.3's, read from PistonBaseBlock.moveBlocks and
// PistonMovingBlockEntity.tick/finalTick and checked on a vanilla server.

func pistonWaterWorld(t *testing.T) *world.World {
	t.Helper()
	w := world.Config{Dim: world.End, Synchronous: true, Entities: redstoneBreakDropTestEntityRegistry()}.New()
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func pistonWaterSlab() Slab { return Slab{Block: Cobblestone{}} }

func waterSourceAt(tx *world.Tx, pos cube.Pos) bool {
	l, ok := tx.Liquid(pos)
	if !ok {
		return false
	}
	w, ok := l.(Water)
	return ok && w.Depth == 8 && !w.Falling
}

func hasLiquid(tx *world.Tx, pos cube.Pos) bool {
	_, ok := tx.Liquid(pos)
	return ok
}

// A waterlogged block pushed all the way arrives dry (tick clears WATERLOGGED), and the spot it left is air
// (moveBlocks sets it to air, so its water is gone too).
func TestPistonPushedWaterloggedBlockArrivesDry(t *testing.T) {
	w := pistonWaterWorld(t)
	pos := cube.Pos{0, 64, 0}
	runWorld(w, func(tx *world.Tx) {
		tx.SetBlock(pos, Piston{Facing: cube.FaceEast}, nil)
		tx.SetBlock(pos.Add(cube.Pos{1}), pistonWaterSlab(), nil)
		tx.SetLiquid(pos.Add(cube.Pos{1}), Water{Depth: 8, Still: true})
		tx.SetBlock(pos.Side(cube.FaceWest), RedstoneBlock{}, nil)
	})
	w.AdvanceTick()
	runWorld(w, func(tx *world.Tx) {
		m, ok := tx.Block(pos.Add(cube.Pos{2})).(MovingPiston)
		if !ok || !m.Waterlogged {
			t.Fatalf("moving slab: %#v, want a waterlogged moving block", tx.Block(pos.Add(cube.Pos{2})))
		}
		if hasLiquid(tx, pos.Add(cube.Pos{1})) {
			t.Error("the water stayed where the slab was pushed from")
		}
	})
	for range 4 {
		w.AdvanceTick()
	}
	runWorld(w, func(tx *world.Tx) {
		if _, ok := tx.Block(pos.Add(cube.Pos{2})).(Slab); !ok {
			t.Fatalf("slab not pushed: %T", tx.Block(pos.Add(cube.Pos{2})))
		}
		if hasLiquid(tx, pos.Add(cube.Pos{2})) {
			t.Error("pushed slab kept its water, Java places it dry")
		}
		if _, ok := tx.Block(pos.Add(cube.Pos{1})).(PistonHead); !ok || hasLiquid(tx, pos.Add(cube.Pos{1})) {
			t.Errorf("origin: %T (water %t), want a dry piston head", tx.Block(pos.Add(cube.Pos{1})), hasLiquid(tx, pos.Add(cube.Pos{1})))
		}
	})
}

// A sticky piston pulling a waterlogged block: it arrives dry and the spot it was pulled from is air.
func TestPistonPulledWaterloggedBlockArrivesDry(t *testing.T) {
	w := pistonWaterWorld(t)
	pos := cube.Pos{0, 64, 0}
	runWorld(w, func(tx *world.Tx) {
		tx.SetBlock(pos, Piston{Facing: cube.FaceEast, Sticky: true}, nil)
		tx.SetBlock(pos.Side(cube.FaceWest), RedstoneBlock{}, nil)
	})
	for range 4 {
		w.AdvanceTick()
	}
	runWorld(w, func(tx *world.Tx) {
		tx.SetBlock(pos.Add(cube.Pos{2}), pistonWaterSlab(), &world.SetOpts{DisableBlockUpdates: true})
		tx.SetLiquid(pos.Add(cube.Pos{2}), Water{Depth: 8, Still: true})
		tx.SetBlock(pos.Side(cube.FaceWest), Air{}, nil)
	})
	for range 4 {
		w.AdvanceTick()
	}
	runWorld(w, func(tx *world.Tx) {
		if _, ok := tx.Block(pos.Add(cube.Pos{1})).(Slab); !ok {
			t.Fatalf("slab not pulled: %T", tx.Block(pos.Add(cube.Pos{1})))
		}
		if hasLiquid(tx, pos.Add(cube.Pos{1})) {
			t.Error("pulled slab kept its water, Java places it dry")
		}
		if _, ok := tx.Block(pos.Add(cube.Pos{2})).(Air); !ok || hasLiquid(tx, pos.Add(cube.Pos{2})) {
			t.Errorf("pulled-from spot: %T (water %t), want dry air", tx.Block(pos.Add(cube.Pos{2})), hasLiquid(tx, pos.Add(cube.Pos{2})))
		}
	})
}

// A move cut short (the sticky piston loses power before the block arrives) ends in finalTick, which places the
// moved state as it is: the block keeps its water.
func TestPistonDroppedWaterloggedBlockKeepsWater(t *testing.T) {
	w := pistonWaterWorld(t)
	pos := cube.Pos{0, 64, 0}
	runWorld(w, func(tx *world.Tx) {
		tx.SetBlock(pos, Piston{Facing: cube.FaceEast, Sticky: true}, nil)
		tx.SetBlock(pos.Add(cube.Pos{1}), pistonWaterSlab(), nil)
		tx.SetLiquid(pos.Add(cube.Pos{1}), Water{Depth: 8, Still: true})
		tx.SetBlock(pos.Side(cube.FaceWest), RedstoneBlock{}, nil)
	})
	w.AdvanceTick()
	runWorld(w, func(tx *world.Tx) { tx.SetBlock(pos.Side(cube.FaceWest), Air{}, nil) })
	for range 4 {
		w.AdvanceTick()
	}
	runWorld(w, func(tx *world.Tx) {
		if _, ok := tx.Block(pos.Add(cube.Pos{2})).(Slab); !ok {
			t.Fatalf("slab not dropped one block on: %T", tx.Block(pos.Add(cube.Pos{2})))
		}
		if !waterSourceAt(tx, pos.Add(cube.Pos{2})) {
			t.Error("dropped slab lost its water, Java's finalTick keeps it")
		}
		if _, ok := tx.Block(pos.Add(cube.Pos{1})).(Air); !ok || hasLiquid(tx, pos.Add(cube.Pos{1})) {
			t.Errorf("origin: %T (water %t), want dry air", tx.Block(pos.Add(cube.Pos{1})), hasLiquid(tx, pos.Add(cube.Pos{1})))
		}
	})
}

// Water is destroyed by pistons: a block pushed into a source (dry or waterlogged) arrives dry and the water is
// gone; kelp in the way is destroyed with its water.
func TestPistonPushIntoWaterAndKelp(t *testing.T) {
	cases := []struct {
		name          string
		first, second world.Block
		firstWater    bool
	}{
		{name: "stone into a source", first: Stone{}, second: Water{Depth: 8, Still: true}},
		{name: "waterlogged slab into a source", first: pistonWaterSlab(), firstWater: true, second: Water{Depth: 8, Still: true}},
		{name: "stone into kelp", first: Stone{}, second: Kelp{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := pistonWaterWorld(t)
			pos := cube.Pos{0, 64, 0}
			runWorld(w, func(tx *world.Tx) {
				// A glass box on a stone floor keeps the water from spreading (and the kelp standing).
				for x := -1; x <= 3; x++ {
					tx.SetBlock(cube.Pos{x, 63, 0}, Stone{}, nil)
				}
				for x := 1; x <= 3; x++ {
					for _, at := range []cube.Pos{{x, 64, -1}, {x, 64, 1}, {x, 65, 0}} {
						tx.SetBlock(at, Glass{}, nil)
					}
				}
				tx.SetBlock(cube.Pos{3, 64, 0}, Glass{}, nil)
				tx.SetBlock(pos, Piston{Facing: cube.FaceEast}, nil)
				tx.SetBlock(pos.Add(cube.Pos{1}), c.first, nil)
				if c.firstWater {
					tx.SetLiquid(pos.Add(cube.Pos{1}), Water{Depth: 8, Still: true})
				}
				tx.SetBlock(pos.Add(cube.Pos{2}), c.second, nil)
				if _, kelp := c.second.(Kelp); kelp {
					tx.SetLiquid(pos.Add(cube.Pos{2}), Water{Depth: 8, Still: true})
				}
				tx.SetBlock(pos.Side(cube.FaceWest), RedstoneBlock{}, nil)
			})
			for range 4 {
				w.AdvanceTick()
			}
			runWorld(w, func(tx *world.Tx) {
				got := tx.Block(pos.Add(cube.Pos{2}))
				if world.BlockRuntimeID(got) != world.BlockRuntimeID(c.first) {
					t.Fatalf("pushed block: %T, want %T", got, c.first)
				}
				if hasLiquid(tx, pos.Add(cube.Pos{2})) {
					t.Error("the water pushed into is still there")
				}
				if _, ok := tx.Block(pos.Add(cube.Pos{1})).(PistonHead); !ok || hasLiquid(tx, pos.Add(cube.Pos{1})) {
					t.Errorf("origin: %T (water %t), want a dry piston head", tx.Block(pos.Add(cube.Pos{1})), hasLiquid(tx, pos.Add(cube.Pos{1})))
				}
			})
		})
	}
}
