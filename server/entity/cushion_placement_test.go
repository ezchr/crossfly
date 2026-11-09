package entity

import (
	"context"
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func cushionTestWorld(t *testing.T) *world.World {
	t.Helper()
	w := world.Config{Synchronous: true, Entities: DefaultRegistry, SettleSynchronousTransactions: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func cushionDo(w *world.World, f func(tx *world.Tx)) { w.Do(f).Wait(context.Background()) }

// cushionClick is a block to use a cushion on and the point clicked on it, as a Java client sends it: On the top
// of its outline. Y is where vanilla 26.3 puts the cushion, relative to the block.
type cushionClick struct {
	name  string
	block world.Block
	click mgl64.Vec3
	face  cube.Face
	y     float64
	fails bool
}

func cushionWheat(growth int) block.WheatSeeds {
	w := block.WheatSeeds{}
	w.Growth = growth
	return w
}

func cushionClicks() []cushionClick {
	planks := block.Planks{Wood: block.OakWood()}
	clicks := []cushionClick{
		{name: "stone", block: block.Stone{}, click: mgl64.Vec3{0.5, 1, 0.5}, y: 1},
		{name: "bottom slab", block: block.Slab{Block: planks}, click: mgl64.Vec3{0.3, 0.5, 0.6}, y: 0.5},
		{name: "top slab", block: block.Slab{Block: planks, Top: true}, click: mgl64.Vec3{0.5, 1, 0.5}, y: 1},
		{name: "closed bottom trapdoor", block: block.WoodTrapdoor{Wood: block.OakWood()}, click: mgl64.Vec3{0.5, 0.1875, 0.5}, y: 0.1875},
		{name: "closed top trapdoor", block: block.WoodTrapdoor{Wood: block.OakWood(), Top: true}, click: mgl64.Vec3{0.5, 1, 0.5}, y: 1},
		{name: "carpet", block: block.Carpet{Colour: item.ColourRed()}, click: mgl64.Vec3{0.5, 0.0625, 0.5}, y: 0.0625},
		{name: "bed", block: block.Bed{Colour: item.ColourRed()}, click: mgl64.Vec3{0.5, 0.5625, 0.5}, y: 0.5625},
		{name: "stairs, lower step", block: block.Stairs{Block: planks, Facing: cube.North}, click: mgl64.Vec3{0.5, 0.5, 0.75}, y: 0.5},
		{name: "stairs, upper step", block: block.Stairs{Block: planks, Facing: cube.North}, click: mgl64.Vec3{0.5, 1, 0.25}, y: 1},
		{name: "farmland", block: block.Farmland{}, click: mgl64.Vec3{0.5, 0.9375, 0.5}, y: 0.9375},
		{name: "dirt path", block: block.DirtPath{}, click: mgl64.Vec3{0.5, 0.9375, 0.5}, y: 0.9375},
		{name: "soul sand", block: block.SoulSand{}, click: mgl64.Vec3{0.5, 1, 0.5}, y: 1},
		{name: "enchanting table", block: block.EnchantingTable{}, click: mgl64.Vec3{0.5, 0.75, 0.5}, y: 0.75},
		{name: "end portal frame", block: block.EndPortalFrame{}, click: mgl64.Vec3{0.5, 0.8125, 0.5}, y: 0.8125},
		{name: "end portal frame with an eye", block: block.EndPortalFrame{Eye: true}, click: mgl64.Vec3{0.5, 1, 0.5}, y: 1},
		{name: "chest", block: block.NewChest(), click: mgl64.Vec3{0.5, 0.875, 0.5}, y: 0.875},
		// Bedrock's chest box is 0.95 high: The cushion rests on the Java chest, not above it.
		{name: "chest, clicked by a Bedrock client", block: block.NewChest(), click: mgl64.Vec3{0.5, 0.95, 0.5}, y: 0.875},
		{name: "lectern", block: block.Lectern{}, click: mgl64.Vec3{0.5, 1.125, 0.5}, y: 1.125},
		{name: "cake", block: block.Cake{}, click: mgl64.Vec3{0.5, 0.5, 0.5}, y: 0.5},
		{name: "unlit campfire", block: block.Campfire{Extinguished: true}, click: mgl64.Vec3{0.5, 0.4375, 0.5}, y: 0.4375},
		{name: "fence", block: block.WoodFence{Wood: block.OakWood()}, click: mgl64.Vec3{0.5, 1, 0.5}, y: 1},
		{name: "lantern", block: block.Lantern{Type: block.NormalFire()}, click: mgl64.Vec3{0.5, 0.5625, 0.5}, y: 0.5625},
		{name: "cauldron floor", block: block.Cauldron{}, click: mgl64.Vec3{0.5, 0.25, 0.5}, y: 0.25},
		{name: "cauldron rim", block: block.Cauldron{}, click: mgl64.Vec3{0.05, 1, 0.5}, y: 1},
		// A Java client reports the inner walls of a cauldron as its top face; the collision shape says otherwise.
		{name: "cauldron inner wall", block: block.Cauldron{}, click: mgl64.Vec3{0.125, 0.6, 0.5}, fails: true},
		{name: "composter", block: block.Composter{Level: 4}, click: mgl64.Vec3{0.5, 0.5625, 0.5}, y: 0.5625},
		{name: "hopper", block: block.Hopper{Facing: cube.FaceDown}, click: mgl64.Vec3{0.5, 0.6875, 0.5}, y: 0.6875},
		{name: "side of a block", block: block.Stone{}, click: mgl64.Vec3{1, 0.5, 0.5}, face: cube.FaceEast, fails: true},
		{name: "short grass", block: block.ShortGrass{}, click: mgl64.Vec3{0.5, 0.8125, 0.5}, y: 0.8125},
		{name: "flower", block: block.Flower{}, click: mgl64.Vec3{0.5, 0.625, 0.5}, y: 0.625},
		{name: "torch", block: block.Torch{Facing: cube.FaceDown, Type: block.NormalFire()}, click: mgl64.Vec3{0.5, 0.625, 0.5}, y: 0.625},
		{name: "wheat, stage 3", block: cushionWheat(3), click: mgl64.Vec3{0.5, 0.5, 0.5}, y: 0.5},
	}
	for layers := 1; layers <= 8; layers++ {
		h := float64(layers) / 8
		clicks = append(clicks, cushionClick{name: "snow layers", block: block.SnowLayer{Height: layers - 1}, click: mgl64.Vec3{0.5, h, 0.5}, y: h})
	}
	for i := range clicks {
		if clicks[i].face == 0 {
			clicks[i].face = cube.FaceUp
		}
	}
	return clicks
}

// TestCushionPlacementHeights places a cushion on top of blocks as vanilla does and checks it is still there after
// its first support check.
func TestCushionPlacementHeights(t *testing.T) {
	for _, c := range cushionClicks() {
		w := cushionTestWorld(t)
		pos := cube.Pos{3, 64, 3}
		var h *world.EntityHandle
		cushionDo(w, func(tx *world.Tx) {
			tx.SetBlock(pos.Side(cube.FaceDown), block.Stone{}, &world.SetOpts{DisableBlockUpdates: true})
			tx.SetBlock(pos, c.block, &world.SetOpts{DisableBlockUpdates: true})
			spawn, ok := cushionPlacement(pos, c.face, c.click, tx)
			if ok == c.fails {
				t.Errorf("%s: placed %v, want %v", c.name, ok, !c.fails)
				return
			}
			if !ok {
				return
			}
			want := mgl64.Vec3{3.5, 64 + c.y, 3.5}
			if spawn.Sub(want).Len() > 1e-9 {
				t.Errorf("%s: cushion at %v, want %v", c.name, spawn, want)
			}
			h = NewCushion(world.EntitySpawnOpts{Position: spawn}, item.ColourWhite())
			tx.AddEntity(h)
		})
		if h == nil {
			continue
		}
		for range 3 {
			w.AdvanceTick()
		}
		cushionDo(w, func(tx *world.Tx) {
			if _, ok := h.Entity(tx); !ok {
				t.Errorf("%s: cushion at %.4f broke after its support check", c.name, c.y)
			}
		})
	}
}

// TestCushionSurvival checks vanilla's support rule: A block's outline must reach the 1/64 below the cushion.
func TestCushionSurvival(t *testing.T) {
	w := cushionTestWorld(t)
	pos := cube.Pos{0, 64, 0}
	cases := []struct {
		name  string
		block world.Block
		y     float64
		want  bool
	}{
		{"on a bottom slab", block.Slab{Block: block.Planks{Wood: block.OakWood()}}, 0.5, true},
		{"a pixel above a bottom slab", block.Slab{Block: block.Planks{Wood: block.OakWood()}}, 0.5 + 1.0/16, false},
		{"just within 1/64 above a bottom slab", block.Slab{Block: block.Planks{Wood: block.OakWood()}}, 0.5 + 1.0/128, true},
		{"on one snow layer", block.SnowLayer{}, 0.125, true},
		{"on the collision box of two snow layers", block.SnowLayer{Height: 1}, 0.125, true},
		{"on top of a fence post", block.WoodFence{Wood: block.OakWood()}, 1, true},
		{"on top of a fence's collision box", block.WoodFence{Wood: block.OakWood()}, 1.5, false},
		{"on air", block.Air{}, 0, false},
		{"in a cobweb", block.Cobweb{}, 0.5, true},
		{"on a lectern, above the block", block.Lectern{}, 1.125, true},
		{"on a wall without post or sides", block.Wall{Block: block.Cobblestone{}}, 0.8125, false},
	}
	cushionDo(w, func(tx *world.Tx) {
		for _, c := range cases {
			tx.SetBlock(pos, c.block, &world.SetOpts{DisableBlockUpdates: true})
			tx.SetBlock(pos.Side(cube.FaceUp), block.Air{}, &world.SetOpts{DisableBlockUpdates: true})
			at := mgl64.Vec3{0.5, 64 + c.y, 0.5}
			if got := cushionCanSurvive(at, tx); got != c.want {
				t.Errorf("%s (y %.4f): survives %v, want %v", c.name, c.y, got, c.want)
			}
		}
		// Fully covered by a suffocating block, a cushion breaks.
		tx.SetBlock(pos, block.Stone{}, nil)
		tx.SetBlock(pos.Side(cube.FaceUp), block.Stone{}, nil)
		if cushionCanSurvive(mgl64.Vec3{0.5, 65, 0.5}, tx) {
			t.Errorf("a cushion inside stone survives")
		}
		// Glass does not suffocate.
		tx.SetBlock(pos.Side(cube.FaceUp), block.Glass{}, nil)
		if !cushionCanSurvive(mgl64.Vec3{0.5, 65, 0.5}, tx) {
			t.Errorf("a cushion inside glass on stone breaks")
		}
	})
}

// TestCushionNotInsideCollision refuses cushions whose bottom is fully inside the collision boxes of blocks.
func TestCushionNotInsideCollision(t *testing.T) {
	w := cushionTestWorld(t)
	pos := cube.Pos{0, 64, 0}
	cushionDo(w, func(tx *world.Tx) {
		tx.SetBlock(pos, block.Stone{}, nil)
		tx.SetBlock(pos.Side(cube.FaceUp), block.Slab{Block: block.Planks{Wood: block.OakWood()}}, nil)
		// Clicked through to the stone below a slab: Its bottom half is solid there.
		if !cushionBuried(mgl64.Vec3{0.5, 65, 0.5}, tx) {
			t.Errorf("a cushion at the bottom of a slab is not buried")
		}
		if cushionBuried(mgl64.Vec3{0.5, 65.5, 0.5}, tx) {
			t.Errorf("a cushion on top of a slab is buried")
		}
		if _, ok := cushionPlacement(pos, cube.FaceUp, mgl64.Vec3{0.5, 1, 0.5}, tx); ok {
			t.Errorf("a cushion was placed inside a slab")
		}
	})
	if math.Abs(cushionBox(mgl64.Vec3{}).Height()-0.25) > 1e-12 {
		t.Errorf("cushion box height %v", cushionBox(mgl64.Vec3{}).Height())
	}
}
