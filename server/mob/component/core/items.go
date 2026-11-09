//go:build crossfly_full

package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// ItemDescriptor is a Bedrock item descriptor as entity JSON writes it:
// "wheat", "minecraft:wheat", "bucket:1" (legacy name:data) or an object
// {"item": "..."} / {"tags": "..."}. BDS ItemDescriptor.
type ItemDescriptor struct {
	// Name is the namespaced item name as written ("minecraft:bucket").
	Name string
	// Data is the legacy data value after the colon, valid when HasData.
	Data    int16
	HasData bool
	// Tags is a Molang tag query ({"tags": ...}); it never matches (item
	// tags are not modelled).
	Tags string
}

// ParseItemDescriptor parses "name[:data]".
func ParseItemDescriptor(s string) ItemDescriptor {
	s = strings.TrimSpace(strings.ToLower(s))
	d := ItemDescriptor{}
	if i := strings.LastIndexByte(s, ':'); i >= 0 {
		if n, err := strconv.Atoi(s[i+1:]); err == nil {
			d.Data, d.HasData = int16(n), true
			s = s[:i]
		}
	}
	if !strings.Contains(s, ":") {
		s = "minecraft:" + s
	}
	d.Name = s
	return d
}

// ParseItemList reads a descriptor list: a string, a list of strings, or
// objects with "item" (and an optional "result_item"/"growth" the caller
// reads itself).
func ParseItemList(raw json.RawMessage) ([]ItemDescriptor, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	if raw[0] != '[' {
		raw = append(append([]byte{'['}, raw...), ']')
	}
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("item list: %w", err)
	}
	out := make([]ItemDescriptor, 0, len(list))
	for _, e := range list {
		d, err := parseItemEntry(e)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func parseItemEntry(e json.RawMessage) (ItemDescriptor, error) {
	var s string
	if json.Unmarshal(e, &s) == nil {
		return ParseItemDescriptor(s), nil
	}
	var obj struct {
		Item string `json:"item"`
		Name string `json:"name"`
		Tags string `json:"tags"`
		Data *int16 `json:"data"`
	}
	if err := json.Unmarshal(e, &obj); err != nil {
		return ItemDescriptor{}, fmt.Errorf("item descriptor %s: %w", e, err)
	}
	if obj.Item == "" {
		obj.Item = obj.Name
	}
	if obj.Item == "" {
		return ItemDescriptor{Tags: obj.Tags}, nil
	}
	d := ParseItemDescriptor(obj.Item)
	if obj.Data != nil {
		d.Data, d.HasData = *obj.Data, true
	}
	return d, nil
}

// Matches reports whether the stack is this item.
func (d ItemDescriptor) Matches(s item.Stack) bool {
	if s.Empty() || d.Name == "" {
		return false
	}
	name, meta := s.Item().EncodeItem()
	for _, c := range d.candidates() {
		if c.name == name && (!c.hasData || c.data == meta) {
			return true
		}
	}
	return false
}

// MatchesAny reports whether s matches one of ds.
func MatchesAny(ds []ItemDescriptor, s item.Stack) bool {
	for _, d := range ds {
		if d.Matches(s) {
			return true
		}
	}
	return false
}

// Stack returns count of the described item, or false if Dragonfly has no
// such item.
func (d ItemDescriptor) Stack(count int) (item.Stack, bool) {
	for _, c := range d.candidates() {
		if it, ok := world.ItemByName(c.name, c.data); ok {
			return item.NewStack(it, count), true
		}
	}
	return item.Stack{}, false
}

type itemName struct {
	name    string
	data    int16
	hasData bool
}

// candidates resolves legacy names (and name:data pairs) to the names
// Dragonfly encodes items with. The descriptor name itself comes first.
func (d ItemDescriptor) candidates() []itemName {
	short := strings.TrimPrefix(d.Name, "minecraft:")
	if d.HasData {
		if to, ok := legacyData(short, d.Data); ok {
			return []itemName{{name: "minecraft:" + to}}
		}
		if to, ok := legacyItems[short]; ok {
			return []itemName{{name: "minecraft:" + to, data: d.Data, hasData: d.Data != 0}, {name: d.Name, data: d.Data, hasData: true}}
		}
		// name:0 means "the item, any or no data" for items without variants.
		return []itemName{{name: d.Name, data: d.Data, hasData: d.Data != 0}}
	}
	out := []itemName{{name: d.Name}}
	if to, ok := legacyItems[short]; ok {
		out = append(out, itemName{name: "minecraft:" + to})
	}
	return out
}

// legacyItems maps item names the behaviour pack still uses (loot tables,
// interactions) to current ones.
var legacyItems = map[string]string{
	"muttonraw":         "mutton",
	"muttoncooked":      "cooked_mutton",
	"netherstar":        "nether_star",
	"totem":             "totem_of_undying",
	"clownfish":         "tropical_fish",
	"fish":              "cod",
	"cooked_fish":       "cooked_cod",
	"lily_pad":          "waterlily",
	"tallgrass":         "short_grass",
	"grass":             "grass_block",
	"record_11":         "music_disc_11",
	"record_13":         "music_disc_13",
	"record_blocks":     "music_disc_blocks",
	"record_cat":        "music_disc_cat",
	"record_chirp":      "music_disc_chirp",
	"record_far":        "music_disc_far",
	"record_mall":       "music_disc_mall",
	"record_mellohi":    "music_disc_mellohi",
	"record_stal":       "music_disc_stal",
	"record_strad":      "music_disc_strad",
	"record_wait":       "music_disc_wait",
	"record_ward":       "music_disc_ward",
	"golden_dandelion":  "golden_dandelion",
	"melon":             "melon_slice",
	"speckled_melon":    "glistering_melon_slice",
	"fireball":          "fire_charge",
	"carrotonastick":    "carrot_on_a_stick",
	"horsearmorleather": "leather_horse_armor",
}

var legacyDyes = [...]string{"ink_sac", "red_dye", "green_dye", "cocoa_beans", "lapis_lazuli", "purple_dye", "cyan_dye",
	"light_gray_dye", "gray_dye", "pink_dye", "lime_dye", "yellow_dye", "light_blue_dye", "magenta_dye", "orange_dye",
	"bone_meal", "black_dye", "brown_dye", "blue_dye", "white_dye"}

var legacySkulls = [...]string{"skeleton_skull", "wither_skeleton_skull", "zombie_head", "player_head", "creeper_head", "dragon_head", "piglin_head"}

var legacyBuckets = map[int16]string{0: "bucket", 1: "milk_bucket", 2: "cod_bucket", 3: "salmon_bucket",
	4: "tropical_fish_bucket", 5: "pufferfish_bucket", 8: "water_bucket", 10: "lava_bucket", 11: "powder_snow_bucket",
	12: "axolotl_bucket", 13: "tadpole_bucket"}

// legacyData resolves name:data items that became separate items.
func legacyData(short string, data int16) (string, bool) {
	switch short {
	case "bucket":
		n, ok := legacyBuckets[data]
		return n, ok
	case "dye":
		if int(data) < len(legacyDyes) && data >= 0 {
			return legacyDyes[data], true
		}
	case "skull":
		if int(data) < len(legacySkulls) && data >= 0 {
			return legacySkulls[data], true
		}
	case "wool", "carpet", "concrete", "stained_glass", "stained_hardened_clay", "concrete_powder":
		cs := item.Colours()
		if int(data) < len(cs) && data >= 0 {
			suffix := map[string]string{"stained_hardened_clay": "terracotta"}[short]
			if suffix == "" {
				suffix = short
			}
			return cs[data].String() + "_" + suffix, true
		}
	case "coal":
		if data == 1 {
			return "charcoal", true
		}
	}
	return "", false
}
