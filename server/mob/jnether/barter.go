//go:build crossfly_full

package jnether

import (
	_ "embed"
	"encoding/json"
	"math"
	"math/rand/v2"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
)

// Piglin bartering (PiglinAi.getBarterResponseItems): one roll of the
// gameplay/piglin_bartering loot table of 26.3 (embedded as is) for every
// gold ingot a piglin takes. The table's modifiers are set_count (uniform),
// set_potion and enchant_randomly (soul speed I-III on books and iron
// boots).

//go:embed data/piglin_bartering.json
var barteringJSON []byte

type barterEntry struct {
	Name     string          `json:"name"`
	Weight   int             `json:"weight"`
	Modifier json.RawMessage `json:"modifier"`
}

var (
	barterOnce    sync.Once
	barterEntries []barterEntry
	barterTotal   int
)

func loadBartering() {
	var t struct {
		Pools []struct {
			Entries []barterEntry `json:"entries"`
		} `json:"pools"`
	}
	if err := json.Unmarshal(barteringJSON, &t); err != nil || len(t.Pools) == 0 {
		panic("jnether: piglin_bartering.json: " + err.Error())
	}
	barterEntries = t.Pools[0].Entries
	for i := range barterEntries {
		if barterEntries[i].Weight == 0 {
			barterEntries[i].Weight = 1
		}
		barterTotal += barterEntries[i].Weight
	}
}

// Barter rolls the bartering loot table once.
func Barter(r *rand.Rand) []item.Stack {
	barterOnce.Do(loadBartering)
	n := r.IntN(barterTotal)
	for _, e := range barterEntries {
		if n -= e.Weight; n < 0 {
			if st, ok := barterItem(e, r); ok {
				return []item.Stack{st}
			}
			return nil
		}
	}
	return nil
}

// BarterWeights returns each entry's Java item name and weight (for tests
// and /mobdebug).
func BarterWeights() map[string]int {
	barterOnce.Do(loadBartering)
	out := map[string]int{}
	for _, e := range barterEntries {
		out[e.Name] += e.Weight
	}
	return out
}

// javaToBedrockItem are the bartering items whose Bedrock name differs.
// Bedrock has no spectral arrow: plain arrows stand in for it.
var javaToBedrockItem = map[string]string{
	"minecraft:nether_brick":   "minecraft:netherbrick",
	"minecraft:spectral_arrow": "minecraft:arrow",
}

// barterItem makes the stack of an entry with its modifier.
func barterItem(e barterEntry, r *rand.Rand) (item.Stack, bool) {
	var mod struct {
		Type    string          `json:"type"`
		Count   json.RawMessage `json:"count"`
		ID      string          `json:"id"`
		Options string          `json:"options"`
	}
	if len(e.Modifier) > 0 {
		_ = json.Unmarshal(e.Modifier, &mod)
	}
	typ := strings.TrimPrefix(mod.Type, "minecraft:")
	switch e.Name {
	case "minecraft:potion", "minecraft:splash_potion":
		p := potion.Water()
		if typ == "set_potion" && strings.TrimPrefix(mod.ID, "minecraft:") == "fire_resistance" {
			p = potion.FireResistance()
		}
		if e.Name == "minecraft:splash_potion" {
			return item.NewStack(item.SplashPotion{Type: p}, 1), true
		}
		return item.NewStack(item.Potion{Type: p}, 1), true
	}
	name := e.Name
	if b, ok := javaToBedrockItem[name]; ok {
		name = b
	}
	it, ok := world.ItemByName(name, 0)
	if !ok {
		return item.Stack{}, false
	}
	count := 1
	if typ == "set_count" {
		count = countOf(mod.Count, r)
	}
	st := item.NewStack(it, count)
	if typ == "enchant_randomly" {
		// EnchantRandomlyFunction with one option: soul speed, a level from
		// 1 to its maximum (3); a book becomes an enchanted book.
		lvl := 1 + r.IntN(enchantment.SoulSpeed.MaxLevel())
		if name == "minecraft:book" {
			st = item.NewStack(item.EnchantedBook{}, 1)
		}
		st = st.WithEnchantments(item.NewEnchantment(enchantment.SoulSpeed, lvl))
	}
	return st, true
}

// countOf is a number provider: a constant or uniform(min, max) (an int
// between the floors of both, inclusive).
func countOf(raw json.RawMessage, r *rand.Rand) int {
	var n float64
	if json.Unmarshal(raw, &n) == nil {
		return int(n)
	}
	var u struct {
		Min float64 `json:"min"`
		Max float64 `json:"max"`
	}
	if json.Unmarshal(raw, &u) != nil {
		return 1
	}
	lo, hi := int(math.Floor(u.Min)), int(math.Floor(u.Max))
	if hi <= lo {
		return lo
	}
	return lo + r.IntN(hi-lo+1)
}
