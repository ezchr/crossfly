//go:build crossfly_full

package jfarm

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/item"
)

// Wool is the Sheep.DATA_WOOL_ID byte: the dye colour (0 white .. 15 black, DyeColor ids, the same on
// both editions and in item.Colour order) in the low 4 bits and 0x10 when sheared.
type Wool uint8

const woolSheared Wool = 0x10

// Colour is Sheep.getColor.
func (w Wool) Colour() item.Colour { return item.Colours()[w&0x0f] }

// Sheared is Sheep.isSheared.
func (w Wool) Sheared() bool { return w&woolSheared != 0 }

// WithColour is Sheep.setColor.
func (w Wool) WithColour(c item.Colour) Wool { return w&0xf0 | Wool(c.Uint8()&0x0f) }

// WithSheared is Sheep.setSheared.
func (w Wool) WithSheared(sheared bool) Wool {
	if sheared {
		return w | woolSheared
	}
	return w &^ woolSheared
}

// dyeMixes are the 26.3 crafting recipes made of exactly two dyes that give a dye
// (DyeColor.findColorMixInRecipes crafts a 2x1 grid of the parents' dyes in either order).
var dyeMixes = map[[2]uint8]uint8{}

func init() {
	c := func(f func() item.Colour) uint8 { return f().Uint8() }
	for _, m := range [][3]uint8{
		{c(item.ColourBlue), c(item.ColourGreen), c(item.ColourCyan)},      // cyan_dye
		{c(item.ColourBlack), c(item.ColourWhite), c(item.ColourGrey)},     // gray_dye
		{c(item.ColourBlue), c(item.ColourWhite), c(item.ColourLightBlue)}, // light_blue_dye_from_blue_white_dye
		{c(item.ColourGrey), c(item.ColourWhite), c(item.ColourLightGrey)}, // light_gray_dye_from_gray_white_dye
		{c(item.ColourGreen), c(item.ColourWhite), c(item.ColourLime)},     // lime_dye
		{c(item.ColourPurple), c(item.ColourPink), c(item.ColourMagenta)},  // magenta_dye_from_purple_and_pink
		{c(item.ColourRed), c(item.ColourYellow), c(item.ColourOrange)},    // orange_dye_from_red_yellow
		{c(item.ColourRed), c(item.ColourWhite), c(item.ColourPink)},       // pink_dye_from_red_white_dye
		{c(item.ColourBlue), c(item.ColourRed), c(item.ColourPurple)},      // purple_dye
	} {
		dyeMixes[[2]uint8{m[0], m[1]}] = m[2]
		dyeMixes[[2]uint8{m[1], m[0]}] = m[2]
	}
}

// MixedColour is DyeColor.getMixedColor, the wool colour of a lamb: the dye crafted from the parents'
// dyes, else one parent's colour at random.
func MixedColour(r *rand.Rand, a, b item.Colour) item.Colour {
	if m, ok := dyeMixes[[2]uint8{a.Uint8(), b.Uint8()}]; ok {
		return item.Colours()[m]
	}
	if r.IntN(2) == 0 {
		return a
	}
	return b
}

type weightedColour struct {
	colour func() item.Colour
	weight int
	// common: the 499 in 500 entry that is pink 1 time in 500 (SheepColorSpawnRules.commonColors).
	common bool
}

// sheepSpawnColours is SheepColorSpawnRules per climate: 5/5/5/3 rare colours and the common colour
// with weight 82, which itself is pink 1 time in 500.
var sheepSpawnColours = map[Climate][]weightedColour{
	Temperate: {{item.ColourBlack, 5, false}, {item.ColourGrey, 5, false}, {item.ColourLightGrey, 5, false},
		{item.ColourBrown, 3, false}, {item.ColourWhite, 82, true}},
	Warm: {{item.ColourGrey, 5, false}, {item.ColourLightGrey, 5, false}, {item.ColourWhite, 5, false},
		{item.ColourBlack, 3, false}, {item.ColourBrown, 82, true}},
	Cold: {{item.ColourLightGrey, 5, false}, {item.ColourGrey, 5, false}, {item.ColourWhite, 5, false},
		{item.ColourBrown, 3, false}, {item.ColourBlack, 82, true}},
}

// SheepSpawnColour is SheepColorSpawnRules.getSheepColor: the colour a sheep spawns with
// (Sheep.finalizeSpawn) in a biome of climate c (FarmAnimalClimate).
func SheepSpawnColour(r *rand.Rand, c Climate) item.Colour {
	list := sheepSpawnColours[c]
	if list == nil {
		list = sheepSpawnColours[Temperate]
	}
	n := r.IntN(100) // the weights add up to 100
	for _, w := range list {
		if n < w.weight {
			if w.common && r.IntN(500) == 0 {
				return item.ColourPink()
			}
			return w.colour()
		}
		n -= w.weight
	}
	return item.ColourWhite()
}

// ShearWoolCount is the loot table shearing/sheep/<colour>: 1 to 3 wool of the sheep's colour
// (uniform rolls, Sheep.shear drops each as its own item).
func ShearWoolCount(r *rand.Rand) int { return 1 + r.IntN(3) }

// SheepEatGrowSeconds is Sheep.ate: a lamb that eats grass grows by 60 seconds.
const SheepEatGrowSeconds = 60

// WoolDropMotion is the push Sheep.shear gives each wool item on top of the item's own spawn motion:
// x and z (r-r)*0.1, y r*0.05.
func WoolDropMotion(r *rand.Rand) (x, y, z float64) {
	f := func() float64 { return float64(r.Float32()) }
	x = (f() - f()) * 0.1
	y = f() * 0.05
	z = (f() - f()) * 0.1
	return
}
