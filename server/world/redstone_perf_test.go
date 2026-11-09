//go:build crossfly_full

package world_test

import (
	"context"
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator"
)

// perfBox is a Structure filled with one block.
type perfBox struct {
	dim [3]int
	b   world.Block
}

func (s perfBox) Dimensions() [3]int { return s.dim }
func (s perfBox) At(int, int, int, func(int, int, int) world.Block) (world.Block, world.Liquid) {
	return s.b, nil
}

// perfRoofGen is a flat world with a generated stone roof, as the mob spawn tests use.
type perfRoofGen struct{ generator.Flat }

func (g perfRoofGen) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	g.Flat.GenerateChunk(pos, c)
	rid := world.DefaultBlockRegistry.BlockRuntimeID(block.Stone{})
	for x := range uint8(16) {
		for z := range uint8(16) {
			c.SetBlock(x, -64+4+6, z, 0, rid)
		}
	}
}

func perfDo(w *world.World, f func(tx *world.Tx)) {
	w.Do(f).Wait(context.Background())
}

// newPerfWorld returns a synchronous world with a 160x160 stone floor at y=63 built without redstone invalidation.
func newPerfWorld(b testing.TB) *world.World {
	w := world.Config{Synchronous: true}.New()
	perfDo(w, func(tx *world.Tx) {
		tx.BuildStructure(cube.Pos{-80, 63, -80}, perfBox{dim: [3]int{160, 1, 160}, b: block.Stone{}})
	})
	w.AdvanceTick()
	return w
}

// benchEngine times only the redstone engine evaluation of the changes prep makes, then drains the rest of the
// tick (neighbour updates, scheduled ticks) untimed.
func benchEngine(b *testing.B, w *world.World, prep func(tx *world.Tx, i int)) {
	dirty := 0
	b.ReportAllocs()
	b.ResetTimer()
	b.StopTimer()
	for i := 0; i < b.N; i++ {
		perfDo(w, func(tx *world.Tx) {
			prep(tx, i)
			dirty += world.RedstoneDirtyLenForTest(w)
			b.StartTimer()
			world.RedstoneTickForTest(tx)
			b.StopTimer()
		})
		w.AdvanceTick()
	}
	b.ReportMetric(float64(dirty)/float64(b.N), "dirty/op")
}

// benchFullTick times a whole world tick after the changes prep makes.
func benchFullTick(b *testing.B, w *world.World, prep func(tx *world.Tx, i int)) {
	dirty := 0
	b.ReportAllocs()
	b.ResetTimer()
	b.StopTimer()
	for i := 0; i < b.N; i++ {
		perfDo(w, func(tx *world.Tx) {
			prep(tx, i)
			dirty += world.RedstoneDirtyLenForTest(w)
		})
		b.StartTimer()
		w.AdvanceTick()
		b.StopTimer()
	}
	b.ReportMetric(float64(dirty)/float64(b.N), "dirty/op")
}

// benchSetAndTick times the block changes prep makes (including their redstone invalidation) and the world tick.
func benchSetAndTick(b *testing.B, w *world.World, prep func(tx *world.Tx, i int)) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		perfDo(w, func(tx *world.Tx) { prep(tx, i) })
		w.AdvanceTick()
	}
}

// randomToggles toggles n random positions on top of the floor between air and planks.
func randomToggles(n int) func(tx *world.Tx, i int) {
	rng := rand.New(rand.NewPCG(1, 2))
	return func(tx *world.Tx, _ int) {
		for range n {
			pos := cube.Pos{rng.IntN(128) - 64, 64, rng.IntN(128) - 64}
			if _, ok := tx.Block(pos).(block.Air); ok {
				tx.SetBlock(pos, block.Planks{}, nil)
			} else {
				tx.SetBlock(pos, block.Air{}, nil)
			}
		}
	}
}

// roofSetBlock places (even i) or removes (odd i) a 64x64 stone roof with SetBlock.
func roofSetBlock(tx *world.Tx, i int) {
	var b world.Block = block.Stone{}
	if i%2 == 1 {
		b = block.Air{}
	}
	for x := -32; x < 32; x++ {
		for z := -32; z < 32; z++ {
			tx.SetBlock(cube.Pos{x, 70, z}, b, nil)
		}
	}
}

func roofStructure(tx *world.Tx, i int) {
	var b world.Block = block.Stone{}
	if i%2 == 1 {
		b = block.Air{}
	}
	tx.BuildStructure(cube.Pos{-32, 70, -32}, perfBox{dim: [3]int{64, 1, 64}, b: b})
}

// buildCircuit lays 8 lines of 15 redstone wire on lamps, each fed by a toggled redstone block at x=0.
func buildCircuit(tx *world.Tx) {
	for line := range 8 {
		z := line * 4
		for x := 1; x <= 15; x++ {
			tx.SetBlock(cube.Pos{x, 63, z}, block.RedstoneLamp{}, nil)
			tx.SetBlock(cube.Pos{x, 64, z}, block.RedstoneWire{}, nil)
		}
	}
}

func toggleClock(tx *world.Tx, i int) {
	var b world.Block = block.RedstoneBlock{}
	if i%2 == 1 {
		b = block.Air{}
	}
	for line := range 8 {
		tx.SetBlock(cube.Pos{0, 64, line * 4}, b, nil)
	}
}

func BenchmarkRedstonePerf(b *testing.B) {
	world.DefaultBlockRegistry.Finalize()
	for _, n := range []int{50, 500} {
		b.Run("random"+itoa(n)+"/engine", func(b *testing.B) {
			w := newPerfWorld(b)
			defer w.Close()
			benchEngine(b, w, randomToggles(n))
		})
		b.Run("random"+itoa(n)+"/fulltick", func(b *testing.B) {
			w := newPerfWorld(b)
			defer w.Close()
			benchFullTick(b, w, randomToggles(n))
		})
		b.Run("random"+itoa(n)+"/set+tick", func(b *testing.B) {
			w := newPerfWorld(b)
			defer w.Close()
			benchSetAndTick(b, w, randomToggles(n))
		})
	}
	b.Run("roof64-setblock/engine", func(b *testing.B) {
		w := newPerfWorld(b)
		defer w.Close()
		benchEngine(b, w, roofSetBlock)
	})
	b.Run("roof64-setblock/set+tick", func(b *testing.B) {
		w := newPerfWorld(b)
		defer w.Close()
		benchSetAndTick(b, w, roofSetBlock)
	})
	b.Run("roof64-structure/set+tick", func(b *testing.B) {
		w := newPerfWorld(b)
		defer w.Close()
		benchSetAndTick(b, w, roofStructure)
	})
	b.Run("roof64-structure/engine", func(b *testing.B) {
		w := newPerfWorld(b)
		defer w.Close()
		benchEngine(b, w, roofStructure)
	})
	// After one bulk placement the dirty map keeps its grown capacity: every later tick with a single change
	// used to clone the whole table.
	b.Run("after-roof-1change/fulltick", func(b *testing.B) {
		w := newPerfWorld(b)
		defer w.Close()
		perfDo(w, func(tx *world.Tx) { roofSetBlock(tx, 0) })
		w.AdvanceTick()
		benchFullTick(b, w, randomToggles(1))
	})
	b.Run("genroof-random50/fulltick", func(b *testing.B) {
		gen := perfRoofGen{generator.NewFlat(biome.Plains{}, []world.Block{block.Grass{}, block.Dirt{}, block.Dirt{}, block.Bedrock{}})}
		w := world.Config{Synchronous: true, Generator: gen}.New()
		defer w.Close()
		perfDo(w, func(tx *world.Tx) {
			for cx := -6; cx <= 6; cx++ {
				for cz := -6; cz <= 6; cz++ {
					tx.Block(cube.Pos{cx * 16, 0, cz * 16})
				}
			}
		})
		w.AdvanceTick()
		rng := rand.New(rand.NewPCG(3, 4))
		benchFullTick(b, w, func(tx *world.Tx, _ int) {
			for range 50 {
				pos := cube.Pos{rng.IntN(96) - 48, -64 + 4, rng.IntN(96) - 48}
				if _, ok := tx.Block(pos).(block.Air); ok {
					tx.SetBlock(pos, block.Planks{}, nil)
				} else {
					tx.SetBlock(pos, block.Air{}, nil)
				}
			}
		})
	})
	b.Run("circuit/engine", func(b *testing.B) {
		w := newPerfWorld(b)
		defer w.Close()
		perfDo(w, buildCircuit)
		for range 5 {
			w.AdvanceTick()
		}
		benchEngine(b, w, toggleClock)
	})
	b.Run("circuit/fulltick", func(b *testing.B) {
		w := newPerfWorld(b)
		defer w.Close()
		perfDo(w, buildCircuit)
		for range 5 {
			w.AdvanceTick()
		}
		benchFullTick(b, w, toggleClock)
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

// TestRedstoneDirtyOnlyNearRedstone checks which block changes queue redstone evaluation.
func TestRedstoneDirtyOnlyNearRedstone(t *testing.T) {
	w := newPerfWorld(t)
	defer w.Close()
	dirtyAfter := func(f func(tx *world.Tx)) int {
		n := 0
		perfDo(w, func(tx *world.Tx) {
			f(tx)
			n = world.RedstoneDirtyLenForTest(w)
		})
		w.AdvanceTick()
		w.AdvanceTick()
		return n
	}
	// A block placed in an empty field is not near anything redstone.
	if n := dirtyAfter(func(tx *world.Tx) { tx.SetBlock(cube.Pos{40, 64, 40}, block.Planks{}, nil) }); n != 0 {
		t.Fatalf("block in an empty field queued %d redstone positions", n)
	}
	// Placing and removing redstone components always queues.
	if n := dirtyAfter(func(tx *world.Tx) { tx.SetBlock(cube.Pos{0, 64, 0}, block.RedstoneWire{}, nil) }); n == 0 {
		t.Fatal("placing wire queued nothing")
	}
	// A solid block next to the wire.
	if n := dirtyAfter(func(tx *world.Tx) { tx.SetBlock(cube.Pos{1, 64, 0}, block.Stone{}, nil) }); n == 0 {
		t.Fatal("block beside wire queued nothing")
	}
	// A block two away, next to a conductor beside a lamp (stone at {11}, lamp at {12}).
	perfDo(w, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{11, 64, 0}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{12, 64, 0}, block.RedstoneLamp{}, nil)
	})
	w.AdvanceTick()
	if n := dirtyAfter(func(tx *world.Tx) { tx.SetBlock(cube.Pos{10, 64, 0}, block.Planks{}, nil) }); n == 0 {
		t.Fatal("block next to a conductor beside a lamp queued nothing")
	}
	// Removing the wire queues.
	if n := dirtyAfter(func(tx *world.Tx) { tx.SetBlock(cube.Pos{0, 64, 0}, block.Air{}, nil) }); n == 0 {
		t.Fatal("removing wire queued nothing")
	}
}

// TestRedstoneStonePlacedOnTorchPowersLamp checks a non-redstone block placed next to a component is still evaluated:
// stone placed on a torch is strongly powered by it and lights the lamp beside it.
func TestRedstoneStonePlacedOnTorchPowersLamp(t *testing.T) {
	w := newPerfWorld(t)
	defer w.Close()
	torch := cube.Pos{20, 64, 20}
	lamp := cube.Pos{21, 65, 20}
	perfDo(w, func(tx *world.Tx) {
		tx.SetBlock(torch, block.RedstoneTorch{Facing: cube.FaceDown, Lit: true}, nil)
		tx.SetBlock(lamp, block.RedstoneLamp{}, nil)
	})
	for range 4 {
		w.AdvanceTick()
	}
	perfDo(w, func(tx *world.Tx) {
		if l := tx.Block(lamp).(block.RedstoneLamp); l.Lit {
			t.Fatal("lamp lit before the stone was placed")
		}
		// The torch strongly powers the stone above it, which powers the lamp beside the stone.
		tx.SetBlock(torch.Side(cube.FaceUp), block.Stone{}, nil)
	})
	for range 4 {
		w.AdvanceTick()
	}
	perfDo(w, func(tx *world.Tx) {
		if l := tx.Block(lamp).(block.RedstoneLamp); !l.Lit {
			t.Fatal("lamp beside the torch-powered stone did not light")
		}
	})
}
