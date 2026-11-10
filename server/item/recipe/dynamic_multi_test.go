package recipe_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

// grid returns a crafting grid of the size passed with the stacks passed in their slots.
func grid(size int, slots map[int]item.Stack) []recipe.Item {
	g := make([]recipe.Item, size)
	for i := range g {
		g[i] = item.Stack{}
	}
	for i, st := range slots {
		g[i] = st
	}
	return g
}

// one returns a stack of one of the item passed.
func one(it world.Item) item.Stack { return item.NewStack(it, 1) }

func dye(c item.Colour) item.Stack { return one(item.Dye{Colour: c}) }

// mustMatch matches a recipe and fails the test if the grid does not match.
func mustMatch(t *testing.T, r recipe.DynamicRecipe, g []recipe.Item) []item.Stack {
	t.Helper()
	out, ok := r.Match(g)
	if !ok {
		t.Fatalf("%T did not match grid %v", r, g)
	}
	return out
}

// noMatch fails the test if a recipe matches the grid.
func noMatch(t *testing.T, r recipe.DynamicRecipe, name string, g []recipe.Item) {
	t.Helper()
	if out, ok := r.Match(g); ok {
		t.Errorf("%s: %T matched with %v", name, r, out)
	}
}

func TestFireworkRocket(t *testing.T) {
	r := recipe.FireworkRecipe{}
	for gunpowder := 1; gunpowder <= 3; gunpowder++ {
		slots := map[int]item.Stack{0: one(item.Paper{})}
		for i := range gunpowder {
			slots[1+i] = one(item.Gunpowder{})
		}
		out := mustMatch(t, r, grid(4+5*(gunpowder/3), slots))
		if len(out) != 1 || out[0].Count() != 3 {
			t.Fatalf("rocket output %v, want 3 rockets", out)
		}
		fw, ok := out[0].Item().(item.Firework)
		if !ok {
			t.Fatalf("output %T, want firework", out[0].Item())
		}
		if fw.Duration != time.Duration(gunpowder+1)*500*time.Millisecond || len(fw.Explosions) != 0 {
			t.Errorf("rocket of %d gunpowder: %+v", gunpowder, fw)
		}
		fl := fw.EncodeNBT()["Fireworks"].(map[string]any)["Flight"]
		if fl != uint8(gunpowder) {
			t.Errorf("Flight %v, want %d", fl, gunpowder)
		}
	}

	star := item.FireworkStar{FireworkExplosion: item.FireworkExplosion{Shape: item.FireworkShapeStar(), Colour: item.ColourRed(), Trail: true}}
	out := mustMatch(t, r, grid(9, map[int]item.Stack{4: one(item.Paper{}), 0: one(item.Gunpowder{}), 8: one(star), 2: one(star)}))
	fw := out[0].Item().(item.Firework)
	if len(fw.Explosions) != 2 || !reflect.DeepEqual(fw.Explosions[0], star.FireworkExplosion) {
		t.Errorf("rocket explosions %+v, want two of %+v", fw.Explosions, star.FireworkExplosion)
	}
	if fw.EncodeNBT()["Fireworks"].(map[string]any)["Flight"] != uint8(1) {
		t.Error("rocket with stars: wrong flight")
	}

	noMatch(t, r, "4 gunpowder", grid(9, map[int]item.Stack{0: one(item.Paper{}), 1: one(item.Gunpowder{}), 2: one(item.Gunpowder{}), 3: one(item.Gunpowder{}), 4: one(item.Gunpowder{})}))
	noMatch(t, r, "2 paper", grid(4, map[int]item.Stack{0: one(item.Paper{}), 1: one(item.Paper{}), 2: one(item.Gunpowder{})}))
	noMatch(t, r, "paper only", grid(4, map[int]item.Stack{0: one(item.Paper{})}))
	noMatch(t, r, "paper, gunpowder and dye", grid(4, map[int]item.Stack{0: one(item.Paper{}), 1: one(item.Gunpowder{}), 2: dye(item.ColourRed())}))
	noMatch(t, r, "stranger", grid(4, map[int]item.Stack{0: one(item.Paper{}), 1: one(item.Gunpowder{}), 2: one(item.Stick{})}))
	noMatch(t, r, "empty", grid(9, nil))
}

func TestFireworkStar(t *testing.T) {
	r := recipe.FireworkRecipe{}
	out := mustMatch(t, r, grid(4, map[int]item.Stack{0: one(item.Gunpowder{}), 3: dye(item.ColourBlue())}))
	star, ok := out[0].Item().(item.FireworkStar)
	if !ok || out[0].Count() != 1 {
		t.Fatalf("star output %v", out)
	}
	if want := (item.FireworkExplosion{Shape: item.FireworkShapeSmallSphere(), Colour: item.ColourBlue()}); !reflect.DeepEqual(star.FireworkExplosion, want) {
		t.Errorf("plain star %+v, want %+v", star.FireworkExplosion, want)
	}

	shapes := map[string]struct {
		it    world.Item
		shape item.FireworkShape
	}{
		"fire charge": {item.FireCharge{}, item.FireworkShapeHugeSphere()},
		"gold nugget": {item.GoldNugget{}, item.FireworkShapeStar()},
		"feather":     {item.Feather{}, item.FireworkShapeBurst()},
		"head":        {block.Skull{Type: block.CreeperHead()}, item.FireworkShapeCreeperHead()},
		"skull":       {block.Skull{Type: block.WitherSkeletonSkull()}, item.FireworkShapeCreeperHead()},
	}
	for name, s := range shapes {
		out := mustMatch(t, r, grid(9, map[int]item.Stack{
			0: one(item.Gunpowder{}), 1: dye(item.ColourRed()), 2: dye(item.ColourLime()),
			3: one(s.it), 4: one(item.GlowstoneDust{}), 5: one(item.Diamond{}),
		}))
		ex := out[0].Item().(item.FireworkStar).FireworkExplosion
		if ex.Shape != s.shape || !ex.Twinkle || !ex.Trail || ex.Colour != item.ColourRed() ||
			!reflect.DeepEqual(ex.Colours, []item.Colour{item.ColourRed(), item.ColourLime()}) {
			t.Errorf("%s star: %+v", name, ex)
		}
	}

	noMatch(t, r, "no dye", grid(4, map[int]item.Stack{0: one(item.Gunpowder{}), 1: one(item.Diamond{})}))
	noMatch(t, r, "two shapes", grid(4, map[int]item.Stack{0: one(item.Gunpowder{}), 1: dye(item.ColourRed()), 2: one(item.Feather{}), 3: one(item.GoldNugget{})}))
	noMatch(t, r, "two gunpowder", grid(4, map[int]item.Stack{0: one(item.Gunpowder{}), 1: one(item.Gunpowder{}), 2: dye(item.ColourRed())}))
	noMatch(t, r, "star in star", grid(4, map[int]item.Stack{0: one(item.Gunpowder{}), 1: dye(item.ColourRed()), 2: one(item.FireworkStar{})}))
}

func TestFireworkStarFade(t *testing.T) {
	r := recipe.FireworkRecipe{}
	base := item.FireworkStar{FireworkExplosion: item.FireworkExplosion{Shape: item.FireworkShapeBurst(), Colour: item.ColourRed(), Twinkle: true}}
	out := mustMatch(t, r, grid(4, map[int]item.Stack{1: one(base), 2: dye(item.ColourWhite())}))
	ex := out[0].Item().(item.FireworkStar).FireworkExplosion
	if !ex.Fades || ex.Fade != item.ColourWhite() || len(ex.FadeColours) != 0 || ex.Shape != base.Shape || !ex.Twinkle || ex.Colour != base.Colour {
		t.Errorf("one fade: %+v", ex)
	}
	out = mustMatch(t, r, grid(4, map[int]item.Stack{0: one(base), 1: dye(item.ColourWhite()), 3: dye(item.ColourBlack())}))
	ex = out[0].Item().(item.FireworkStar).FireworkExplosion
	if !reflect.DeepEqual(ex.AllFades(), []item.Colour{item.ColourWhite(), item.ColourBlack()}) {
		t.Errorf("two fades: %+v", ex.AllFades())
	}
	noMatch(t, r, "fade with effect", grid(4, map[int]item.Stack{0: one(base), 1: dye(item.ColourWhite()), 2: one(item.Diamond{})}))
	noMatch(t, r, "two stars", grid(4, map[int]item.Stack{0: one(base), 1: one(base), 2: dye(item.ColourWhite())}))
	noMatch(t, r, "star alone", grid(4, map[int]item.Stack{0: one(base)}))
}

// TestFireworkExplosionNBT: explosions of several colours and fades survive NBT, both the byte arrays
// read from disk and the byte lists sent over the network, and one colour encodes as before.
func TestFireworkExplosionNBT(t *testing.T) {
	ex := item.FireworkExplosion{Shape: item.FireworkShapeStar(), Trail: true}.
		WithColours(item.ColourRed(), item.ColourBlue(), item.ColourYellow()).
		WithFades(item.ColourWhite(), item.ColourBlack())
	data := ex.EncodeNBT()
	if c, ok := data["FireworkColor"].([3]uint8); !ok || c != [3]uint8{1, 4, 11} {
		t.Errorf("FireworkColor %#v", data["FireworkColor"])
	}
	b, err := nbt.Marshal(item.FireworkStar{FireworkExplosion: ex}.EncodeNBT())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := nbt.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	got := item.FireworkStar{}.DecodeNBT(m).(item.FireworkStar).FireworkExplosion
	if !reflect.DeepEqual(got, ex) {
		t.Errorf("round trip:\n got %+v\nwant %+v", got, ex)
	}
	net := map[string]any{"FireworkType": uint8(2), "FireworkColor": []any{uint8(1), uint8(4)}, "FireworkFade": []any{}, "FireworkFlicker": uint8(0), "FireworkTrail": uint8(1)}
	if got := (item.FireworkExplosion{}).DecodeNBT(net).(item.FireworkExplosion); !reflect.DeepEqual(got.AllColours(), []item.Colour{item.ColourRed(), item.ColourBlue()}) || got.Fades {
		t.Errorf("network colours %+v", got)
	}
	single := item.FireworkExplosion{Colour: item.ColourRed(), Fade: item.ColourBlue(), Fades: true}.EncodeNBT()
	if single["FireworkColor"] != [1]uint8{1} || single["FireworkFade"] != [1]uint8{4} {
		t.Errorf("single colour encoding %#v %#v", single["FireworkColor"], single["FireworkFade"])
	}
}

func TestBookCloning(t *testing.T) {
	r := recipe.BookCloningRecipe{}
	book := item.WrittenBook{Title: "Rules", Author: "ezchr", Pages: []string{"one", "two"}, Generation: item.OriginalGeneration()}
	original := item.NewStack(book, 3).WithCustomName("Server rules")
	out := mustMatch(t, r, grid(9, map[int]item.Stack{4: original, 0: one(item.BookAndQuill{}), 8: one(item.BookAndQuill{})}))
	if len(out) != 2 {
		t.Fatalf("outputs %v, want copies and the original", out)
	}
	cp, ok := out[0].Item().(item.WrittenBook)
	if !ok || out[0].Count() != 2 || cp.Generation != item.CopyGeneration() || cp.Title != "Rules" || cp.Author != "ezchr" ||
		!reflect.DeepEqual(cp.Pages, book.Pages) || out[0].CustomName() != "Server rules" {
		t.Errorf("copies %v %+v", out[0], cp)
	}
	if out[1].Count() != 1 || !reflect.DeepEqual(out[1].Item(), book) || out[1].CustomName() != "Server rules" {
		t.Errorf("original back %v", out[1])
	}

	book.Generation = item.CopyGeneration()
	out = mustMatch(t, r, grid(4, map[int]item.Stack{0: one(book), 1: one(item.BookAndQuill{})}))
	if out[0].Item().(item.WrittenBook).Generation != item.CopyOfCopyGeneration() {
		t.Error("copy of a copy is not a copy of a copy")
	}

	book.Generation = item.CopyOfCopyGeneration()
	noMatch(t, r, "copy of copy", grid(4, map[int]item.Stack{0: one(book), 1: one(item.BookAndQuill{})}))
	book.Generation = item.OriginalGeneration()
	noMatch(t, r, "no blank", grid(4, map[int]item.Stack{0: one(book)}))
	noMatch(t, r, "two books", grid(4, map[int]item.Stack{0: one(book), 1: one(book), 2: one(item.BookAndQuill{})}))
	noMatch(t, r, "plain book", grid(4, map[int]item.Stack{0: one(book), 1: one(item.Book{})}))
}

func bannerStack(c item.Colour, layers ...block.BannerPatternLayer) item.Stack {
	return one(block.Banner{Colour: c, Patterns: layers})
}

func TestBannerDuplicate(t *testing.T) {
	r := recipe.BannerDuplicateRecipe{}
	layers := []block.BannerPatternLayer{{Type: block.CreeperBannerPattern(), Colour: item.ColourBlack()}, {Type: block.BorderBannerPattern(), Colour: item.ColourYellow()}}
	patterned := bannerStack(item.ColourRed(), layers...).WithCustomName("Team red")
	out := mustMatch(t, r, grid(4, map[int]item.Stack{1: patterned, 2: bannerStack(item.ColourRed())}))
	b, ok := out[0].Item().(block.Banner)
	if len(out) != 1 || !ok || out[0].Count() != 2 || b.Colour != item.ColourRed() || !reflect.DeepEqual(b.Patterns, layers) || out[0].CustomName() != "Team red" {
		t.Errorf("duplicate %v", out)
	}
	noMatch(t, r, "different colour", grid(4, map[int]item.Stack{0: patterned, 1: bannerStack(item.ColourBlue())}))
	noMatch(t, r, "two blank", grid(4, map[int]item.Stack{0: bannerStack(item.ColourRed()), 1: bannerStack(item.ColourRed())}))
	noMatch(t, r, "two patterned", grid(4, map[int]item.Stack{0: patterned, 1: patterned}))
	noMatch(t, r, "ominous", grid(4, map[int]item.Stack{0: one(block.Banner{Colour: item.ColourWhite(), Illager: true}), 1: bannerStack(item.ColourWhite())}))
	noMatch(t, r, "extra item", grid(4, map[int]item.Stack{0: patterned, 1: bannerStack(item.ColourRed()), 2: dye(item.ColourRed())}))
}

func TestBannerAddPattern(t *testing.T) {
	r := recipe.BannerAddPatternRecipe{}
	// Stripe bottom: dyes in the bottom row, banner anywhere else.
	out := mustMatch(t, r, grid(9, map[int]item.Stack{4: bannerStack(item.ColourWhite()), 6: dye(item.ColourBlue()), 7: dye(item.ColourBlue()), 8: dye(item.ColourBlue())}))
	b := out[0].Item().(block.Banner)
	want := []block.BannerPatternLayer{{Type: block.StripeBottomBannerPattern(), Colour: item.ColourBlue()}}
	if out[0].Count() != 1 || b.Colour != item.ColourWhite() || !reflect.DeepEqual(b.Patterns, want) {
		t.Errorf("stripe bottom: %+v", b)
	}

	// Border on top of an existing layer: dyes around the banner in the middle.
	existing := block.BannerPatternLayer{Type: block.CrossBannerPattern(), Colour: item.ColourRed()}
	slots := map[int]item.Stack{4: bannerStack(item.ColourBlack(), existing)}
	for _, i := range []int{0, 1, 2, 3, 5, 6, 7, 8} {
		slots[i] = dye(item.ColourYellow())
	}
	b = mustMatch(t, r, grid(9, slots))[0].Item().(block.Banner)
	if want := []block.BannerPatternLayer{existing, {Type: block.BorderBannerPattern(), Colour: item.ColourYellow()}}; !reflect.DeepEqual(b.Patterns, want) {
		t.Errorf("border: %+v", b.Patterns)
	}

	ingredients := map[string]struct {
		it      world.Item
		pattern block.BannerPatternType
	}{
		"bricks":  {block.Bricks{}, block.BricksBannerPattern()},
		"creeper": {block.Skull{Type: block.CreeperHead()}, block.CreeperBannerPattern()},
		"skull":   {block.Skull{Type: block.WitherSkeletonSkull()}, block.SkullBannerPattern()},
		"vines":   {block.Vines{}, block.CurlyBorderBannerPattern()},
		"flower":  {block.Flower{Type: block.OxeyeDaisy()}, block.FlowerBannerPattern()},
		"mojang":  {item.EnchantedApple{}, block.MojangBannerPattern()},
	}
	for name, in := range ingredients {
		out, ok := r.Match(grid(4, map[int]item.Stack{0: one(in.it), 3: bannerStack(item.ColourWhite()), 2: dye(item.ColourPurple())}))
		if !ok {
			t.Errorf("%s: no match", name)
			continue
		}
		if b := out[0].Item().(block.Banner); len(b.Patterns) != 1 || b.Patterns[0].Type != in.pattern || b.Patterns[0].Colour != item.ColourPurple() {
			t.Errorf("%s: %+v", name, b.Patterns)
		}
	}

	noMatch(t, r, "two dye colours", grid(9, map[int]item.Stack{4: bannerStack(item.ColourWhite()), 6: dye(item.ColourBlue()), 7: dye(item.ColourRed()), 8: dye(item.ColourBlue())}))
	noMatch(t, r, "shape in 2x2", grid(4, map[int]item.Stack{0: bannerStack(item.ColourWhite()), 1: dye(item.ColourBlue())}))
	noMatch(t, r, "no shape", grid(9, map[int]item.Stack{0: bannerStack(item.ColourWhite()), 4: dye(item.ColourBlue()), 8: dye(item.ColourBlue())}))
	noMatch(t, r, "banner on a dye spot", grid(9, map[int]item.Stack{6: bannerStack(item.ColourWhite()), 7: dye(item.ColourBlue()), 8: dye(item.ColourBlue())}))
	noMatch(t, r, "ingredient with two dyes", grid(4, map[int]item.Stack{0: one(block.Bricks{}), 1: bannerStack(item.ColourWhite()), 2: dye(item.ColourRed()), 3: dye(item.ColourRed())}))
	noMatch(t, r, "two banners", grid(4, map[int]item.Stack{0: one(block.Bricks{}), 1: bannerStack(item.ColourWhite()), 2: dye(item.ColourRed()), 3: bannerStack(item.ColourWhite())}))
	noMatch(t, r, "no banner", grid(9, map[int]item.Stack{6: dye(item.ColourBlue()), 7: dye(item.ColourBlue()), 8: dye(item.ColourBlue())}))
	full := make([]block.BannerPatternLayer, 6)
	for i := range full {
		full[i] = existing
	}
	noMatch(t, r, "six patterns", grid(4, map[int]item.Stack{0: one(block.Bricks{}), 1: bannerStack(item.ColourWhite(), full...), 2: dye(item.ColourRed())}))
}

func TestRepairItem(t *testing.T) {
	r := recipe.RepairItemRecipe{}
	sword := item.Sword{Tier: item.ToolTierDiamond}
	maxD := one(sword).MaxDurability()
	a := one(sword).WithDurability(100).WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 5), item.NewEnchantment(enchantment.CurseOfVanishing, 1))
	b := one(sword).WithDurability(200).WithCustomName("Sword").WithEnchantments(item.NewEnchantment(enchantment.Unbreaking, 3))
	out := mustMatch(t, r, grid(4, map[int]item.Stack{0: a, 3: b}))
	if len(out) != 1 || out[0].Count() != 1 || out[0].Durability() != 300+maxD*5/100 || out[0].CustomName() != "" {
		t.Errorf("repaired %v, durability %d, want %d", out, out[0].Durability(), 300+maxD*5/100)
	}
	if e := out[0].Enchantments(); len(e) != 1 || e[0].Type() != enchantment.CurseOfVanishing {
		t.Errorf("enchantments %v, want only the curse", e)
	}
	nearlyNew := mustMatch(t, r, grid(4, map[int]item.Stack{0: one(sword).WithDurability(maxD - 1), 1: one(sword).WithDurability(maxD - 1)}))
	if nearlyNew[0].Durability() != maxD {
		t.Errorf("durability %d, want capped at %d", nearlyNew[0].Durability(), maxD)
	}
	noMatch(t, r, "different items", grid(4, map[int]item.Stack{0: a, 1: one(item.Sword{Tier: item.ToolTierIron})}))
	noMatch(t, r, "three", grid(4, map[int]item.Stack{0: a, 1: b, 2: one(sword)}))
	noMatch(t, r, "one", grid(4, map[int]item.Stack{0: a}))
	noMatch(t, r, "not durable", grid(4, map[int]item.Stack{0: one(item.Stick{}), 1: one(item.Stick{})}))
	noMatch(t, r, "stacked", grid(4, map[int]item.Stack{0: item.NewStack(item.Stick{}, 2), 1: a}))
}
