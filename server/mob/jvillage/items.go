//go:build crossfly_full

package jvillage

import (
	"image/color"
	"sync"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/java/javamap"
	jitem "github.com/df-mc/dragonfly/server/java/protocol/item"
	"github.com/df-mc/dragonfly/server/world"
)

// javaItems maps Java item ids to the Dragonfly item the Java client shows
// for them (the reverse of javamap.ItemID, as javasession builds it).
var javaItems = sync.OnceValue(func() map[int32]world.Item {
	type key struct {
		meta int16
		name string
	}
	m := map[int32]world.Item{}
	keys := map[int32]key{}
	for _, it := range world.Items() {
		name, meta := it.EncodeItem()
		id, ok := javamap.ItemID(name, meta)
		if !ok {
			continue
		}
		if prev, ok := keys[id]; ok && (prev.meta < meta || prev.meta == meta && prev.name <= name) {
			continue
		}
		m[id], keys[id] = it, key{meta, name}
	}
	return m
})

// JavaItem returns the Dragonfly item of a Java item name ("emerald" or
// "minecraft:emerald").
func JavaItem(name string) (world.Item, bool) {
	id, ok := jitem.ByName("minecraft:" + trimNS(name))
	if !ok {
		return nil, false
	}
	it, ok := javaItems()[id]
	return it, ok
}

// JavaName returns the Java item name (without namespace) of a Dragonfly
// item, if it has one.
func JavaName(it world.Item) (string, bool) {
	name, meta := it.EncodeItem()
	id, ok := javamap.ItemID(name, meta)
	if !ok {
		return "", false
	}
	n := jitem.Name(id)
	return trimNS(n), n != ""
}

// potionNames are the Java potion names by Bedrock potion id
// (potion.From). Bedrock's long mundane and decay have no Java potion.
var potionNames = [...]string{
	"water", "mundane", "", "thick", "awkward", "night_vision", "long_night_vision", "invisibility", "long_invisibility",
	"leaping", "long_leaping", "strong_leaping", "fire_resistance", "long_fire_resistance", "swiftness", "long_swiftness",
	"strong_swiftness", "slowness", "long_slowness", "water_breathing", "long_water_breathing", "healing", "strong_healing",
	"harming", "strong_harming", "poison", "long_poison", "strong_poison", "regeneration", "long_regeneration",
	"strong_regeneration", "strength", "long_strength", "strong_strength", "weakness", "long_weakness", "",
	"turtle_master", "long_turtle_master", "strong_turtle_master", "slow_falling", "long_slow_falling", "strong_slowness",
	"wind_charged", "weaving", "oozing", "infested",
}

// JavaPotion returns the Dragonfly potion of a Java potion name.
func JavaPotion(name string) (potion.Potion, bool) {
	name = trimNS(name)
	for id, n := range potionNames {
		if n == name && n != "" {
			return potion.From(int32(id)), true
		}
	}
	return potion.Potion{}, false
}

// enchantmentNames are the Java names of Dragonfly's (Bedrock) enchantment
// ids.
var enchantmentNames = [...]string{
	0: "protection", 1: "fire_protection", 2: "feather_falling", 3: "blast_protection",
	4: "projectile_protection", 5: "thorns", 6: "respiration", 7: "depth_strider", 8: "aqua_affinity",
	9: "sharpness", 10: "smite", 11: "bane_of_arthropods", 12: "knockback", 13: "fire_aspect",
	14: "looting", 15: "efficiency", 16: "silk_touch", 17: "unbreaking", 18: "fortune", 19: "power",
	20: "punch", 21: "flame", 22: "infinity", 23: "luck_of_the_sea", 24: "lure", 25: "frost_walker",
	26: "mending", 27: "binding_curse", 28: "vanishing_curse", 29: "impaling", 30: "riptide",
	31: "loyalty", 32: "channeling", 33: "multishot", 34: "piercing", 35: "quick_charge",
	36: "soul_speed", 37: "swift_sneak", 38: "wind_burst", 39: "density", 40: "breach", 41: "lunge",
}

// JavaEnchantment returns the Dragonfly enchantment type of a Java
// enchantment name. Sweeping edge has no Bedrock counterpart.
func JavaEnchantment(name string) (item.EnchantmentType, bool) {
	name = trimNS(name)
	for id, n := range enchantmentNames {
		if n == name {
			return item.EnchantmentByID(id)
		}
	}
	return nil, false
}

// dyeColours are DyeColor.getTextureDiffuseColor, in DyeColor order.
var dyeColours = [16]uint32{
	0xF9FFFE, 0xF9801D, 0xC74EBD, 0x3AB3DA, 0xFED83D, 0x80C71F, 0xF38BAA, 0x474F52,
	0x9D9D97, 0x169C9C, 0x8932B8, 0x3C44AA, 0x835432, 0x5E7C16, 0xB02E26, 0x1D1D21,
}

// applyDyes is DyedItemColor.applyDyes for an undyed item: the average of
// the dye colours scaled to their average brightness.
func applyDyes(dyes []int) color.RGBA {
	var r, g, b, intensity, n int
	for _, d := range dyes {
		c := dyeColours[d]
		cr, cg, cb := int(c>>16&0xFF), int(c>>8&0xFF), int(c&0xFF)
		intensity += max(cr, cg, cb)
		r, g, b = r+cr, g+cg, b+cb
		n++
	}
	r, g, b = r/n, g/n, b/n
	avg := float32(intensity) / float32(n)
	mx := float32(max(r, g, b))
	r = int(float32(r) * avg / mx)
	g = int(float32(g) * avg / mx)
	b = int(float32(b) * avg / mx)
	return color.RGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 0xFF}
}

// withLeatherColour dyes a leather armour piece.
func withLeatherColour(it world.Item, c color.RGBA) world.Item {
	t := item.ArmourTierLeather{Colour: c}
	switch v := it.(type) {
	case item.Helmet:
		v.Tier = t
		return v
	case item.Chestplate:
		v.Tier = t
		return v
	case item.Leggings:
		v.Tier = t
		return v
	case item.Boots:
		v.Tier = t
		return v
	}
	return it
}

// stewTypes maps the stew effects of the trade data to Bedrock's stew types.
var stewTypes = map[string]func() item.StewType{
	"night_vision": item.NightVisionPoppyStew,
	"jump_boost":   item.JumpBoostStew,
	"weakness":     item.WeaknessStew,
	"blindness":    item.BlindnessBluetStew,
	"poison":       item.PoisonStew,
	"saturation":   item.SaturationDandelionStew,
}

// enchantability is the Java Enchantable component of the items villagers
// enchant (ToolMaterial / ArmorMaterial enchantment values; bow, crossbow
// and fishing rod 1).
func enchantability(javaName string) int {
	switch javaName {
	case "iron_sword", "iron_pickaxe", "iron_axe", "iron_shovel", "iron_hoe":
		return 14
	case "iron_helmet", "iron_chestplate", "iron_leggings", "iron_boots":
		return 9
	case "diamond_sword", "diamond_pickaxe", "diamond_axe", "diamond_shovel", "diamond_hoe",
		"diamond_helmet", "diamond_chestplate", "diamond_leggings", "diamond_boots":
		return 10
	case "bow", "crossbow", "fishing_rod", "book":
		return 1
	}
	return 0
}
