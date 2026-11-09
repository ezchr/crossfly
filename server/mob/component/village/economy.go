//go:build crossfly_full

package village

import (
	"encoding/json"
	"fmt"
	"math"
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Offer is one generated trade (BDS MerchantRecipe).
type Offer struct {
	BuyA, BuyB, Sell     item.Stack
	BaseA, BaseB         int
	PriceMulA, PriceMulB float64
	Tier                 int
	Uses, MaxUses        int
	TraderExp            int
	Demand               int
	RewardExp            bool
	// PriceA is buyA's count after demand and discounts.
	PriceA int
}

// OutOfUses is MerchantRecipe::isOutOfUses.
func (o *Offer) OutOfUses() bool { return o.MaxUses >= 0 && o.Uses >= o.MaxUses }

// Economy is minecraft:economy_trade_table (BDS EconomyTradeableComponent)
// and the legacy minecraft:trade_table (LegacyTradeableComponent): it
// generates every tier's offers from the table at once, opens the trading
// window when a player interacts, prices the offers by demand and cured /
// hero discounts, counts uses, trader experience and levels, and restocks
// on minecraft:trade_resupply.
type Economy struct {
	legacy        bool
	displayName   string
	table         string
	newScreen     bool
	heroDemand    int
	curedMin      int
	curedMax      int
	maxCuredMin   int
	maxCuredMax   int
	nearbyCured   int
	maxNearby     int
	persist       bool
	showScreen    bool
	legacyFormula bool

	offers  []*Offer
	tierExp []int
	path    string
	riches  int
	exp     int
	maxTier int
	low     int
	high    int
	nearby  int

	trading      *world.EntityHandle
	closed       atomic.Bool
	levelUpTimer int
	levelPending bool
	levelPlayer  *world.EntityHandle
	variant      int32
}

func newEconomy(key string, raw json.RawMessage) (mob.Component, error) {
	j := struct {
		DisplayName   string    `json:"display_name"`
		Table         string    `json:"table"`
		NewScreen     bool      `json:"new_screen"`
		HeroDemand    *int      `json:"hero_demand_discount"`
		Cured         *intRange `json:"cured_discount"`
		MaxCured      *intRange `json:"max_cured_discount"`
		Nearby        *int      `json:"nearby_cured_discount"`
		MaxNearby     *int      `json:"max_nearby_cured_discount"`
		Persist       bool      `json:"persist_trades"`
		Show          *bool     `json:"show_trade_screen"`
		LegacyFormula bool      `json:"use_legacy_price_formula"`
	}{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &j); err != nil {
			return nil, fmt.Errorf("%v: %w", key, err)
		}
	}
	e := &Economy{legacy: key == "minecraft:trade_table", displayName: j.DisplayName, table: j.Table, newScreen: j.NewScreen,
		heroDemand: -4, curedMin: -25, curedMax: -20, maxCuredMin: -25, maxCuredMax: -20, nearbyCured: -20, maxNearby: -200,
		persist: j.Persist, showScreen: true, legacyFormula: j.LegacyFormula}
	if j.HeroDemand != nil {
		e.heroDemand = *j.HeroDemand
	}
	if j.Cured != nil {
		e.curedMin, e.curedMax = j.Cured.Min, j.Cured.Max
	}
	if j.MaxCured != nil {
		e.maxCuredMin, e.maxCuredMax = j.MaxCured.Min, j.MaxCured.Max
	}
	if j.Nearby != nil {
		e.nearbyCured = *j.Nearby
	}
	if j.MaxNearby != nil {
		e.maxNearby = *j.MaxNearby
	}
	if j.Show != nil {
		e.showScreen = *j.Show
	}
	if e.legacy {
		e.newScreen, e.legacyFormula = true, true
	}
	return e, nil
}

type intRange struct{ Min, Max int }

func (r *intRange) UnmarshalJSON(b []byte) error {
	var n int
	if json.Unmarshal(b, &n) == nil {
		r.Min, r.Max = n, n
		return nil
	}
	var a []int
	if json.Unmarshal(b, &a) == nil && len(a) > 0 {
		r.Min, r.Max = a[0], a[len(a)-1]
		return nil
	}
	var o struct{ Min, Max int }
	if err := json.Unmarshal(b, &o); err != nil {
		return err
	}
	r.Min, r.Max = o.Min, o.Max
	return nil
}

// EconomyOf returns m's trade component.
func EconomyOf(m *mob.Mob) (*Economy, bool) { return mob.ComponentOf[*Economy](m) }

// TradeLocked is DwellerComponent::_isLockedInToProfession: the villager
// has traded (tier or trade experience above 0).
func TradeLocked(m *mob.Mob) bool {
	if e, ok := EconomyOf(m); ok {
		return e.exp != 0 || m.Meta().TradeTier != 0
	}
	if v, ok := m.Data(tradeStateKey{}); ok {
		s := v.(*savedTrades)
		return s.exp != 0 || s.tier != 0
	}
	return false
}

// savedTrades keeps the offers over a component rebuild (a profession group
// swap keeps them when the table path matches).
type savedTrades struct {
	offers  []*Offer
	tierExp []int
	path    string
	exp     int
	tier    int
	maxTier int
	riches  int
	low     int
	high    int
	nearby  int
}

type tradeStateKey struct{}

func (e *Economy) Attach(m *mob.Mob) {
	e.variant = m.Meta().Variant
	if v, ok := m.Data(tradeStateKey{}); ok {
		s := v.(*savedTrades)
		e.offers, e.tierExp, e.path, e.exp, e.maxTier, e.riches = s.offers, s.tierExp, s.path, s.exp, s.maxTier, s.riches
		e.low, e.high, e.nearby = s.low, s.high, s.nearby
		m.SetData(tradeStateKey{}, nil)
	}
}

func (e *Economy) Detach(m *mob.Mob) {
	m.SetData(tradeStateKey{}, &savedTrades{offers: e.offers, tierExp: e.tierExp, path: e.path, exp: e.exp, tier: int(m.Meta().TradeTier),
		maxTier: e.maxTier, riches: e.riches, low: e.low, high: e.high, nearby: e.nearby})
	e.closeWindow(m)
}

// Offers returns the generated offers (all tiers).
func (e *Economy) Offers() []*Offer { return e.offers }

// Experience is the trade experience (TRADE_EXPERIENCE).
func (e *Economy) Experience() int { return e.exp }

// Riches is the emeralds received.
func (e *Economy) Riches() int { return e.riches }

// TierExp returns the experience each tier needs.
func (e *Economy) TierExp() []int { return e.tierExp }

// Trading returns the player trading with the mob.
func (e *Economy) Trading(tx *world.Tx) (*player.Player, bool) {
	if e.trading == nil || e.closed.Load() {
		return nil, false
	}
	ent, ok := e.trading.Entity(tx)
	if !ok {
		return nil, false
	}
	p, ok := ent.(*player.Player)
	return p, ok && !p.Dead()
}

// HasSupply is hasSupplyRemaining (filter has_trade_supply).
func (e *Economy) HasSupply() bool {
	for _, o := range e.offers {
		if !o.OutOfUses() {
			return true
		}
	}
	return false
}

// generate is EconomyTradeableComponent::_generateTrades.
func (e *Economy) generate(m *mob.Mob) {
	if len(e.offers) > 0 && (e.path == e.table || e.path == "") {
		e.path = e.table
		return
	}
	t, err := LoadTradeTable(e.table)
	if err != nil {
		return
	}
	r := m.Rand()
	e.offers, e.tierExp = nil, nil
	e.maxTier = len(t.Tiers) - 1
	for tierIdx, tier := range t.Tiers {
		e.tierExp = append(e.tierExp, tier.ExpRequired)
		for _, g := range tier.Groups {
			trades := append([]TradeDef(nil), g.Trades...)
			n := len(trades)
			if g.NumToSelect > 0 {
				n = g.NumToSelect
				rearrange(r.IntN, trades, n)
			}
			created := 0
			for _, td := range trades {
				o, ok := e.makeOffer(m, td, tierIdx)
				if !ok {
					continue
				}
				e.offers = append(e.offers, o)
				if created++; created >= n {
					break
				}
			}
		}
	}
	e.path = e.table
	if tier := int(m.Meta().TradeTier); tier > e.maxTier {
		e.setTier(m, e.maxTier)
	}
	m.UpdateMeta(func(meta *mob.Meta) { meta.MaxTradeTier = int32(max(e.maxTier, 0)) })
}

// rearrange is EconomyTradeableComponent::_rearrangeTradeList: n weighted
// picks moved to the front, then shuffled.
func rearrange(intn func(int) int, trades []TradeDef, n int) {
	if n <= 0 || n >= len(trades) {
		return
	}
	total := 0
	for _, t := range trades {
		total += t.Weight
	}
	for k := 0; k < n && total > 0; k++ {
		r := intn(total)
		for i := k; i < len(trades); i++ {
			r -= trades[i].Weight
			if r < 0 {
				total -= trades[i].Weight
				trades[k], trades[i] = trades[i], trades[k]
				break
			}
		}
	}
	for i := n - 1; i > 0; i-- {
		j := intn(i + 1)
		trades[i], trades[j] = trades[j], trades[i]
	}
}

func (e *Economy) filtered(m *mob.Mob, cs []TradeItemDef) []TradeItemDef {
	var out []TradeItemDef
	for _, c := range cs {
		if c.Filter == nil || c.Filter.Test(m.Ctx()) {
			out = append(out, c)
		}
	}
	return out
}

func (e *Economy) makeOffer(m *mob.Mob, td TradeDef, tier int) (*Offer, bool) {
	r := m.Rand()
	as := e.filtered(m, td.Wants[0])
	gs := e.filtered(m, td.Gives[0])
	var bs []TradeItemDef
	if len(td.Wants) > 1 {
		bs = e.filtered(m, td.Wants[1])
	}
	if len(as) == 0 || len(gs) == 0 || (len(td.Wants) > 1 && len(bs) == 0) {
		return nil, false
	}
	count := func(it TradeItemDef) int {
		if it.Min < it.Max {
			return it.Min + r.IntN(it.Max-it.Min)
		}
		return it.Min
	}
	a := as[r.IntN(len(as))]
	buyA, _, ok := makeStack(r, a, count(a))
	if !ok {
		return nil, false
	}
	o := &Offer{Tier: tier, MaxUses: td.MaxUses, TraderExp: td.TraderExp, RewardExp: td.RewardExp, PriceMulA: a.PriceMultiplier}
	if len(bs) > 0 {
		b := bs[r.IntN(len(bs))]
		buyB, _, ok := makeStack(r, b, count(b))
		if !ok {
			return nil, false
		}
		o.BuyB, o.PriceMulB = buyB, b.PriceMultiplier
	}
	g := gs[r.IntN(len(gs))]
	sell, price, ok := makeStack(r, g, count(g))
	if !ok {
		return nil, false
	}
	if price > 0 {
		buyA = buyA.Grow(min(price, max(1, buyA.MaxCount())) - buyA.Count())
	}
	o.BuyA, o.Sell = buyA, sell
	o.BaseA, o.BaseB = buyA.Count(), o.BuyB.Count()
	o.PriceA = o.BaseA
	return o, true
}

// tierFromExp is _getTradeTierFromCurrentExp.
func (e *Economy) tierFromExp() int {
	n := 0
	for _, req := range e.tierExp {
		if e.exp < req {
			break
		}
		n++
	}
	return max(n-1, 0)
}

func (e *Economy) setTier(m *mob.Mob, t int) {
	m.UpdateMeta(func(meta *mob.Meta) { meta.TradeTier = int32(t); meta.TradeExperience = int32(e.exp) })
}

// matchExpAndTier is EconomyTradeableComponent::matchExpAndTier.
func (e *Economy) matchExpAndTier(m *mob.Mob) {
	t := min(int(m.Meta().TradeTier), e.maxTier)
	req := 0
	if t >= 0 && t < len(e.tierExp) {
		req = e.tierExp[t]
	}
	if e.exp >= req {
		e.setTier(m, e.tierFromExp())
		return
	}
	e.exp = req
	e.setTier(m, max(t, 0))
}

// calculatePrices prices every offer for the trading player
// (MerchantRecipe::calculateDemandPrices and the legacy formula).
func (e *Economy) calculatePrices(p *player.Player) {
	heroAmp, hero := 0, false
	for _, ef := range p.Effects() {
		if ef.Type() == HeroOfTheVillage {
			hero, heroAmp = true, ef.Level()-1
		}
	}
	e.pricesFor(hero, heroAmp)
}

// pricesFor prices the offers with or without hero of the village.
func (e *Economy) pricesFor(hero bool, heroAmp int) {
	if e.legacyFormula {
		hd := 0
		if hero {
			hd = e.heroDemand
		}
		for _, o := range e.offers {
			bA := e.low + hd + e.nearby
			if o.PriceMulA >= 0.1 {
				bA = e.high + hd + e.nearby
			}
			o.PriceA = clampCount(int((float64(o.Demand+bA)*o.PriceMulA+1)*float64(o.BaseA)), o.BuyA)
		}
		return
	}
	low, high := e.low, e.high
	if low > 0 {
		low = 0
	} else {
		low = max(low, e.maxCuredMin)
	}
	if high > 0 {
		high = 0
	} else {
		high = max(high, e.maxCuredMax)
	}
	for _, o := range e.offers {
		h := 0
		if hero {
			h = -max(1, int(math.Floor(float64(o.BaseA)*(0.3+0.0625*float64(heroAmp)))))
		}
		disc := low + 5*high
		dem := max(0, int(math.Floor(float64(o.BaseA*o.Demand)*o.PriceMulA)))
		o.PriceA = clampCount(h+dem+o.BaseA+int(float64(disc)*o.PriceMulA), o.BuyA)
	}
}

func clampCount(n int, s item.Stack) int { return max(1, min(n, max(1, s.MaxCount()))) }

// resupply is EconomyTradeableComponent::resupplyTrades.
func (e *Economy) resupply() {
	for _, o := range e.offers {
		o.Demand = max(0, o.Demand+o.Uses-(o.MaxUses-o.Uses))
		o.Uses = 0
		o.PriceA = clampCount(o.BaseA+max(0, int(math.Floor(float64(o.BaseA*o.Demand)*o.PriceMulA))), o.BuyA)
	}
}

// Resupply restocks every offer (minecraft:trade_resupply).
func (e *Economy) Resupply() { e.resupply() }

// Interact opens the trading window (EconomyTradeableComponent::
// getInteraction): the trader must be alive and not trading already.
func (e *Economy) Interact(m *mob.Mob, user item.User, held item.Stack, _ mgl64.Vec3) (item.Stack, bool) {
	p, ok := user.(*player.Player)
	if !ok || !e.showScreen || m.Dead() || IsSleeping(m) {
		return held, false
	}
	if _, busy := e.Trading(m.Tx()); busy {
		return held, false
	}
	e.generate(m)
	if len(e.offers) == 0 {
		return held, false
	}
	e.trading = p.H()
	e.closed.Store(false)
	e.calculatePrices(p)
	e.matchExpAndTier(m)
	e.variant = m.Meta().Variant
	w := e.window(m)
	h, ph := m.H(), p.H()
	w.OnTradeTx = func(tx *world.Tx, index, times int) bool { return e.onTrade(tx, h, ph, index, times) }
	p.OpenTradeWindow(m, w, nil)
	m.PlaySound("haggle")
	return held, true
}

// window builds the trading window for the client.
func (e *Economy) window(m *mob.Mob) session.TradeWindow {
	w := session.TradeWindow{Name: e.displayName, Tier: int(m.Meta().TradeTier), TierExp: e.tierExp, Economic: !e.legacy,
		Experience: e.exp, OnClose: func() { e.closed.Store(true) }}
	for _, o := range e.offers {
		w.Offers = append(w.Offers, session.TradeOffer{
			Buy: o.BuyA.Grow(o.PriceA - o.BuyA.Count()), Buy2: o.BuyB, Sell: o.Sell, Uses: o.Uses, MaxUses: o.MaxUses,
			Tier: o.Tier, TraderExp: o.TraderExp, RewardExp: o.RewardExp, Demand: o.Demand,
			BaseBuyCount: o.BaseA, BaseBuy2Count: o.BaseB, PriceMultiplier: o.PriceMulA, PriceMultiplier2: o.PriceMulB,
		})
	}
	return w
}

// onTrade runs from the session, in the transaction the trade is handled
// in, when the player took offer index times.
func (e *Economy) onTrade(tx *world.Tx, h, ph *world.EntityHandle, index, times int) bool {
	m, ok := mob.OfHandle(tx, h)
	if !ok || index < 0 || index >= len(e.offers) {
		return false
	}
	o := e.offers[index]
	if o.Tier > int(m.Meta().TradeTier) || (o.MaxUses >= 0 && o.Uses+times > o.MaxUses) {
		return false
	}
	for range times {
		e.notifyTrade(m, tx, ph, index)
	}
	tx.Defer(func(tx *world.Tx) {
		mm, ok := mob.OfHandle(tx, h)
		pe, found := ph.Entity(tx)
		if !ok || !found {
			return
		}
		if p, isP := pe.(*player.Player); isP {
			p.UpdateTradeWindow(e.window(mm))
		}
	})
	return true
}

// notifyTrade is EconomyTradeableComponent::notifyTrade.
func (e *Economy) notifyTrade(m *mob.Mob, tx *world.Tx, ph *world.EntityHandle, index int) {
	o := e.offers[index]
	o.Uses++
	m.PlaySound("haggle.yes")
	xp := m.Rand().IntN(4) + 3
	if e.legacy {
		if o.Uses == 1 || m.Rand().IntN(5) == 0 {
			e.levelUpTimer, e.levelPending, e.levelPlayer = 40, true, ph
			xp += 5
		}
	} else if tier := int(m.Meta().TradeTier); tier < e.maxTier {
		e.exp += o.TraderExp
		if tier < e.tierFromExp() {
			e.levelUpTimer, e.levelPending, e.levelPlayer = 10, true, ph
			xp += 5
		}
		e.setTier(m, e.tierFromExp())
	}
	m.UpdateMeta(func(meta *mob.Meta) { meta.TradeExperience = int32(e.exp) })
	if name, _ := o.BuyA.Item().EncodeItem(); name == "minecraft:emerald" {
		e.riches += o.PriceA
	}
	if o.RewardExp {
		for _, orb := range entity.NewExperienceOrbs(m.Position().Add(mgl64.Vec3{0, 0.5}), xp) {
			tx.AddEntity(orb)
		}
	}
}

// Tick runs the level-up timer (newServerAiStep) and closes the window when
// it is no longer valid (Trade2ContainerManagerModel::isValid).
func (e *Economy) Tick(m *mob.Mob) {
	if e.trading != nil {
		p, ok := e.Trading(m.Tx())
		switch {
		case !ok:
			e.trading = nil
		case m.Dead(), m.Position().Sub(p.Position()).Len() > 8, m.Meta().Variant != e.variant, recentlyHurt(m):
			e.closeWindow(m)
		}
	}
	if e.trading == nil && e.levelUpTimer > 0 {
		if e.levelUpTimer--; e.levelUpTimer == 0 {
			if e.levelPending && e.levelPlayer != nil {
				if v, ok := VillageOf(m); ok {
					if pe, ok := e.levelPlayer.Entity(m.Tx()); ok {
						v.ModifyStanding(pe, StandingSmallReward)
					}
				}
			}
			e.levelPending, e.levelPlayer = false, nil
			m.AddEffect(effect.New(effect.Regeneration, 1, 200*1e9/20))
		}
	}
}

func recentlyHurt(m *mob.Mob) bool {
	_, t, ok := m.LastHurtBy()
	return ok && m.Ticks()-t < 10
}

func (e *Economy) closeWindow(m *mob.Mob) {
	if e.trading == nil {
		return
	}
	if pe, ok := e.trading.Entity(m.Tx()); ok {
		if p, ok := pe.(*player.Player); ok {
			p.CloseTradeWindow()
		}
	}
	e.trading = nil
	e.closed.Store(true)
}

// EncodeNBT ...
func (e *Economy) EncodeNBT() map[string]any {
	offers := make([]any, 0, len(e.offers))
	for _, o := range e.offers {
		r := map[string]any{"buyA": item.WriteNBT(o.BuyA, true), "sell": item.WriteNBT(o.Sell, true), "tier": int32(o.Tier),
			"uses": int32(o.Uses), "maxUses": int32(o.MaxUses), "traderExp": int32(o.TraderExp), "rewardExp": boolByte(o.RewardExp),
			"demand": int32(o.Demand), "buyCountA": int32(o.BaseA), "buyCountB": int32(o.BaseB),
			"priceMultiplierA": float32(o.PriceMulA), "priceMultiplierB": float32(o.PriceMulB)}
		if !o.BuyB.Empty() {
			r["buyB"] = item.WriteNBT(o.BuyB, true)
		}
		offers = append(offers, r)
	}
	tiers := make([]any, len(e.tierExp))
	for i, t := range e.tierExp {
		tiers[i] = int32(t)
	}
	return map[string]any{"Recipes": offers, "TierExp": tiers, "TradeTablePath": e.path, "Riches": int32(e.riches),
		"TradeExperience": int32(e.exp), "MaxTradeTier": int32(e.maxTier),
		"LowTierCuredDiscount": int32(e.low), "HighTierCuredDiscount": int32(e.high), "NearbyCuredDiscount": int32(e.nearby)}
}

// DecodeNBT ...
func (e *Economy) DecodeNBT(m *mob.Mob, data map[string]any) {
	i32 := func(k string) int { v, _ := data[k].(int32); return int(v) }
	e.path, _ = data["TradeTablePath"].(string)
	e.riches, e.exp, e.maxTier = i32("Riches"), i32("TradeExperience"), i32("MaxTradeTier")
	e.low, e.high, e.nearby = i32("LowTierCuredDiscount"), i32("HighTierCuredDiscount"), i32("NearbyCuredDiscount")
	e.tierExp = nil
	if l, ok := data["TierExp"].([]any); ok {
		for _, v := range l {
			n, _ := v.(int32)
			e.tierExp = append(e.tierExp, int(n))
		}
	}
	e.offers = nil
	if l, ok := data["Recipes"].([]any); ok {
		for _, rv := range l {
			r, ok := rv.(map[string]any)
			if !ok {
				continue
			}
			ri := func(k string) int { v, _ := r[k].(int32); return int(v) }
			rf := func(k string) float64 { v, _ := r[k].(float32); return float64(v) }
			o := &Offer{Tier: ri("tier"), Uses: ri("uses"), MaxUses: ri("maxUses"), TraderExp: ri("traderExp"), Demand: ri("demand"),
				BaseA: ri("buyCountA"), BaseB: ri("buyCountB"), PriceMulA: rf("priceMultiplierA"), PriceMulB: rf("priceMultiplierB")}
			o.RewardExp = r["rewardExp"] != uint8(0)
			if a, ok := r["buyA"].(map[string]any); ok {
				o.BuyA = item.ReadNBT(a, nil)
			}
			if b, ok := r["buyB"].(map[string]any); ok {
				o.BuyB = item.ReadNBT(b, nil)
			}
			if s, ok := r["sell"].(map[string]any); ok {
				o.Sell = item.ReadNBT(s, nil)
			}
			if o.BuyA.Empty() || o.Sell.Empty() {
				continue
			}
			o.PriceA = o.BaseA
			e.offers = append(e.offers, o)
		}
	}
	m.UpdateMeta(func(meta *mob.Meta) {
		meta.TradeExperience = int32(e.exp)
		meta.MaxTradeTier = int32(max(e.maxTier, 0))
	})
}

// tradeResupply is minecraft:trade_resupply (BDS TradeResupplyComponent):
// restocks once each time the component is added (the villager schedule
// adds it on arrival at the job site and removes it on the next schedule
// change). It runs on its first tick so a saved HasResupplied is honoured.
type tradeResupply struct{ done bool }

func newTradeResupply(string, json.RawMessage) (mob.Component, error) { return &tradeResupply{}, nil }
func (*tradeResupply) Attach(*mob.Mob)                                {}
func (*tradeResupply) Detach(*mob.Mob)                                {}
func (t *tradeResupply) Tick(m *mob.Mob) {
	if t.done {
		return
	}
	t.done = true
	if e, ok := EconomyOf(m); ok {
		e.resupply()
	}
}
func (t *tradeResupply) EncodeNBT() map[string]any {
	return map[string]any{"HasResupplied": boolByte(t.done)}
}
func (t *tradeResupply) DecodeNBT(_ *mob.Mob, d map[string]any) {
	if b, ok := d["HasResupplied"].(uint8); ok {
		t.done = b != 0
	}
}

// Trade takes offer index once for the player behind ph, as the trading
// window would (scripted trades, tests).
func (e *Economy) Trade(m *mob.Mob, tx *world.Tx, ph *world.EntityHandle, index int) bool {
	return e.onTrade(tx, m.H(), ph, index, 1)
}

// Generate generates the offers if the mob has none yet.
func (e *Economy) Generate(m *mob.Mob) { e.generate(m) }
