//go:build crossfly_full

package village

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"image/color"
	"io/fs"
	"math/rand/v2"
	"path"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/mob/component/loot"
)

// Trade tables: trading/*.json (legacy minecraft:trade_table) and
// trading/economy_trades/*.json (minecraft:economy_trade_table), merged
// from the BDS 1.26.63 behaviour packs like the entity files. Three
// economy tables and six legacy ones are stored in the packs in Mojang's
// binary JSON; those come from Mojang/bedrock-samples (the same content).
//
//go:embed trading
var tradingFS embed.FS

var (
	tablesMu sync.Mutex
	tables   = map[string]*TradeTable{}
	extra    []fs.FS
)

// AddTradeTables adds a file system with custom trade tables (paths like
// "trading/economy_trades/x.json"), looked up before the vanilla ones.
func AddTradeTables(fsys fs.FS) {
	tablesMu.Lock()
	defer tablesMu.Unlock()
	extra = append(extra, fsys)
	tables = map[string]*TradeTable{}
}

// TradeTable is a parsed trade table (BDS TradeTableData).
type TradeTable struct {
	Path  string
	Tiers []TradeTierDef
}

// TradeTierDef is one tier of a table.
type TradeTierDef struct {
	ExpRequired int
	Groups      []TradeGroupDef
}

// TradeGroupDef is a group of trades of which NumToSelect are offered (-1
// all).
type TradeGroupDef struct {
	NumToSelect int
	Trades      []TradeDef
}

// TradeDef is one trade.
type TradeDef struct {
	Wants     [][]TradeItemDef // per slot (max 2), the choices
	Gives     [][]TradeItemDef
	TraderExp int
	MaxUses   int
	RewardExp bool
	Weight    int
}

// TradeItemDef is one item choice of a trade slot.
type TradeItemDef struct {
	Item            string
	Min, Max        int
	PriceMultiplier float64
	Functions       []json.RawMessage
	Filter          mob.Filter
}

// LoadTradeTable returns the parsed table at p ("trading/economy_trades/
// farmer_trades.json").
func LoadTradeTable(p string) (*TradeTable, error) {
	p = strings.TrimPrefix(path.Clean(strings.ReplaceAll(p, "\\", "/")), "/")
	tablesMu.Lock()
	defer tablesMu.Unlock()
	if t, ok := tables[p]; ok {
		return t, nil
	}
	var data []byte
	var err error = fs.ErrNotExist
	for i := len(extra) - 1; i >= 0 && err != nil; i-- {
		data, err = fs.ReadFile(extra[i], p)
	}
	if err != nil {
		data, err = tradingFS.ReadFile(p)
	}
	if err != nil {
		return nil, fmt.Errorf("trade table %v: %w", p, err)
	}
	t, err := parseTradeTable(p, data)
	if err != nil {
		return nil, err
	}
	tables[p] = t
	return t, nil
}

// parseTradeTable is TradeTableData::parseJson.
func parseTradeTable(p string, data []byte) (*TradeTable, error) {
	var j struct {
		Tiers []struct {
			TotalExp *int              `json:"total_exp_required"`
			Groups   []json.RawMessage `json:"groups"`
			Trades   []json.RawMessage `json:"trades"`
		} `json:"tiers"`
	}
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, fmt.Errorf("trade table %v: %w", p, err)
	}
	t := &TradeTable{Path: p}
	for _, tj := range j.Tiers {
		tier := TradeTierDef{}
		if tj.TotalExp != nil {
			tier.ExpRequired = *tj.TotalExp
		}
		if len(tj.Groups) == 0 && len(tj.Trades) > 0 {
			g, err := parseGroup(-1, tj.Trades)
			if err != nil {
				return nil, fmt.Errorf("trade table %v: %w", p, err)
			}
			tier.Groups = append(tier.Groups, g)
		}
		for _, gr := range tj.Groups {
			var gj struct {
				Num    *int              `json:"num_to_select"`
				Trades []json.RawMessage `json:"trades"`
			}
			if err := json.Unmarshal(gr, &gj); err != nil {
				return nil, fmt.Errorf("trade table %v: %w", p, err)
			}
			n := -1
			if gj.Num != nil {
				n = *gj.Num
			}
			g, err := parseGroup(n, gj.Trades)
			if err != nil {
				return nil, fmt.Errorf("trade table %v: %w", p, err)
			}
			tier.Groups = append(tier.Groups, g)
		}
		t.Tiers = append(t.Tiers, tier)
	}
	return t, nil
}

func parseGroup(n int, trades []json.RawMessage) (TradeGroupDef, error) {
	g := TradeGroupDef{NumToSelect: n}
	for _, raw := range trades {
		var tj struct {
			Wants     []json.RawMessage `json:"wants"`
			Gives     []json.RawMessage `json:"gives"`
			TraderExp *int              `json:"trader_exp"`
			MaxUses   *int              `json:"max_uses"`
			RewardExp *bool             `json:"reward_exp"`
			Weight    *int              `json:"weight"`
		}
		if err := json.Unmarshal(raw, &tj); err != nil {
			return g, err
		}
		td := TradeDef{TraderExp: 1, MaxUses: 7, RewardExp: true, Weight: 1}
		if tj.TraderExp != nil {
			td.TraderExp = *tj.TraderExp
		}
		if tj.MaxUses != nil {
			td.MaxUses = *tj.MaxUses
		}
		if tj.RewardExp != nil {
			td.RewardExp = *tj.RewardExp
		}
		if tj.Weight != nil {
			if *tj.Weight <= 0 {
				continue
			}
			td.Weight = *tj.Weight
		}
		for i, w := range tj.Wants {
			if i >= 2 {
				break
			}
			c, err := parseChoices(w)
			if err != nil {
				return g, err
			}
			td.Wants = append(td.Wants, c)
		}
		for i, w := range tj.Gives {
			if i >= 1 {
				break
			}
			c, err := parseChoices(w)
			if err != nil {
				return g, err
			}
			td.Gives = append(td.Gives, c)
		}
		if len(td.Wants) == 0 || len(td.Gives) == 0 {
			continue
		}
		g.Trades = append(g.Trades, td)
	}
	if n > len(g.Trades) {
		g.NumToSelect = len(g.Trades)
	}
	return g, nil
}

// parseChoices reads an item object or {"choice": [items]}.
func parseChoices(raw json.RawMessage) ([]TradeItemDef, error) {
	var c struct {
		Choice []json.RawMessage `json:"choice"`
	}
	if err := json.Unmarshal(raw, &c); err == nil && c.Choice != nil {
		var out []TradeItemDef
		for _, r := range c.Choice {
			it, err := parseTradeItem(r)
			if err != nil {
				return nil, err
			}
			out = append(out, it)
		}
		return out, nil
	}
	it, err := parseTradeItem(raw)
	if err != nil {
		return nil, err
	}
	return []TradeItemDef{it}, nil
}

func parseTradeItem(raw json.RawMessage) (TradeItemDef, error) {
	var j struct {
		Item      string            `json:"item"`
		Quantity  json.RawMessage   `json:"quantity"`
		PriceMul  float64           `json:"price_multiplier"`
		Functions []json.RawMessage `json:"functions"`
		Filters   json.RawMessage   `json:"filters"`
	}
	if err := json.Unmarshal(raw, &j); err != nil {
		return TradeItemDef{}, err
	}
	it := TradeItemDef{Item: j.Item, Min: 1, Max: 1, PriceMultiplier: j.PriceMul, Functions: j.Functions}
	if q := bytes.TrimSpace(j.Quantity); len(q) > 0 {
		var n int
		var r struct{ Min, Max int }
		if json.Unmarshal(q, &n) == nil {
			it.Min, it.Max = n, n
		} else if json.Unmarshal(q, &r) == nil {
			it.Min, it.Max = r.Min, r.Max
		}
	}
	it.Min = max(1, it.Min)
	it.Max = max(it.Min, it.Max)
	if len(bytes.TrimSpace(j.Filters)) > 0 {
		f, err := mob.CompileFilter(j.Filters)
		if err != nil {
			return it, err
		}
		it.Filter = f
	}
	return it, nil
}

// makeStack builds the item of a trade item with count n, the functions
// applied. It returns the price override of enchant_book_for_trading (0 if
// none).
func makeStack(r *rand.Rand, it TradeItemDef, n int) (item.Stack, int, bool) {
	d := core.ParseItemDescriptor(it.Item)
	s, ok := d.Stack(1)
	if !ok {
		return item.Stack{}, 0, false
	}
	n = min(n, max(1, s.MaxCount()))
	s = s.Grow(n - 1)
	price := 0
	for _, raw := range it.Functions {
		var f struct {
			Function       string          `json:"function"`
			Levels         json.RawMessage `json:"levels"`
			Treasure       bool            `json:"treasure"`
			ID             string          `json:"id"`
			Values         struct{ Min, Max int }
			BaseCost       *int `json:"base_cost"`
			BaseRandomCost *int `json:"base_random_cost"`
			PerLevelRandom *int `json:"per_level_random_cost"`
			PerLevelCost   *int `json:"per_level_cost"`
		}
		if json.Unmarshal(raw, &f) != nil {
			continue
		}
		switch strings.TrimPrefix(f.Function, "minecraft:") {
		case "enchant_with_levels":
			lo, hi := rangeOf(f.Levels)
			lvl := lo
			if hi > lo {
				lvl += r.IntN(hi - lo + 1)
			}
			s = loot.RandomlyEnchant(r, s, lvl, f.Treasure)
		case "enchant_book_for_trading":
			var lvl int
			s, lvl = enchantBookForTrading(r, s)
			cost := or(f.BaseCost, 2) + r.IntN(or(f.BaseRandomCost, 5)+or(f.PerLevelRandom, 10)*lvl) + or(f.PerLevelCost, 3)*lvl
			price = cost
		case "set_potion":
			if p, ok := potionByID(f.ID); ok {
				s = withPotion(s, p)
			}
		case "random_dye":
			s = randomDye(r, s)
		case "random_aux_value":
			// Aux values only matter for legacy items Dragonfly resolves by
			// name; the item keeps its default variant.
		case "exploration_map":
			// Maps are not modelled: the plain item stays.
		}
	}
	return s, price, true
}

func or(p *int, d int) int {
	if p == nil {
		return d
	}
	return *p
}

func rangeOf(raw json.RawMessage) (int, int) {
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return n, n
	}
	var r struct{ Min, Max int }
	if json.Unmarshal(raw, &r) == nil {
		return r.Min, max(r.Min, r.Max)
	}
	return 0, 0
}

// enchantBookForTrading picks a random enchantment and level for a book
// (EnchantBookForTradingFunction), returning the level.
func enchantBookForTrading(r *rand.Rand, s item.Stack) (item.Stack, int) {
	all := item.Enchantments()
	var pool []item.EnchantmentType
	for _, e := range all {
		if e == enchantment.SoulSpeed || e == enchantment.SwiftSneak {
			continue
		}
		pool = append(pool, e)
	}
	if len(pool) == 0 {
		return s, 1
	}
	e := pool[r.IntN(len(pool))]
	lvl := 1 + r.IntN(max(1, e.MaxLevel()))
	book := item.NewStack(item.EnchantedBook{}, 1).WithEnchantments(item.NewEnchantment(e, lvl))
	return book, lvl
}

func potionByID(id string) (potion.Potion, bool) {
	id = strings.TrimPrefix(strings.ToLower(id), "minecraft:")
	m := map[string]func() potion.Potion{
		"water": potion.Water, "mundane": potion.Mundane, "thick": potion.Thick, "awkward": potion.Awkward,
		"night_vision": potion.NightVision, "nightvision": potion.NightVision, "invisibility": potion.Invisibility,
		"leaping": potion.Leaping, "fire_resistance": potion.FireResistance, "swiftness": potion.Swiftness,
		"slowness": potion.Slowness, "water_breathing": potion.WaterBreathing, "healing": potion.Healing,
		"harming": potion.Harming, "poison": potion.Poison, "regeneration": potion.Regeneration,
		"strength": potion.Strength, "weakness": potion.Weakness, "turtle_master": potion.TurtleMaster,
		"slow_falling": potion.SlowFalling, "long_invisibility": potion.LongInvisibility,
	}
	if f, ok := m[id]; ok {
		return f(), true
	}
	return potion.Potion{}, false
}

func withPotion(s item.Stack, p potion.Potion) item.Stack {
	switch s.Item().(type) {
	case item.Potion:
		return item.NewStack(item.Potion{Type: p}, s.Count())
	case item.SplashPotion:
		return item.NewStack(item.SplashPotion{Type: p}, s.Count())
	case item.LingeringPotion:
		return item.NewStack(item.LingeringPotion{Type: p}, s.Count())
	case item.Arrow:
		return item.NewStack(item.Arrow{Tip: p}, s.Count())
	}
	return s
}

// randomDye is RandomDyeFunction: leather armour gets a random colour.
func randomDye(r *rand.Rand, s item.Stack) item.Stack {
	switch it := s.Item().(type) {
	case item.Helmet:
		if t, ok := it.Tier.(item.ArmourTierLeather); ok {
			t.Colour = randomColour(r)
			it.Tier = t
			return s.WithItem(it)
		}
	case item.Chestplate:
		if t, ok := it.Tier.(item.ArmourTierLeather); ok {
			t.Colour = randomColour(r)
			it.Tier = t
			return s.WithItem(it)
		}
	case item.Leggings:
		if t, ok := it.Tier.(item.ArmourTierLeather); ok {
			t.Colour = randomColour(r)
			it.Tier = t
			return s.WithItem(it)
		}
	case item.Boots:
		if t, ok := it.Tier.(item.ArmourTierLeather); ok {
			t.Colour = randomColour(r)
			it.Tier = t
			return s.WithItem(it)
		}
	}
	return s
}

func randomColour(r *rand.Rand) color.RGBA {
	c := item.Colours()[r.IntN(len(item.Colours()))]
	return c.RGBA()
}
