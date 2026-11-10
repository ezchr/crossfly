//go:build crossfly_full

package jvillage

import (
	"math"

	"github.com/df-mc/dragonfly/server/item"
)

// Offer is Java's MerchantOffer. BuyA's count is the base price; CostA()
// applies demand and the special price (reputation, Hero of the Village).
type Offer struct {
	BuyA, BuyB item.Stack
	Sell       item.Stack
	Uses       int
	MaxUses    int
	RewardExp  bool
	// SpecialPriceDiff is added to the price (negative for discounts).
	SpecialPriceDiff int
	Demand           int
	PriceMultiplier  float32
	XP               int
	// IgnoreDiscounts is Paper's per-offer flag (never set by vanilla data).
	IgnoreDiscounts bool
}

// NewOffer is new MerchantOffer(baseCostA, costB, result, maxUses, xp,
// priceMultiplier): no uses, rewards experience.
func NewOffer(buyA, buyB, sell item.Stack, maxUses, xp int, priceMultiplier float32) *Offer {
	return &Offer{BuyA: buyA, BuyB: buyB, Sell: sell, MaxUses: maxUses, RewardExp: true, XP: xp, PriceMultiplier: priceMultiplier}
}

func floorF(x float64) int { return int(math.Floor(x)) }

// modifiedCount is MerchantOffer.getModifiedCostCount for cost A.
func (o *Offer) modifiedCount() int {
	base := o.BuyA.Count()
	// Mth.floor(basePrice * demand * priceMultiplier) in float arithmetic.
	demandDiff := max(0, floorF(float64(float32(base*o.Demand)*o.PriceMultiplier)))
	maxStack := 64
	if !o.BuyA.Empty() {
		maxStack = o.BuyA.MaxCount()
	}
	return min(max(base+demandDiff+o.SpecialPriceDiff, 1), maxStack)
}

// CostA is MerchantOffer.getCostA: the first payment after demand and
// special prices, clamped to [1, max stack size].
func (o *Offer) CostA() item.Stack {
	if o.BuyA.Empty() {
		return o.BuyA
	}
	return o.BuyA.Grow(o.modifiedCount() - o.BuyA.Count())
}

// CostB is MerchantOffer.getCostB.
func (o *Offer) CostB() item.Stack { return o.BuyB }

// UpdateDemand is MerchantOffer.updateDemand (Paper's
// prevent-negative-villager-demand is false by default).
func (o *Offer) UpdateDemand() {
	o.Demand = o.Demand + o.Uses - (o.MaxUses - o.Uses)
	if preventNegativeDemand {
		o.Demand = max(0, o.Demand)
	}
}

// preventNegativeDemand is Paper's misc.prevent-negative-villager-demand.
var preventNegativeDemand = false

// ResetUses, IncreaseUses, SetToOutOfStock, IsOutOfStock and NeedsRestock
// are the MerchantOffer methods of the same names.
func (o *Offer) ResetUses()            { o.Uses = 0 }
func (o *Offer) IncreaseUses()         { o.Uses++ }
func (o *Offer) SetToOutOfStock()      { o.Uses = o.MaxUses }
func (o *Offer) IsOutOfStock() bool    { return o.Uses >= o.MaxUses }
func (o *Offer) NeedsRestock() bool    { return o.Uses > 0 }
func (o *Offer) AddSpecialPrice(d int) { o.SpecialPriceDiff += d }

// sameItem is ItemCost.test: the same item (Java compares the item and the
// exact components the cost names; Crossfly compares the item value, which
// carries potion type and similar data, and ignores names and enchantments
// as vanilla costs never name them).
func sameItem(have, want item.Stack) bool {
	if have.Empty() || want.Empty() {
		return false
	}
	hn, hm := have.Item().EncodeItem()
	wn, wm := want.Item().EncodeItem()
	return hn == wn && hm == wm
}

// SatisfiedBy is MerchantOffer.satisfiedBy.
func (o *Offer) SatisfiedBy(a, b item.Stack) bool {
	if !sameItem(a, o.BuyA) || a.Count() < o.modifiedCount() {
		return false
	}
	if o.BuyB.Empty() {
		return b.Empty()
	}
	return sameItem(b, o.BuyB) && b.Count() >= o.BuyB.Count()
}

// Take is MerchantOffer.take: the payments left after one trade, or false.
func (o *Offer) Take(a, b item.Stack) (item.Stack, item.Stack, bool) {
	if !o.SatisfiedBy(a, b) {
		return a, b, false
	}
	a = a.Grow(-o.CostA().Count())
	if !o.BuyB.Empty() {
		b = b.Grow(-o.BuyB.Count())
	}
	return a, b, true
}

// Copy returns a copy of the offer.
func (o *Offer) Copy() *Offer { c := *o; return &c }

// Offers is Java's MerchantOffers.
type Offers []*Offer

// NBT encodes the offers as Java's "Offers" compound ({Recipes: [...]}),
// with the item stacks in Dragonfly's item NBT.
func (os Offers) NBT() map[string]any {
	recipes := make([]any, 0, len(os))
	for _, o := range os {
		m := map[string]any{
			"buy": item.WriteNBT(o.BuyA, true), "sell": item.WriteNBT(o.Sell, true),
			"uses": int32(o.Uses), "maxUses": int32(o.MaxUses), "rewardExp": boolByte(o.RewardExp),
			"specialPrice": int32(o.SpecialPriceDiff), "demand": int32(o.Demand),
			"priceMultiplier": o.PriceMultiplier, "xp": int32(o.XP),
		}
		if !o.BuyB.Empty() {
			m["buyB"] = item.WriteNBT(o.BuyB, true)
		}
		if o.IgnoreDiscounts {
			m["Paper.IgnoreDiscounts"] = uint8(1)
		}
		recipes = append(recipes, m)
	}
	return map[string]any{"Recipes": recipes}
}

func boolByte(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}

// OffersFromNBT decodes an "Offers" compound; field defaults follow
// MerchantOffer.CODEC (maxUses 4, rewardExp true, xp 1).
func OffersFromNBT(m map[string]any) Offers {
	l, _ := m["Recipes"].([]any)
	var out Offers
	for _, x := range l {
		c, _ := x.(map[string]any)
		if c == nil {
			continue
		}
		buy, _ := c["buy"].(map[string]any)
		sell, _ := c["sell"].(map[string]any)
		o := &Offer{BuyA: item.ReadNBT(buy, nil), Sell: item.ReadNBT(sell, nil), MaxUses: 4, RewardExp: true, XP: 1}
		if o.BuyA.Empty() {
			continue
		}
		if b, ok := c["buyB"].(map[string]any); ok {
			o.BuyB = item.ReadNBT(b, nil)
		}
		o.Uses = int(i32(c["uses"], 0))
		o.MaxUses = int(i32(c["maxUses"], 4))
		if v, ok := c["rewardExp"].(uint8); ok {
			o.RewardExp = v != 0
		}
		o.SpecialPriceDiff = int(i32(c["specialPrice"], 0))
		o.Demand = int(i32(c["demand"], 0))
		if f, ok := c["priceMultiplier"].(float32); ok {
			o.PriceMultiplier = f
		}
		o.XP = int(i32(c["xp"], 1))
		if v, ok := c["Paper.IgnoreDiscounts"].(uint8); ok {
			o.IgnoreDiscounts = v != 0
		}
		out = append(out, o)
	}
	return out
}

func i32(v any, def int32) int32 {
	if x, ok := v.(int32); ok {
		return x
	}
	return def
}
