package recipe

import (
	"slices"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

// The IDs of the vanilla multi recipes below, as hardcoded in the client and in BDS (<Name>Recipe::ID).
var (
	repairItemRecipeID       = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	fireworkRecipeID         = uuid.MustParse("00000000-0000-0000-0000-000000000002")
	bookCloningRecipeID      = uuid.MustParse("d1ca6b84-338e-4f2f-9c6b-76cc8b4bd98d")
	bannerDuplicateRecipeID  = uuid.MustParse("b5c5d105-75a2-4076-af2b-923ea2bf4bf0")
	bannerAddPatternRecipeID = uuid.MustParse("d81aaeaf-e172-4440-9225-868df030d27b")
)

// gridStacks returns the item stacks in a crafting grid that are not empty. It returns false if the
// grid holds anything other than item stacks.
func gridStacks(input []Item) ([]item.Stack, bool) {
	stacks := make([]item.Stack, 0, len(input))
	for _, in := range input {
		if in == nil || in.Empty() {
			continue
		}
		st, ok := in.(item.Stack)
		if !ok {
			return nil, false
		}
		stacks = append(stacks, st)
	}
	return stacks, true
}

// itemName returns the name an item is saved with.
func itemName(st item.Stack) string {
	name, _ := st.Item().EncodeItem()
	return name
}

// FireworkRecipe crafts firework rockets, firework stars and adds fade colours to firework stars
// (BDS FireworksRecipe):
//   - a rocket: one paper, one to three gunpowder and any number of firework stars gives three rockets
//     that fly for as long as the gunpowder count and explode like the stars;
//   - a star: one gunpowder, at least one dye, at most one shape item (fire charge, gold nugget, any
//     head, feather) and any number of glowstone dust (twinkle) and diamonds (trail);
//   - a fading star: one firework star and at least one dye gives the star fading into the dyes.
type FireworkRecipe struct{}

// Match ...
func (FireworkRecipe) Match(input []Item) ([]item.Stack, bool) {
	stacks, ok := gridStacks(input)
	if !ok {
		return nil, false
	}
	var (
		gunpowder, paper, shapes, effects int
		twinkle, trail                    bool
		shape                             = item.FireworkShapeSmallSphere()
		stars                             []item.FireworkStar
		dyes                              []item.Colour
	)
	for _, st := range stacks {
		switch it := st.Item().(type) {
		case item.Gunpowder:
			gunpowder++
		case item.FireworkStar:
			stars = append(stars, it)
		case item.Dye:
			dyes = append(dyes, it.Colour)
		case item.Paper:
			paper++
		case item.GlowstoneDust:
			twinkle, effects = true, effects+1
		case item.Diamond:
			trail, effects = true, effects+1
		case item.FireCharge:
			shape, shapes = item.FireworkShapeHugeSphere(), shapes+1
		case item.GoldNugget:
			shape, shapes = item.FireworkShapeStar(), shapes+1
		case item.Feather:
			shape, shapes = item.FireworkShapeBurst(), shapes+1
		default:
			if !isHead(st) {
				return nil, false
			}
			shape, shapes = item.FireworkShapeCreeperHead(), shapes+1
		}
	}
	if gunpowder > 3 || paper > 1 || shapes > 1 {
		return nil, false
	}
	switch {
	case gunpowder > 0 && paper == 1 && len(dyes)+effects+shapes == 0:
		explosions := make([]item.FireworkExplosion, 0, len(stars))
		for _, s := range stars {
			explosions = append(explosions, s.FireworkExplosion)
		}
		// Firework keeps its Flight byte n as a Duration of (n+1)*500ms.
		rocket := item.Firework{Duration: time.Duration(gunpowder+1) * 500 * time.Millisecond, Explosions: explosions}
		return []item.Stack{item.NewStack(rocket, 3)}, true
	case gunpowder == 1 && paper == 0 && len(stars) == 0 && len(dyes) > 0:
		explosion := item.FireworkExplosion{Shape: shape, Twinkle: twinkle, Trail: trail}.WithColours(dyes...)
		return []item.Stack{item.NewStack(item.FireworkStar{FireworkExplosion: explosion}, 1)}, true
	case gunpowder == 0 && paper == 0 && len(stars) == 1 && len(dyes) > 0 && effects+shapes == 0:
		star := stars[0]
		star.FireworkExplosion = star.FireworkExplosion.WithFades(dyes...)
		return []item.Stack{item.NewStack(star, 1)}, true
	}
	return nil, false
}

// isHead checks if a stack holds a mob head or skull, which gives a firework star the creeper shape.
func isHead(st item.Stack) bool {
	name := itemName(st)
	return strings.HasSuffix(name, "_head") || strings.HasSuffix(name, "_skull") || name == "minecraft:skull"
}

// Block ...
func (FireworkRecipe) Block() string { return "crafting_table" }

// UUID ...
func (FireworkRecipe) UUID() uuid.UUID { return fireworkRecipeID }

// BookCloningRecipe copies a written book into books and quills (BDS BookCloningRecipe): one written
// book that is an original or a copy, and at least one book and quill. It gives a copy for each book and
// quill, one generation further and with the same custom name, and the written book back.
type BookCloningRecipe struct{}

// Match ...
func (BookCloningRecipe) Match(input []Item) ([]item.Stack, bool) {
	stacks, ok := gridStacks(input)
	if !ok {
		return nil, false
	}
	var (
		original item.Stack
		book     item.WrittenBook
		found    bool
		blanks   int
	)
	for _, st := range stacks {
		switch it := st.Item().(type) {
		case item.WrittenBook:
			if found || it.Generation.Uint8() >= item.CopyOfCopyGeneration().Uint8() {
				return nil, false
			}
			original, book, found = st, it, true
		case item.BookAndQuill:
			blanks++
		default:
			return nil, false
		}
	}
	if !found || blanks == 0 {
		return nil, false
	}
	cp := book
	cp.Pages = slices.Clone(book.Pages)
	if book.Generation == item.OriginalGeneration() {
		cp.Generation = item.CopyGeneration()
	} else {
		cp.Generation = item.CopyOfCopyGeneration()
	}
	copies := item.NewStack(cp, blanks)
	if name := original.CustomName(); name != "" {
		copies = copies.WithCustomName(name)
	}
	return []item.Stack{copies, original.Grow(1 - original.Count())}, true
}

// Block ...
func (BookCloningRecipe) Block() string { return "crafting_table" }

// UUID ...
func (BookCloningRecipe) UUID() uuid.UUID { return bookCloningRecipeID }

// maxBannerPatterns is the most patterns a banner can get in a crafting table (BannerBlockActor::MAX_PATTERNS).
const maxBannerPatterns = 6

// bannerItem is a banner item: the block package's Banner, which cannot be imported here.
type bannerItem interface {
	world.Item
	world.NBTer
}

// banner returns the banner in a stack and its NBT, if the stack holds a banner.
func banner(st item.Stack) (bannerItem, map[string]any, bool) {
	b, ok := st.Item().(bannerItem)
	if !ok || itemName(st) != "minecraft:banner" {
		return nil, nil, false
	}
	return b, b.EncodeNBT(), true
}

// defaultBanner checks if banner NBT is that of a normal banner, not an ominous one.
func defaultBanner(data map[string]any) bool {
	t, _ := data["Type"].(int32)
	return t != 1
}

// bannerPatterns returns the pattern layers in banner NBT.
func bannerPatterns(data map[string]any) []any {
	p, _ := data["Patterns"].([]any)
	return p
}

// BannerDuplicateRecipe copies the patterns of a banner onto a blank banner of the same colour (BDS
// BannerDuplicateRecipe), giving two of the patterned banner.
type BannerDuplicateRecipe struct{}

// Match ...
func (BannerDuplicateRecipe) Match(input []Item) ([]item.Stack, bool) {
	stacks, ok := gridStacks(input)
	if !ok || len(stacks) != 2 {
		return nil, false
	}
	var patterned, blank item.Stack
	for _, st := range stacks {
		_, data, ok := banner(st)
		if !ok || !defaultBanner(data) {
			return nil, false
		}
		if len(bannerPatterns(data)) > 0 {
			if !patterned.Empty() {
				return nil, false
			}
			patterned = st
		} else {
			if !blank.Empty() {
				return nil, false
			}
			blank = st
		}
	}
	if patterned.Empty() || blank.Empty() {
		return nil, false
	}
	// The meta of a banner item is its base colour.
	_, patternedColour := patterned.Item().EncodeItem()
	_, blankColour := blank.Item().EncodeItem()
	if patternedColour != blankColour {
		return nil, false
	}
	return []item.Stack{patterned.Grow(2 - patterned.Count())}, true
}

// Block ...
func (BannerDuplicateRecipe) Block() string { return "crafting_table" }

// UUID ...
func (BannerDuplicateRecipe) UUID() uuid.UUID { return bannerDuplicateRecipeID }

// bannerCraftPattern is a banner pattern that a crafting table can add to a banner: either a shape of dyes
// in a 3x3 grid, or a dye with an ingredient item.
type bannerCraftPattern struct {
	id         string
	shape      [3]string
	ingredient func(name string) bool
}

// bannerCraftPatterns are the banner patterns a crafting table adds, in the order BDS matches them
// (BannerPattern::mPatterns). The loom-only patterns (illager, piglin, globe, flow, guster) are missing.
var bannerCraftPatterns = []bannerCraftPattern{
	{id: "bo", shape: [3]string{"###", "# #", "###"}},
	{id: "bri", ingredient: isName("minecraft:brick_block")},
	{id: "mc", shape: [3]string{"   ", " # ", "   "}},
	{id: "cre", ingredient: isName("minecraft:creeper_head")},
	{id: "sku", ingredient: isName("minecraft:wither_skeleton_skull")},
	{id: "cr", shape: [3]string{"# #", " # ", "# #"}},
	{id: "cbo", ingredient: isName("minecraft:vine")},
	{id: "ld", shape: [3]string{"## ", "#  ", "   "}},
	{id: "rud", shape: [3]string{" ##", "  #", "   "}},
	{id: "lud", shape: [3]string{"   ", "#  ", "## "}},
	{id: "rd", shape: [3]string{"   ", "  #", " ##"}},
	{id: "flo", ingredient: isName("minecraft:oxeye_daisy")},
	{id: "gra", shape: [3]string{"# #", " # ", " # "}},
	{id: "gru", shape: [3]string{" # ", " # ", "# #"}},
	{id: "hh", shape: [3]string{"###", "###", "   "}},
	{id: "hhb", shape: [3]string{"   ", "###", "###"}},
	{id: "vh", shape: [3]string{"## ", "## ", "## "}},
	{id: "vhr", shape: [3]string{" ##", " ##", " ##"}},
	{id: "moj", ingredient: isName("minecraft:enchanted_golden_apple")},
	{id: "mr", shape: [3]string{" # ", "# #", " # "}},
	{id: "ss", shape: [3]string{"# #", "# #", "   "}},
	{id: "bl", shape: [3]string{"   ", "   ", "#  "}},
	{id: "br", shape: [3]string{"   ", "   ", "  #"}},
	{id: "tl", shape: [3]string{"#  ", "   ", "   "}},
	{id: "tr", shape: [3]string{"  #", "   ", "   "}},
	{id: "sc", shape: [3]string{" # ", "###", " # "}},
	{id: "bs", shape: [3]string{"   ", "   ", "###"}},
	{id: "cs", shape: [3]string{" # ", " # ", " # "}},
	{id: "dls", shape: [3]string{"  #", " # ", "#  "}},
	{id: "drs", shape: [3]string{"#  ", " # ", "  #"}},
	{id: "ls", shape: [3]string{"#  ", "#  ", "#  "}},
	{id: "ms", shape: [3]string{"   ", "###", "   "}},
	{id: "rs", shape: [3]string{"  #", "  #", "  #"}},
	{id: "ts", shape: [3]string{"###", "   ", "   "}},
	{id: "bt", shape: [3]string{"   ", " # ", "# #"}},
	{id: "tt", shape: [3]string{"# #", " # ", "   "}},
	{id: "bts", shape: [3]string{"   ", "# #", " # "}},
	{id: "tts", shape: [3]string{" # ", "# #", "   "}},
}

// isName returns a function that checks if an item name is the name passed.
func isName(want string) func(string) bool {
	return func(name string) bool { return name == want }
}

// match checks if the crafting grid passed, with the banner at bannerSlot, makes this pattern. It
// returns the colour of the dye(s) used.
func (p bannerCraftPattern) match(input []Item, bannerSlot int) (item.Colour, bool) {
	var (
		colour item.Colour
		dyes   int
	)
	if p.ingredient != nil {
		ingredients := 0
		for i, in := range input {
			if i == bannerSlot || in == nil || in.Empty() {
				continue
			}
			st := in.(item.Stack)
			if dye, ok := st.Item().(item.Dye); ok {
				colour, dyes = dye.Colour, dyes+1
				continue
			}
			if !p.ingredient(itemName(st)) {
				return colour, false
			}
			ingredients++
		}
		return colour, dyes == 1 && ingredients == 1
	}
	if len(input) != 9 {
		return colour, false
	}
	for i, in := range input {
		hash := p.shape[i/3][i%3] == '#'
		if i == bannerSlot || in == nil || in.Empty() {
			if hash {
				return colour, false
			}
			continue
		}
		dye, ok := in.(item.Stack).Item().(item.Dye)
		if !ok || !hash || (dyes > 0 && dye.Colour != colour) {
			return colour, false
		}
		colour, dyes = dye.Colour, dyes+1
	}
	return colour, dyes > 0
}

// BannerAddPatternRecipe adds a pattern to a banner in a crafting table (BDS BannerAddPatternRecipe):
// one banner with fewer than six patterns, and either dyes of one colour in the shape of a pattern in a
// 3x3 grid, or one dye with the pattern's ingredient (bricks, a creeper head, a wither skeleton skull,
// vines, an oxeye daisy or an enchanted golden apple) anywhere.
type BannerAddPatternRecipe struct{}

// Match ...
func (BannerAddPatternRecipe) Match(input []Item) ([]item.Stack, bool) {
	if _, ok := gridStacks(input); !ok {
		return nil, false
	}
	bannerSlot := -1
	var (
		st   item.Stack
		b    bannerItem
		data map[string]any
	)
	for i, in := range input {
		if in == nil || in.Empty() {
			continue
		}
		s := in.(item.Stack)
		bi, d, ok := banner(s)
		if !ok {
			continue
		}
		if bannerSlot != -1 || !defaultBanner(d) || len(bannerPatterns(d)) >= maxBannerPatterns {
			return nil, false
		}
		bannerSlot, st, b, data = i, s, bi, d
	}
	if bannerSlot == -1 {
		return nil, false
	}
	for _, p := range bannerCraftPatterns {
		colour, ok := p.match(input, bannerSlot)
		if !ok {
			continue
		}
		patterns := append(slices.Clone(bannerPatterns(data)), map[string]any{
			"Pattern": p.id,
			"Color":   int32(^int16(colour.Uint8()) & 0xf),
		})
		data["Patterns"] = patterns
		decoded, ok := b.DecodeNBT(data).(world.Item)
		if !ok {
			return nil, false
		}
		return []item.Stack{st.Grow(1 - st.Count()).WithItem(decoded)}, true
	}
	return nil, false
}

// Block ...
func (BannerAddPatternRecipe) Block() string { return "crafting_table" }

// UUID ...
func (BannerAddPatternRecipe) UUID() uuid.UUID { return bannerAddPatternRecipeID }

// RepairItemRecipe combines two damaged items of the same kind into one (BDS RepairItemRecipe): it gets
// the durability of both plus 5% of the maximum, and only the curses of the two items.
type RepairItemRecipe struct{}

// Match ...
func (RepairItemRecipe) Match(input []Item) ([]item.Stack, bool) {
	stacks, ok := gridStacks(input)
	if !ok || len(stacks) != 2 {
		return nil, false
	}
	a, b := stacks[0], stacks[1]
	if itemName(a) != itemName(b) || a.Count() != 1 || b.Count() != 1 {
		return nil, false
	}
	maxDurability := a.MaxDurability()
	if maxDurability <= 0 {
		return nil, false
	}
	durability := min(maxDurability, a.Durability()+b.Durability()+maxDurability*5/100)
	out := item.NewStack(a.Item(), 1).WithDurability(durability)

	var curses []item.Enchantment
	for _, st := range stacks {
		for _, e := range st.Enchantments() {
			if c, ok := e.Type().(interface{ Curse() bool }); !ok || !c.Curse() {
				continue
			}
			if i := slices.IndexFunc(curses, func(o item.Enchantment) bool { return o.Type() == e.Type() }); i >= 0 {
				if e.Level() > curses[i].Level() {
					curses[i] = e
				}
				continue
			}
			curses = append(curses, e)
		}
	}
	return []item.Stack{out.WithEnchantments(curses...)}, true
}

// Block ...
func (RepairItemRecipe) Block() string { return "crafting_table" }

// UUID ...
func (RepairItemRecipe) UUID() uuid.UUID { return repairItemRecipeID }
