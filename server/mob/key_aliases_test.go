//go:build crossfly_full

package mob

import (
	"encoding/json"
	"testing"
)

func TestAliasKeys(t *testing.T) {
	for _, c := range []struct {
		key, in, want string
	}{
		// TameableDefinition "tame_items|tameItems".
		{"minecraft:tameable", `{"probability": 0.33, "tameItems": ["carrot"]}`, `{"probability":0.33,"tame_items":["carrot"]}`},
		// Both spellings: the first of the schema name wins ("tame_items" here).
		{"minecraft:tameable", `{"tameItems": ["bone"], "tame_items": ["carrot"]}`, `{"tame_items":["carrot"]}`},
		// "requireTame|require_tame": the camelCase name wins.
		{"minecraft:breedable", `{"require_tame": false, "requireTame": true}`, `{"require_tame":true}`},
		// Nested: BreedableType "mateType|mate_type", "babyType|baby_type".
		{"minecraft:breedable", `{"breedsWith": [{"mateType": "a:b", "babyType": "a:c", "breed_event": {"event": "e"}}], "inheritTamed": false}`,
			`{"breeds_with":[{"mate_type":"a:b","baby_type":"a:c","breed_event":{"event":"e"}}],"inherit_tamed":false}`},
		{"minecraft:nameable", `{"alwaysShow": true, "allowNameTagRenaming": false}`, `{"always_show":true,"allow_name_tag_renaming":false}`},
		{"minecraft:tamemount", `{"maxTemper": 100, "feedItems": [{"item": "wheat", "temperMod": 3}]}`, `{"max_temper":100,"feed_items":[{"item":"wheat","temper_mod":3}]}`},
		{"minecraft:shooter", `{"def": "minecraft:arrow", "auxVal": 2}`, `{"def":"minecraft:arrow","aux_val":2}`},
	} {
		got := aliasKeys(c.key, json.RawMessage(c.in))
		if string(got) != c.want {
			t.Errorf("%s %s:\n got %s\nwant %s", c.key, c.in, got, c.want)
		}
	}
	// Nothing to rename, or a component without aliases: the same JSON.
	for _, c := range []struct{ key, in string }{
		{"minecraft:tameable", `{"tame_items": ["bone"]}`},
		{"minecraft:health", `{"tameItems": 1}`},
	} {
		if got := aliasKeys(c.key, json.RawMessage(c.in)); string(got) != c.in {
			t.Errorf("%s %s changed to %s", c.key, c.in, got)
		}
	}
}
