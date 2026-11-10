//go:build crossfly_full

package jvillage

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/jbrain"
	"github.com/df-mc/dragonfly/server/world"
)

// tradesJSON is the vanilla 26.3 villager trade data, bundled by
// data/gen_trades.py from the builtin data pack: villager_trade, trade_set,
// tags/villager_trade, the enchantments and the tags trades use.
//
//go:embed data/trades.json
var tradesJSON []byte

type enchDef struct {
	Weight    int      `json:"weight"`
	MaxLevel  int      `json:"max_level"`
	MinCost   [2]int   `json:"min_cost"`
	MaxCost   [2]int   `json:"max_cost"`
	Supported []string `json:"supported"`
	Primary   []string `json:"primary"`
	Exclusive []string `json:"exclusive"`
	name      string
}

func (e *enchDef) minCost(l int) int { return e.MinCost[0] + e.MinCost[1]*(l-1) }
func (e *enchDef) maxCost(l int) int { return e.MaxCost[0] + e.MaxCost[1]*(l-1) }

// isPrimaryItem is Enchantment.isPrimaryItem.
func (e *enchDef) isPrimaryItem(javaName string) bool {
	return slices.Contains(e.Supported, javaName) && (e.Primary == nil || slices.Contains(e.Primary, javaName))
}

type tradeData struct {
	Trades    map[string]json.RawMessage `json:"trades"`
	TradeTags map[string][]string        `json:"trade_tags"`
	TradeSets map[string]struct {
		Amount          json.RawMessage `json:"amount"`
		AllowDuplicates bool            `json:"allow_duplicates"`
		Trades          string          `json:"trades"`
	} `json:"trade_sets"`
	Enchantments    map[string]*enchDef `json:"enchantments"`
	EnchantmentTags map[string][]string `json:"enchantment_tags"`
	PotionTags      map[string][]string `json:"potion_tags"`
}

// tradeSet is a parsed TradeSet.
type tradeSet struct {
	// incomplete marks sets that lost trades to missing items.
	incomplete      bool
	amount          intProv
	allowDuplicates bool
	trades          []*villagerTrade
}

var data = sync.OnceValue(func() *parsedData {
	var d tradeData
	if err := json.Unmarshal(tradesJSON, &d); err != nil {
		panic("jvillage: trade data: " + err.Error())
	}
	p := &parsedData{raw: &d, trades: map[string]*villagerTrade{}, sets: map[string]*tradeSet{}, missing: map[string]error{}}
	for name, e := range d.Enchantments {
		e.name = name
	}
	for key, raw := range d.Trades {
		t, err := parseTrade(raw, &d)
		if err != nil {
			// Items Crossfly has no counterpart for (boats, maps, bells,
			// some 26.x plants): the trade is left out, like a Java
			// trade whose offer comes out empty.
			slog.Default().Debug("jvillage: villager trade left out", "trade", key, "err", err)
			p.missing[key] = err
			continue
		}
		t.key = key
		p.trades[key] = t
	}
	for key, s := range d.TradeSets {
		amount, err := parseIntProv(s.Amount)
		if err != nil {
			panic("jvillage: trade set " + key + ": " + err.Error())
		}
		ts := &tradeSet{amount: amount, allowDuplicates: s.AllowDuplicates}
		ref := trimNS(strings.TrimPrefix(s.Trades, "#"))
		ids := d.TradeTags[ref]
		if !strings.HasPrefix(s.Trades, "#") {
			ids = []string{ref}
		}
		for _, id := range ids {
			if t, ok := p.trades[trimNS(id)]; ok {
				ts.trades = append(ts.trades, t)
			} else {
				ts.incomplete = true
			}
		}
		p.sets[key] = ts
	}
	return p
})

type parsedData struct {
	missing map[string]error
	raw     *tradeData
	trades  map[string]*villagerTrade
	sets    map[string]*tradeSet
}

// lootCtx is the LootContext of a trade roll: the random and the merchant.
type lootCtx struct {
	r            jbrain.Random
	villagerType string
}

// intProv is a ContextIntProvider (constant, uniform, binomial, add).
type intProv func(c *lootCtx) int

// floatProv is a ContextFloatProvider (constant only in the trade data).
type floatProv func(c *lootCtx) float32

func parseIntProv(raw json.RawMessage) (intProv, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("missing number")
	}
	var n float64
	if json.Unmarshal(raw, &n) == nil {
		v := int(n)
		return func(*lootCtx) int { return v }, nil
	}
	var o struct {
		Type   string            `json:"type"`
		Min    json.RawMessage   `json:"min"`
		Max    json.RawMessage   `json:"max"`
		N      json.RawMessage   `json:"n"`
		P      json.RawMessage   `json:"p"`
		Value  json.RawMessage   `json:"value"`
		Inputs []json.RawMessage `json:"inputs"`
	}
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, err
	}
	switch trimNS(o.Type) {
	case "constant":
		return parseIntProv(o.Value)
	case "uniform":
		lo, err := parseIntProv(o.Min)
		if err != nil {
			return nil, err
		}
		hi, err := parseIntProv(o.Max)
		if err != nil {
			return nil, err
		}
		// UniformGenerator: Mth.nextInt(random, min, max).
		return func(c *lootCtx) int { return mthNextInt(c.r, lo(c), hi(c)) }, nil
	case "binomial":
		n, err := parseIntProv(o.N)
		if err != nil {
			return nil, err
		}
		p, err := parseFloatProv(o.P)
		if err != nil {
			return nil, err
		}
		// BinomialDistributionGenerator: n trials of nextFloat() < p.
		return func(c *lootCtx) int {
			trials, pp, res := n(c), p(c), 0
			for range trials {
				if c.r.Float32() < pp {
					res++
				}
			}
			return res
		}, nil
	case "add", "sum":
		var ins []intProv
		for _, in := range o.Inputs {
			f, err := parseIntProv(in)
			if err != nil {
				return nil, err
			}
			ins = append(ins, f)
		}
		return func(c *lootCtx) int {
			s := 0
			for _, f := range ins {
				s += f(c)
			}
			return s
		}, nil
	}
	return nil, fmt.Errorf("unsupported int provider %q", o.Type)
}

func parseFloatProv(raw json.RawMessage) (floatProv, error) {
	var n float64
	if json.Unmarshal(raw, &n) == nil {
		v := float32(n)
		return func(*lootCtx) float32 { return v }, nil
	}
	var o struct {
		Type  string          `json:"type"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, err
	}
	if trimNS(o.Type) == "constant" {
		return parseFloatProv(o.Value)
	}
	return nil, fmt.Errorf("unsupported float provider %q", o.Type)
}

// mthNextInt is Mth.nextInt(random, min, max).
func mthNextInt(r jbrain.Random, lo, hi int) int {
	if lo >= hi {
		return lo
	}
	return lo + r.IntN(hi-lo+1)
}

// tradeItem is the item being built for a trade: the Java name it has, the
// Dragonfly stack and the additional trade cost component.
type tradeItem struct {
	java      string
	stack     item.Stack
	addCost   int
	hasCost   bool
	enchanted bool
	dyed      bool
	hasMapID  bool
	// names are the Java enchantments added, in order.
	names []string
}

type modifier func(c *lootCtx, it *tradeItem)

type costDef struct {
	item   world.Item
	count  intProv
	potion string
}

type villagerTrade struct {
	key        string
	wants      costDef
	addWants   *costDef
	gives      costDef
	givesJava  string
	givesCount int
	maxUses    intProv
	xp         intProv
	repDisc    floatProv
	types      []string
	modifiers  []modifier
	doubleEnch []string
}

func parseCost(raw json.RawMessage) (costDef, error) {
	var c struct {
		ID         string          `json:"id"`
		Count      json.RawMessage `json:"count"`
		Components struct {
			PotionContents *struct {
				Potion string `json:"potion"`
			} `json:"minecraft:potion_contents"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return costDef{}, err
	}
	it, ok := JavaItem(c.ID)
	if !ok {
		return costDef{}, fmt.Errorf("no item for %v", c.ID)
	}
	d := costDef{item: it, count: func(*lootCtx) int { return 1 }}
	if len(c.Count) > 0 {
		f, err := parseIntProv(c.Count)
		if err != nil {
			return costDef{}, err
		}
		d.count = f
	}
	if c.Components.PotionContents != nil {
		d.potion = c.Components.PotionContents.Potion
	}
	return d, nil
}

// stackOf builds the stack of a cost or result with count n.
func (d costDef) stackOf(n int) item.Stack {
	it := d.item
	if d.potion != "" {
		if p, ok := JavaPotion(d.potion); ok {
			switch it.(type) {
			case item.Potion:
				it = item.Potion{Type: p}
			case item.SplashPotion:
				it = item.SplashPotion{Type: p}
			case item.LingeringPotion:
				it = item.LingeringPotion{Type: p}
			case item.Arrow:
				it = item.Arrow{Tip: p}
			}
		}
	}
	return item.NewStack(it, n)
}

func parseTrade(raw json.RawMessage, d *tradeData) (*villagerTrade, error) {
	var j struct {
		Wants      json.RawMessage `json:"wants"`
		AddWants   json.RawMessage `json:"additional_wants"`
		Gives      json.RawMessage `json:"gives"`
		MaxUses    json.RawMessage `json:"max_uses"`
		XP         json.RawMessage `json:"xp"`
		RepDisc    json.RawMessage `json:"reputation_discount"`
		Predicate  json.RawMessage `json:"merchant_predicate"`
		Modifier   json.RawMessage `json:"given_item_modifier"`
		DoubleEnch string          `json:"double_trade_price_enchantments"`
	}
	if err := json.Unmarshal(raw, &j); err != nil {
		return nil, err
	}
	t := &villagerTrade{}
	var err error
	if t.wants, err = parseCost(j.Wants); err != nil {
		return nil, err
	}
	if len(j.AddWants) > 0 {
		c, err := parseCost(j.AddWants)
		if err != nil {
			return nil, err
		}
		t.addWants = &c
	}
	if t.gives, err = parseCost(j.Gives); err != nil {
		return nil, err
	}
	var g struct {
		ID    string `json:"id"`
		Count int    `json:"count"`
	}
	_ = json.Unmarshal(j.Gives, &g)
	t.givesJava, t.givesCount = trimNS(g.ID), max(1, g.Count)
	t.maxUses = func(*lootCtx) int { return 4 }
	t.xp = func(*lootCtx) int { return 1 }
	t.repDisc = func(*lootCtx) float32 { return 0 }
	if len(j.MaxUses) > 0 {
		if t.maxUses, err = parseIntProv(j.MaxUses); err != nil {
			return nil, err
		}
	}
	if len(j.XP) > 0 {
		if t.xp, err = parseIntProv(j.XP); err != nil {
			return nil, err
		}
	}
	if len(j.RepDisc) > 0 {
		if t.repDisc, err = parseFloatProv(j.RepDisc); err != nil {
			return nil, err
		}
	}
	if len(j.Predicate) > 0 {
		var p struct {
			Type      string `json:"type"`
			Predicate struct {
				Preds struct {
					Variant json.RawMessage `json:"minecraft:villager/variant"`
				} `json:"minecraft:predicates"`
			} `json:"predicate"`
		}
		if err := json.Unmarshal(j.Predicate, &p); err != nil {
			return nil, err
		}
		var one string
		var many []string
		if json.Unmarshal(p.Predicate.Preds.Variant, &one) == nil {
			many = []string{one}
		} else if err := json.Unmarshal(p.Predicate.Preds.Variant, &many); err != nil {
			return nil, fmt.Errorf("merchant predicate: %w", err)
		}
		for _, v := range many {
			t.types = append(t.types, trimNS(v))
		}
	}
	if j.DoubleEnch != "" {
		t.doubleEnch = d.EnchantmentTags[trimNS(strings.TrimPrefix(j.DoubleEnch, "#"))]
	}
	if len(j.Modifier) > 0 {
		var list []json.RawMessage
		if j.Modifier[0] == '[' {
			if err := json.Unmarshal(j.Modifier, &list); err != nil {
				return nil, err
			}
		} else {
			list = []json.RawMessage{j.Modifier}
		}
		for _, m := range list {
			f, err := parseModifier(m, d)
			if err != nil {
				return nil, err
			}
			t.modifiers = append(t.modifiers, f)
		}
	}
	return t, nil
}

func tagOrList(raw json.RawMessage, tags map[string][]string) []string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if strings.HasPrefix(s, "#") {
			return tags[trimNS(s[1:])]
		}
		return []string{trimNS(s)}
	}
	var l []string
	_ = json.Unmarshal(raw, &l)
	for i := range l {
		l[i] = trimNS(l[i])
	}
	return l
}

func parseModifier(raw json.RawMessage, d *tradeData) (modifier, error) {
	var m struct {
		Type       string          `json:"type"`
		Levels     json.RawMessage `json:"levels"`
		Options    json.RawMessage `json:"options"`
		AddCost    bool            `json:"include_additional_cost_component"`
		OnlyCompat *bool           `json:"only_compatible"`
		Dyes       json.RawMessage `json:"number_of_dyes"`
		Effects    []struct {
			Type     string          `json:"type"`
			Duration json.RawMessage `json:"duration"`
		} `json:"effects"`
		ItemFilter struct {
			Items      json.RawMessage            `json:"items"`
			Predicates map[string]json.RawMessage `json:"predicates"`
		} `json:"item_filter"`
		OnFail json.RawMessage `json:"on_fail"`
		ID     string          `json:"id"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	switch trimNS(m.Type) {
	case "enchant_with_levels":
		levels, err := parseIntProv(m.Levels)
		if err != nil {
			return nil, err
		}
		opts := tagOrList(m.Options, d.EnchantmentTags)
		addCost := m.AddCost
		// EnchantWithLevelsFunction.run
		return func(c *lootCtx, it *tradeItem) {
			cost := levels(c)
			selectEnchantments(c, d, it, cost, opts)
			if addCost && !it.stack.Empty() && cost > 0 {
				it.addCost, it.hasCost = cost, true
			}
		}, nil
	case "enchant_randomly":
		opts := tagOrList(m.Options, d.EnchantmentTags)
		onlyCompat := m.OnlyCompat == nil || *m.OnlyCompat
		addCost := m.AddCost
		// EnchantRandomlyFunction.run
		return func(c *lootCtx, it *tradeItem) {
			isBook := it.java == "book"
			var cands []*enchDef
			for _, name := range opts {
				e := d.Enchantments[name]
				if e == nil {
					continue
				}
				if !isBook && onlyCompat && !slices.Contains(e.Supported, it.java) {
					continue
				}
				cands = append(cands, e)
			}
			if len(cands) == 0 {
				return
			}
			e := cands[c.r.IntN(len(cands))]
			level := mthNextInt(c.r, 1, e.MaxLevel)
			enchant(it, e.name, level)
			if addCost {
				it.addCost, it.hasCost = 2+c.r.IntN(5+level*10)+3*level, true
			}
		}, nil
	case "set_random_dyes":
		n, err := parseIntProv(m.Dyes)
		if err != nil {
			return nil, err
		}
		// SetRandomDyesFunction.run
		return func(c *lootCtx, it *tradeItem) {
			rolls := n(c)
			if rolls <= 0 {
				return
			}
			dyes := make([]int, rolls)
			for i := range dyes {
				dyes[i] = c.r.IntN(16)
			}
			it.stack = item.NewStack(withLeatherColour(it.stack.Item(), applyDyes(dyes)), 1)
			it.dyed = true
		}, nil
	case "set_stew_effect":
		effects := m.Effects
		// SetStewEffectFunction.run: a random entry (its duration provider
		// is rolled too; Bedrock stews carry fixed durations).
		durs := make([]intProv, len(effects))
		for i, e := range effects {
			f, err := parseIntProv(e.Duration)
			if err != nil {
				return nil, err
			}
			durs[i] = f
		}
		return func(c *lootCtx, it *tradeItem) {
			if it.java != "suspicious_stew" || len(effects) == 0 {
				return
			}
			i := c.r.IntN(len(effects))
			durs[i](c)
			if f, ok := stewTypes[trimNS(effects[i].Type)]; ok {
				it.stack = item.NewStack(item.SuspiciousStew{Type: f()}, it.stack.Count())
			}
		}, nil
	case "set_random_potion":
		opts := tagOrList(m.Options, d.PotionTags)
		return func(c *lootCtx, it *tradeItem) {
			if len(opts) == 0 {
				return
			}
			setPotion(it, opts[c.r.IntN(len(opts))])
		}, nil
	case "set_potion":
		id := trimNS(m.ID)
		return func(_ *lootCtx, it *tradeItem) { setPotion(it, id) }, nil
	case "exploration_map":
		// ExplorationMapFunction needs a structure search Crossfly cannot
		// do: no map id is set, so the filter that follows discards the
		// trade, as Java does when no structure is found.
		return func(*lootCtx, *tradeItem) {}, nil
	case "filtered":
		items := tagOrList(m.ItemFilter.Items, nil)
		preds := m.ItemFilter.Predicates
		discard := strings.Contains(string(m.OnFail), "discard")
		return func(_ *lootCtx, it *tradeItem) {
			ok := len(items) == 0 || slices.Contains(items, it.java)
			for p := range preds {
				switch trimNS(p) {
				case "enchantments", "stored_enchantments":
					ok = ok && it.enchanted
				case "dyed_color":
					ok = ok && it.dyed
				case "map_id":
					ok = ok && it.hasMapID
				}
			}
			if !ok && discard {
				it.stack = item.Stack{}
			}
		}, nil
	}
	return nil, fmt.Errorf("unsupported item modifier %q", m.Type)
}

func setPotion(it *tradeItem, name string) {
	p, ok := JavaPotion(name)
	if !ok {
		return
	}
	n := it.stack.Count()
	switch it.stack.Item().(type) {
	case item.Arrow:
		it.stack = item.NewStack(item.Arrow{Tip: p}, n)
	case item.Potion:
		it.stack = item.NewStack(item.Potion{Type: p}, n)
	case item.SplashPotion:
		it.stack = item.NewStack(item.SplashPotion{Type: p}, n)
	case item.LingeringPotion:
		it.stack = item.NewStack(item.LingeringPotion{Type: p}, n)
	}
}

// enchant is ItemStack.enchant (a book turns into an enchanted book).
func enchant(it *tradeItem, name string, level int) {
	it.enchanted = true
	if it.java == "book" {
		it.java = "enchanted_book"
		it.stack = item.NewStack(item.EnchantedBook{}, it.stack.Count())
	}
	if t, ok := JavaEnchantment(name); ok {
		it.stack = it.stack.WithEnchantments(item.NewEnchantment(t, level))
	}
	it.names = append(it.names, name)
}

// selectEnchantments is EnchantmentHelper.enchantItem/selectEnchantment.
func selectEnchantments(c *lootCtx, d *tradeData, it *tradeItem, cost int, opts []string) {
	value := enchantability(it.java)
	if value <= 0 {
		return
	}
	cost += 1 + c.r.IntN(value/4+1) + c.r.IntN(value/4+1)
	span := (c.r.Float32() + c.r.Float32() - 1) * 0.15
	cost = max(1, javaRound(float32(cost)+float32(cost)*span))
	isBook := it.java == "book"
	type inst struct {
		e     *enchDef
		level int
	}
	var avail []inst
	for _, name := range opts {
		e := d.Enchantments[name]
		if e == nil || !(e.isPrimaryItem(it.java) || isBook) {
			continue
		}
		for l := e.MaxLevel; l >= 1; l-- {
			if cost >= e.minCost(l) && cost <= e.maxCost(l) {
				avail = append(avail, inst{e, l})
				break
			}
		}
	}
	pick := func() (inst, bool) {
		total := 0
		for _, a := range avail {
			total += a.e.Weight
		}
		if total <= 0 {
			return inst{}, false
		}
		n := c.r.IntN(total)
		for _, a := range avail {
			n -= a.e.Weight
			if n < 0 {
				return a, true
			}
		}
		return inst{}, false
	}
	var results []inst
	if len(avail) == 0 {
		return
	}
	if a, ok := pick(); ok {
		results = append(results, a)
	}
	for c.r.IntN(50) <= cost {
		if len(results) > 0 {
			last := results[len(results)-1].e
			avail = slices.DeleteFunc(avail, func(a inst) bool { return !compatible(last, a.e) })
		}
		if len(avail) == 0 {
			break
		}
		if a, ok := pick(); ok {
			results = append(results, a)
		}
		cost /= 2
	}
	for _, r := range results {
		enchant(it, r.e.name, r.level)
	}
}

// compatible is Enchantment.areCompatible.
func compatible(a, b *enchDef) bool {
	return a != b && !slices.Contains(a.Exclusive, b.name) && !slices.Contains(b.Exclusive, a.name)
}

// javaRound is Math.round(float).
func javaRound(f float32) int {
	return int(floor32(f + 0.5))
}

func floor32(f float32) float32 {
	i := float32(int64(f))
	if i > f {
		i--
	}
	return i
}

// offer is VillagerTrade.getOffer: nil when the merchant predicate fails or
// a modifier discards the result.
func (t *villagerTrade) offer(c *lootCtx) *Offer {
	if t.types != nil && !slices.Contains(t.types, c.villagerType) {
		return nil
	}
	it := &tradeItem{java: t.givesJava, stack: t.gives.stackOf(t.givesCount)}
	for _, m := range t.modifiers {
		m(c, it)
		if it.stack.Empty() {
			return nil
		}
	}
	additional := 0
	if it.hasCost {
		additional = it.addCost
	}
	if t.doubleEnch != nil && it.java == "enchanted_book" {
		for _, n := range it.names {
			if slices.Contains(t.doubleEnch, n) {
				additional *= 2
				break
			}
		}
	}
	// TradeCost.toItemCost: clamp to [0, default max stack size].
	wantsMax := item.NewStack(t.wants.item, 1).MaxCount()
	count := min(max(t.wants.count(c)+additional, 0), wantsMax)
	if count < 1 {
		return nil
	}
	buyA := t.wants.stackOf(count)
	var buyB item.Stack
	if t.addWants != nil {
		bm := item.NewStack(t.addWants.item, 1).MaxCount()
		n := min(max(t.addWants.count(c), 0), bm)
		if n < 1 {
			return nil
		}
		buyB = t.addWants.stackOf(n)
	}
	return NewOffer(buyA, buyB, it.stack, max(t.maxUses(c), 1), max(t.xp(c), 0), max(t.repDisc(c), 0))
}

// AddOffersFromTradeSet is AbstractVillager.addOffersFromTradeSet: roll the
// set's amount of offers (count >= 0 overrides it) for a merchant of
// villagerType and append them. It reports false for an unknown set.
func AddOffersFromTradeSet(offers *Offers, set string, count int, villagerType string, r jbrain.Random) bool {
	ts, ok := data().sets[set]
	if !ok {
		return false
	}
	c := &lootCtx{r: r, villagerType: villagerType}
	n := count
	if n < 0 {
		n = ts.amount(c)
	}
	list := slices.Clone(ts.trades)
	found := 0
	for found < n && len(list) > 0 {
		if ts.allowDuplicates {
			// addOffersFromItemListings
			roll := r.IntN(len(list))
			o := list[roll].offer(c)
			if o == nil {
				list = slices.Delete(list, roll, roll+1)
				continue
			}
			*offers = append(*offers, o)
			found++
			continue
		}
		// addOffersFromItemListingsWithoutDuplicates
		roll := r.IntN(len(list))
		t := list[roll]
		list = slices.Delete(list, roll, roll+1)
		if o := t.offer(c); o != nil {
			*offers = append(*offers, o)
			found++
		}
	}
	return true
}

// TradeSets returns the keys of the loaded trade sets (for tests).
func TradeSets() []string {
	var out []string
	for k := range data().sets {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
