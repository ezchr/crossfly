package block

import (
	"context"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// snowPlacer is a user that places blocks straight into tx.
type snowPlacer struct {
	cauldronUser
	tx *world.Tx
}

func (u *snowPlacer) PlaceBlock(pos cube.Pos, b world.Block, ctx *item.UseContext) {
	u.tx.SetBlock(pos, b, nil)
	ctx.CountSub++
}

func snowWorld(t *testing.T) *world.World {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func do(t *testing.T, w *world.World, f func(tx *world.Tx)) {
	t.Helper()
	if err := w.Do(f).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSnowLayerStacking(t *testing.T) {
	w := snowWorld(t)
	ground, pos := cube.Pos{0, 0, 0}, cube.Pos{0, 1, 0}
	do(t, w, func(tx *world.Tx) {
		tx.SetBlock(ground, Stone{}, nil)
		u := &snowPlacer{tx: tx}
		use := func(clicked cube.Pos, face cube.Face) bool {
			return SnowLayer{}.UseOnBlock(clicked, face, mgl64.Vec3{}, tx, u, &item.UseContext{})
		}
		if !use(ground, cube.FaceUp) {
			t.Fatal("placing on stone failed")
		}
		if got := tx.Block(pos); got != (SnowLayer{}) {
			t.Fatalf("placed %v, want one layer", got)
		}
		for h := 1; h <= 7; h++ {
			if !use(pos, cube.FaceUp) {
				t.Fatalf("adding layer %d failed", h+1)
			}
			if got := tx.Block(pos); got != (SnowLayer{Height: h}) {
				t.Fatalf("after layer %d: %v", h+1, got)
			}
		}
		// Eight layers take no more: the layer goes on top of them instead.
		if !use(pos, cube.FaceUp) {
			t.Error("placing on eight layers failed")
		}
		if got := tx.Block(pos); got != (SnowLayer{Height: 7}) {
			t.Errorf("eight layers became %v, want them to stay eight layers", got)
		}
		if got := tx.Block(pos.Side(cube.FaceUp)); got != (SnowLayer{}) {
			t.Errorf("on eight layers: %v, want a new layer", got)
		}
		// Clicking the top of the block below a snow layer adds to that layer, as it is next to the face clicked.
		side := cube.Pos{1, 1, 0}
		tx.SetBlock(cube.Pos{1, 0, 0}, Stone{}, nil)
		tx.SetBlock(side, SnowLayer{Height: 2}, nil)
		if !use(cube.Pos{1, 0, 0}, cube.FaceUp) || tx.Block(side) != (SnowLayer{Height: 3}) {
			t.Errorf("clicking the block below: %v, want 4 layers", tx.Block(side))
		}
	})
}

func TestSnowLayerSupport(t *testing.T) {
	w := snowWorld(t)
	do(t, w, func(tx *world.Tx) {
		u := &snowPlacer{tx: tx}
		cases := []struct {
			below world.Block
			ok    bool
		}{
			{Stone{}, true},
			{Grass{}, true},
			{Leaves{Type: OakLeaves()}, true},
			{SoulSand{}, true},
			{SnowLayer{Height: 7}, true},
			{SnowLayer{Height: 3}, false},
			{Ice{}, false},
			{PackedIce{}, false},
			{Barrier{}, false},
			{Slab{Block: Stone{}}, false},
			{Carpet{Colour: item.ColourWhite()}, false},
			{Slab{Block: Stone{}, Top: true}, true},
		}
		for i, c := range cases {
			below := cube.Pos{i * 2, 10, 0}
			tx.SetBlock(below.Side(cube.FaceDown), Stone{}, nil)
			tx.SetBlock(below, c.below, nil)
			placedOK := SnowLayer{}.UseOnBlock(below, cube.FaceUp, mgl64.Vec3{}, tx, u, &item.UseContext{})
			if _, isSnow := c.below.(SnowLayer); isSnow && !c.ok {
				// Clicking a lower snow layer adds to it instead.
				continue
			}
			_, there := tx.Block(below.Side(cube.FaceUp)).(SnowLayer)
			if placedOK != c.ok || there != c.ok {
				t.Errorf("on %T %+v: placed=%v there=%v, want %v", c.below, c.below, placedOK, there, c.ok)
			}
		}
	})
	// Removing the support removes the layer, without drops.
	support, pos := cube.Pos{0, 20, 0}, cube.Pos{0, 21, 0}
	do(t, w, func(tx *world.Tx) {
		tx.SetBlock(support, Stone{}, nil)
		tx.SetBlock(pos, SnowLayer{Height: 2}, nil)
		tx.SetBlock(support, Air{}, nil)
	})
	w.AdvanceTick()
	do(t, w, func(tx *world.Tx) {
		if b := tx.Block(pos); b != (Air{}) {
			t.Errorf("snow layer stayed without support: %v", b)
		}
		for e := range tx.Entities() {
			t.Errorf("entity %v left behind", e.H().Type().EncodeEntity())
		}
	})
}

func TestSnowLayerReplaceable(t *testing.T) {
	if !(SnowLayer{}).ReplaceableBy(Stone{}) {
		t.Error("one layer not replaceable by stone")
	}
	if (SnowLayer{Height: 1}).ReplaceableBy(Stone{}) {
		t.Error("two layers replaceable by stone")
	}
	if (SnowLayer{}).ReplaceableBy(SnowLayer{}) {
		t.Error("snow layer replaced by snow layer instead of adding a layer")
	}
	if got := (SnowLayer{}).Model().BBox(cube.Pos{}, nil); len(got) != 0 {
		t.Errorf("one layer has collision %v", got)
	}
	if got := (SnowLayer{Height: 4}).Model().BBox(cube.Pos{}, nil); len(got) != 1 || got[0].Max().Y() != 0.5 {
		t.Errorf("five layers collision %v, want 8/16 tall", got)
	}
}

func TestSnowLayerDrops(t *testing.T) {
	shovel := item.Shovel{Tier: item.ToolTierIron}
	silk := []item.Enchantment{item.NewEnchantment(enchantment.SilkTouch, 1)}
	info := func(h int) BreakInfo { return SnowLayer{Height: h}.BreakInfo() }
	if info(0).Harvestable(item.ToolNone{}) {
		t.Error("harvestable by hand")
	}
	if d := info(3).Drops(item.ToolNone{}, nil); len(d) != 0 {
		t.Errorf("hand drops %v", d)
	}
	if !info(0).Harvestable(shovel) {
		t.Error("not harvestable with a shovel")
	}
	for h := 0; h <= 7; h++ {
		d := info(h).Drops(shovel, nil)
		if len(d) != 1 || d[0].Count() != h+1 || d[0].Item() != (item.Snowball{}) {
			t.Errorf("height %d drops %v, want %d snowballs", h, d, h+1)
		}
	}
	if d := info(2).Drops(shovel, silk); len(d) != 1 || d[0].Count() != 3 || d[0].Item() != (SnowLayer{}) {
		t.Errorf("silk touch 3 layers: %v", d)
	}
	if d := info(7).Drops(shovel, silk); len(d) != 1 || d[0].Count() != 1 || d[0].Item() != (Snow{}) {
		t.Errorf("silk touch 8 layers: %v", d)
	}
}

func TestSnowLayerRegistered(t *testing.T) {
	it, ok := world.ItemByName("minecraft:snow_layer", 0)
	if !ok {
		t.Fatal("minecraft:snow_layer item not registered")
	}
	if _, ok := it.(SnowLayer); !ok {
		t.Fatalf("minecraft:snow_layer is %T", it)
	}
	for h := 0; h <= 7; h++ {
		b, ok := world.BlockByName("minecraft:snow_layer", map[string]any{"height": int32(h), "covered_bit": false})
		if !ok || b != (SnowLayer{Height: h}) {
			t.Errorf("height %d: %v %v", h, b, ok)
		}
	}
}
