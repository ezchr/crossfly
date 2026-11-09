//go:build crossfly_full

package world_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// Benchmarks of whole world ticks over Java redstone machines (Crossfly Full). Each op is one world tick.

// machine is a redstone build: build places it, step changes its inputs before tick i (may be nil).
type machine struct {
	name  string
	build func(tx *world.Tx)
	step  func(tx *world.Tx, i int)
}

// longLine is a 150 long dust line with a repeater every 15 dust, toggled every 4 ticks at its start.
func longLine() machine {
	return machine{name: "longline150",
		build: func(tx *world.Tx) {
			for x := -74; x <= 75; x++ {
				if (x+75)%16 == 0 {
					tx.SetBlock(cube.Pos{x, 64, 0}, block.RedstoneRepeater{Facing: cube.East, Delay: 0}, nil)
					continue
				}
				tx.SetBlock(cube.Pos{x, 64, 0}, block.RedstoneWire{}, nil)
			}
			tx.SetBlock(cube.Pos{76, 64, 0}, block.RedstoneLamp{}, nil)
		},
		step: func(tx *world.Tx, i int) {
			if i%4 != 0 {
				return
			}
			var b world.Block = block.RedstoneBlock{}
			if i%8 == 4 {
				b = block.Air{}
			}
			tx.SetBlock(cube.Pos{-75, 64, 0}, b, nil)
		},
	}
}

// observerClock is two observers watching each other (a 2 tick clock) driving 15 dust over lamps.
func observerClock() machine {
	return machine{name: "observerclock",
		build: func(tx *world.Tx) {
			for x := 2; x <= 16; x++ {
				tx.SetBlock(cube.Pos{x, 63, 0}, block.RedstoneLamp{}, nil)
				tx.SetBlock(cube.Pos{x, 64, 0}, block.RedstoneWire{}, nil)
			}
			// The output of b (its back) faces the dust.
			tx.SetBlock(cube.Pos{0, 64, 0}, block.Observer{Facing: cube.FaceEast}, nil)
			tx.SetBlock(cube.Pos{1, 64, 0}, block.Observer{Facing: cube.FaceWest}, nil)
			block.ObserverSetblockPulse(cube.Pos{0, 64, 0}, tx)
		},
	}
}

// pistonDoor is 8 sticky pistons pushing stone, each driven by a redstone block toggled every 6 ticks.
func pistonDoor() machine {
	return machine{name: "pistondoor8",
		build: func(tx *world.Tx) {
			for z := 0; z < 8; z++ {
				tx.SetBlock(cube.Pos{1, 64, z}, block.Piston{Facing: cube.FaceEast, Sticky: true}, nil)
				tx.SetBlock(cube.Pos{2, 64, z}, block.Stone{}, nil)
			}
		},
		step: func(tx *world.Tx, i int) {
			if i%6 != 0 {
				return
			}
			var b world.Block = block.RedstoneBlock{}
			if i%12 == 6 {
				b = block.Air{}
			}
			for z := 0; z < 8; z++ {
				tx.SetBlock(cube.Pos{0, 64, z}, b, nil)
			}
		},
	}
}

// hopperChain is 16 hoppers passing items east into a chest, read by a comparator driving 14 dust.
func hopperChain() machine {
	return machine{name: "hopperchain16",
		build: func(tx *world.Tx) {
			for x := 0; x < 16; x++ {
				h := block.NewHopper()
				h.Facing = cube.FaceEast
				tx.SetBlock(cube.Pos{x, 64, 0}, h, nil)
			}
			tx.SetBlock(cube.Pos{16, 64, 0}, block.NewChest(), nil)
			tx.SetBlock(cube.Pos{17, 64, 0}, block.RedstoneComparator{Facing: cube.East}, nil)
			for x := 18; x < 32; x++ {
				tx.SetBlock(cube.Pos{x, 64, 0}, block.RedstoneWire{}, nil)
			}
		},
		step: func(tx *world.Tx, i int) {
			if i%200 != 0 {
				return
			}
			// Refill the first hopper and empty the chest so the chain keeps moving.
			if h, ok := tx.Block(cube.Pos{0, 64, 0}).(block.Hopper); ok {
				for s := 0; s < 5; s++ {
					_ = h.Inventory(tx, cube.Pos{0, 64, 0}).SetItem(s, item.NewStack(block.Stone{}, 64))
				}
			}
			if c, ok := tx.Block(cube.Pos{16, 64, 0}).(block.Chest); ok {
				c.Inventory(tx, cube.Pos{16, 64, 0}).Clear()
			}
		},
	}
}

// idleField is 64 lines of 64 dust over stone that never change.
func idleField() machine {
	return machine{name: "idle4096dust",
		build: func(tx *world.Tx) {
			for z := 0; z < 64; z++ {
				for x := 0; x < 64; x++ {
					tx.SetBlock(cube.Pos{x - 32, 64, z*2 - 64}, block.RedstoneWire{}, nil)
				}
			}
		},
	}
}

// empty is the stone floor alone.
func empty() machine { return machine{name: "empty", build: func(*world.Tx) {}} }

func eightLines() machine {
	return machine{name: "lines8", build: buildCircuit, step: toggleClock}
}

func allMachines() []machine {
	return []machine{empty(), eightLines(), longLine(), observerClock(), pistonDoor(), hopperChain(), idleField()}
}

func newMachineWorld(b testing.TB, m machine) *world.World {
	world.DefaultBlockRegistry.Finalize()
	w := world.Config{Synchronous: true, Entities: entity.WithJavaRedstone(entity.DefaultRegistry.Config()).New(append(entity.DefaultRegistry.Types(), entity.JavaRedstoneTypes()...))}.New()
	perfDo(w, func(tx *world.Tx) {
		tx.BuildStructure(cube.Pos{-80, 63, -80}, perfBox{dim: [3]int{160, 1, 160}, b: block.Stone{}})
	})
	w.AdvanceTick()
	perfDo(w, m.build)
	for range 40 {
		w.AdvanceTick()
	}
	return w
}

// BenchmarkJavaRedstoneMachines times whole world ticks of each machine.
func BenchmarkJavaRedstoneMachines(b *testing.B) {
	for _, m := range allMachines() {
		b.Run(m.name, func(b *testing.B) {
			w := newMachineWorld(b, m)
			defer w.Close()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if m.step != nil {
					perfDo(w, func(tx *world.Tx) { m.step(tx, i) })
				}
				w.AdvanceTick()
			}
		})
	}
}
