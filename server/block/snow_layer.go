package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// SnowLayer is a thin layer of snow, one to eight layers of 2/16 of a block each. It covers the ground of snowy
// biomes and melts near bright light.
type SnowLayer struct {
	transparent

	// Height is the number of layers above the first: 0 is one layer, 7 is eight layers. Eight layers stay a snow
	// layer and do not become a snow block.
	Height int
	// Covered is Bedrock's covered_bit: set when the layer was placed into a block such as short grass, which
	// Bedrock keeps in the second block layer. Dragonfly does not set it and keeps it only for worlds that have it.
	Covered bool
}

// Model ...
func (s SnowLayer) Model() world.BlockModel {
	return model.SnowLayer{Height: s.Height}
}

// ReplaceableBy returns true for a single layer, unless the block placed is snow layer: that adds a layer instead.
func (s SnowLayer) ReplaceableBy(b world.Block) bool {
	if _, ok := b.(SnowLayer); ok {
		return false
	}
	return s.Height == 0
}

// canSurvive checks if a snow layer can stay at pos: the block below must have a full solid top face, or be eight
// layers of snow, and must not be ice, packed ice or a barrier. Soul sand, mud and leaves always hold it.
func (SnowLayer) canSurvive(pos cube.Pos, tx *world.Tx) bool {
	below := pos.Side(cube.FaceDown)
	switch b := tx.Block(below).(type) {
	case Ice, PackedIce, Barrier:
		return false
	case SoulSand, Mud, Leaves:
		return true
	case SnowLayer:
		return b.Height == 7
	default:
		return b.Model().FaceSolid(below, cube.FaceUp, tx)
	}
}

// UseOnBlock adds a layer to the snow layer clicked on its top face, or to one next to the face clicked, up to
// eight layers. Otherwise it places a new layer, if the block below can hold it.
func (s SnowLayer) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	if existing, ok := tx.Block(pos).(SnowLayer); ok && face == cube.FaceUp && existing.Height < 7 {
		existing.Height++
		place(tx, pos, existing, user, ctx)
		return placed(ctx)
	}
	if side := pos.Side(face); !side.OutOfBounds(tx.Range()) {
		if existing, ok := tx.Block(side).(SnowLayer); ok {
			if existing.Height >= 7 {
				return false
			}
			existing.Height++
			place(tx, side, existing, user, ctx)
			return placed(ctx)
		}
	}
	pos, _, used := firstReplaceable(tx, pos, face, s)
	if !used || !s.canSurvive(pos, tx) {
		return false
	}
	place(tx, pos, SnowLayer{}, user, ctx)
	return placed(ctx)
}

// NeighbourUpdateTick removes the snow layer, without drops, once the block below no longer holds it.
func (s SnowLayer) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !s.canSurvive(pos, tx) {
		breakBlockNoDrops(s, pos, tx)
	}
}

// RandomTick melts the snow layer, without drops, when the block light at it is above 11.
func (s SnowLayer) RandomTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if tx.BlockLight(pos) > 11 {
		breakBlockNoDrops(s, pos, tx)
	}
}

// HasLiquidDrops returns false: flowing water washes snow layers away without drops.
func (SnowLayer) HasLiquidDrops() bool {
	return false
}

// SideClosed ...
func (SnowLayer) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// BreakInfo ...
func (s SnowLayer) BreakInfo() BreakInfo {
	return newBreakInfo(0.1, shovelEffective, shovelEffective, s.drops)
}

// drops returns a snowball per layer when broken with a shovel. With silk touch it returns a snow layer item per
// layer, or one snow block for eight layers. Without a shovel it drops nothing.
func (s SnowLayer) drops(t item.Tool, enchantments []item.Enchantment) []item.Stack {
	if t.ToolType() != item.TypeShovel {
		return nil
	}
	n := s.Height + 1
	if hasSilkTouch(enchantments) {
		if n == 8 {
			return []item.Stack{item.NewStack(Snow{}, 1)}
		}
		return []item.Stack{item.NewStack(SnowLayer{}, n)}
	}
	return []item.Stack{item.NewStack(item.Snowball{}, n)}
}

// EncodeItem ...
func (SnowLayer) EncodeItem() (name string, meta int16) {
	return "minecraft:snow_layer", 0
}

// EncodeBlock ...
func (s SnowLayer) EncodeBlock() (string, map[string]any) {
	return "minecraft:snow_layer", map[string]any{"height": int32(s.Height), "covered_bit": s.Covered}
}

// allSnowLayers returns every snow layer state.
func allSnowLayers() (b []world.Block) {
	for h := 0; h <= 7; h++ {
		b = append(b, SnowLayer{Height: h}, SnowLayer{Height: h, Covered: true})
	}
	return
}
