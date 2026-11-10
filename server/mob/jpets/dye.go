//go:build crossfly_full

package jpets

import "math/rand/v2"

// Dye colour ids (DyeColor.getId), the same as Dragonfly's item.Colour order
// and Bedrock's: white 0 .. black 15.
const (
	DyeWhite = iota
	DyeOrange
	DyeMagenta
	DyeLightBlue
	DyeYellow
	DyeLime
	DyePink
	DyeGray
	DyeLightGray
	DyeCyan
	DyePurple
	DyeBlue
	DyeBrown
	DyeGreen
	DyeRed
	DyeBlack
)

// dyeMixes are the two-dye shapeless crafting recipes of 26.3
// (data/minecraft/recipe: cyan_dye, gray_dye, light_blue_dye_from_blue_white_dye,
// light_gray_dye_from_gray_white_dye, lime_dye, magenta_dye_from_purple_and_pink,
// orange_dye_from_red_yellow, pink_dye_from_red_white_dye, purple_dye).
var dyeMixes = map[[2]int]int{
	{DyeBlue, DyeGreen}:  DyeCyan,
	{DyeBlack, DyeWhite}: DyeGray,
	{DyeBlue, DyeWhite}:  DyeLightBlue,
	{DyeGray, DyeWhite}:  DyeLightGray,
	{DyeGreen, DyeWhite}: DyeLime,
	{DyePurple, DyePink}: DyeMagenta,
	{DyeRed, DyeYellow}:  DyeOrange,
	{DyeRed, DyeWhite}:   DyePink,
	{DyeBlue, DyeRed}:    DyePurple,
}

// MixDyes is DyeColor.getMixedColor: the dye a 2x1 crafting grid of the two
// dyes makes (shapeless recipes, so either order), else either colour at
// random (level.getRandom().nextBoolean()). Wolf and cat collars of bred
// babies use it.
func MixDyes(r *rand.Rand, a, b int) int {
	if c, ok := dyeMixes[[2]int{a, b}]; ok {
		return c
	}
	if c, ok := dyeMixes[[2]int{b, a}]; ok {
		return c
	}
	if r.IntN(2) == 0 {
		return a
	}
	return b
}

// dyeItems are the dye item names by colour id (the DYE component).
var dyeItems = [16]string{
	"minecraft:white_dye", "minecraft:orange_dye", "minecraft:magenta_dye", "minecraft:light_blue_dye",
	"minecraft:yellow_dye", "minecraft:lime_dye", "minecraft:pink_dye", "minecraft:gray_dye",
	"minecraft:light_gray_dye", "minecraft:cyan_dye", "minecraft:purple_dye", "minecraft:blue_dye",
	"minecraft:brown_dye", "minecraft:green_dye", "minecraft:red_dye", "minecraft:black_dye",
}

// DyeOfItem returns the colour of a dye item (#minecraft:dyes, the collar
// dyes of wolves and cats).
func DyeOfItem(name string) (int, bool) {
	for i, n := range dyeItems {
		if n == name {
			return i, true
		}
	}
	return 0, false
}
