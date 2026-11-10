//go:build crossfly_full

package jvillage

import (
	"math"
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
)

// tradeSession is the open trading screen of a merchant.
type tradeSession struct {
	player *world.EntityHandle
	closed atomic.Bool
}

// merchant is AbstractVillager's trading state, shared by villagers and
// wandering traders.
type merchant struct {
	offers           Offers
	hasOffers        bool
	tradingPlayer    *world.EntityHandle
	trade            *tradeSession
	lastTradedPlayer *world.EntityHandle
}

// merchantHost is what a merchant's mob class adds: its trades, the
// experience reward and the screen's level bar.
type merchantHost interface {
	mstate() *merchant
	updateTrades(m *javamob.Mob, amount int) bool
	rewardTradeXp(m *javamob.Mob, o *Offer)
	// screen is the title, level (1-5), experience and whether the level
	// bar shows.
	screen() (title string, level, xp int, progress bool)
}

func (mc *merchant) mstate() *merchant { return mc }

// offersOf is AbstractVillager.getOffers: generated on first use.
func offersOf(h merchantHost, m *javamob.Mob) Offers {
	mc := h.mstate()
	if !mc.hasOffers {
		mc.offers, mc.hasOffers = nil, true
		h.updateTrades(m, -1)
	}
	return mc.offers
}

// Offers returns the villager's offers, generating them first.
func (v *Villager) Offers(m *javamob.Mob) Offers { return offersOf(v, m) }

// SetOffers replaces the offers (Villager.setOffers).
func (mc *merchant) SetOffers(o Offers) { mc.offers, mc.hasOffers = o, true }

// Trading is AbstractVillager.isTrading.
func (mc *merchant) Trading() bool { return mc.tradingPlayer != nil }

func (mc *merchant) tradingPlayerEntity(m *javamob.Mob) (*player.Player, bool) {
	if mc.tradingPlayer == nil {
		return nil, false
	}
	e, ok := mc.tradingPlayer.Entity(m.Tx())
	if !ok {
		return nil, false
	}
	p, ok := e.(*player.Player)
	return p, ok
}

// updateTrades is Villager.updateTrades(level, amount): the trade set of the
// current profession and level.
func (v *Villager) updateTrades(m *javamob.Mob, amount int) bool {
	set, ok := v.profession().TradeSet(v.Data.Level)
	if !ok {
		return false
	}
	if !AddOffersFromTradeSet(&v.offers, set, amount, v.Data.Type, m.Rand()) {
		return false
	}
	if p, ok := v.tradingPlayerEntity(m); ok {
		v.updateSpecialPrices(m, p)
	}
	return true
}

func (v *Villager) screen() (string, int, int, bool) {
	return professionName(v.profession()), v.Data.Level, v.xp, true
}

// MobInteract is Villager.mobInteract: babies shake their heads, villagers
// without offers too; others open the trading screen.
func (v *Villager) MobInteract(m *javamob.Mob, user item.User, held item.Stack) (item.Stack, bool) {
	p, ok := user.(*player.Player)
	if !ok || isVillagerSpawnEgg(held) || m.Dead() || v.Trading() || v.sleeping {
		return held, false
	}
	if v.Baby() {
		v.SetUnhappy(m)
		return held, true
	}
	if len(v.Offers(m)) == 0 {
		v.SetUnhappy(m)
		return held, true
	}
	v.updateSpecialPrices(m, p)
	startTrading(v, m, p)
	return held, true
}

func isVillagerSpawnEgg(s item.Stack) bool {
	if s.Empty() {
		return false
	}
	n, _ := s.Item().EncodeItem()
	return n == "minecraft:villager_spawn_egg"
}

// startTrading is setTradingPlayer + openTradingScreen.
func startTrading(h merchantHost, m *javamob.Mob, p *player.Player) {
	mc := h.mstate()
	mc.tradingPlayer = p.H()
	ts := &tradeSession{player: p.H()}
	mc.trade = ts
	w := window(h, m)
	w.OnClose = func() { ts.closed.Store(true) }
	id := m.H()
	w.OnTradeTx = func(tx *world.Tx, index, times int) bool {
		vm, ok := javamob.OfHandle(tx, id)
		if !ok {
			return false
		}
		hh, ok := vm.Behaviour().(merchantHost)
		if !ok || hh.mstate().trade != ts {
			return false
		}
		return processTrades(hh, vm, index, times)
	}
	p.OpenTradeWindow(m, w, nil)
}

// window builds the trading screen of the current offers. Prices go out as
// the cost after demand and special prices (getCostA) with the base price
// beside it, so both clients show what Java shows.
func window(h merchantHost, m *javamob.Mob) session.TradeWindow {
	title, level, xp, progress := h.screen()
	w := session.TradeWindow{Name: title, Tier: max(0, level-1), Experience: xp}
	if progress {
		w.TierExp = []int{0, 10, 70, 150, 250}
	}
	for _, o := range offersOf(h, m) {
		w.Offers = append(w.Offers, session.TradeOffer{
			Buy: o.CostA(), Buy2: o.BuyB, Sell: o.Sell, Uses: o.Uses, MaxUses: o.MaxUses,
			TraderExp: o.XP, RewardExp: o.RewardExp, BaseBuyCount: o.BuyA.Count(),
			PriceMultiplier: float64(o.PriceMultiplier),
		})
	}
	return w
}

func professionName(p *Profession) string {
	if p == nil || p == ProfNone {
		return "Villager"
	}
	n := []byte(p.Name)
	n[0] -= 'a' - 'A'
	return string(n)
}

// refreshWindow sends the offers again (sendMerchantOffers).
func refreshWindow(h merchantHost, m *javamob.Mob) {
	mc := h.mstate()
	if p, ok := mc.tradingPlayerEntity(m); ok && mc.trade != nil {
		p.UpdateTradeWindow(window(h, m))
	}
}

// processTrades runs AbstractVillager.processTrade for each trade done.
func processTrades(h merchantHost, m *javamob.Mob, index, times int) bool {
	offers := offersOf(h, m)
	if index < 0 || index >= len(offers) {
		return false
	}
	o := offers[index]
	for range max(times, 1) {
		if o.IsOutOfStock() {
			break
		}
		o.IncreaseUses()
		h.rewardTradeXp(m, o)
		m.PlaySound("yes") // notifyTrade
	}
	refreshWindow(h, m)
	return true
}

// rewardTradeXp is Villager.rewardTradeXp.
func (v *Villager) rewardTradeXp(m *javamob.Mob, o *Offer) {
	pop := 3 + m.Rand().IntN(4)
	v.xp += o.XP
	v.lastTradedPlayer = v.tradingPlayer
	if v.shouldIncreaseLevel() {
		v.increaseMerchantCareer(m)
		m.AddEffect(effect.New(effect.Regeneration, 1, ticksDuration(200)))
		pop += 5
	}
	if o.RewardExp {
		spawnXP(m, pop)
	}
}

// shouldIncreaseLevel is Villager.shouldIncreaseLevel.
func (v *Villager) shouldIncreaseLevel() bool {
	return CanLevelUp(v.Data.Level) && v.xp >= MaxXPPerLevel(v.Data.Level)
}

// increaseMerchantCareer is Villager.increaseMerchantCareer.
func (v *Villager) increaseMerchantCareer(m *javamob.Mob) {
	v.Data.Level++
	v.syncData(m)
	v.updateTrades(m, -1)
}

// stopTrading is AbstractVillager.stopTrading; villagers also drop their
// special prices (Villager.resetSpecialPrices).
func stopTrading(h merchantHost, m *javamob.Mob) {
	mc := h.mstate()
	if p, ok := mc.tradingPlayerEntity(m); ok && mc.trade != nil && !mc.trade.closed.Load() {
		p.CloseTradeWindow()
	}
	mc.tradingPlayer, mc.trade = nil, nil
	if _, ok := h.(*Villager); ok {
		for _, o := range mc.offers {
			o.SpecialPriceDiff = 0
		}
	}
}

func (v *Villager) stopTrading(m *javamob.Mob) { stopTrading(v, m) }

// checkTradeScreen ends the trade when the screen closed or the player left
// (MerchantMenu.stillValid).
func checkTradeScreen(h merchantHost, m *javamob.Mob) {
	mc := h.mstate()
	if mc.tradingPlayer == nil {
		return
	}
	p, ok := mc.tradingPlayerEntity(m)
	if !ok || mc.trade == nil || mc.trade.closed.Load() || p.Dead() || m.Dead() || p.Position().Sub(m.Position()).Len() > 8 {
		stopTrading(h, m)
	}
}

// updateSpecialPrices is Villager.updateSpecialPrices: reputation and Hero
// of the Village discounts for the player.
func (v *Villager) updateSpecialPrices(m *javamob.Mob, p *player.Player) {
	offers := v.Offers(m)
	for _, o := range offers {
		o.SpecialPriceDiff = 0
	}
	rep := v.gossips.Reputation(p.UUID(), nil)
	hero := 0.0
	if amp, ok := HeroAmplifier(p); ok {
		hero = float64(float32(0.3) + float32(0.0625)*float32(amp))
	}
	for _, o := range offers {
		if o.IgnoreDiscounts {
			continue
		}
		if rep != 0 {
			o.AddSpecialPrice(-floorF(float64(float32(rep) * o.PriceMultiplier)))
		}
		if hero > 0 {
			cut := int(math.Floor(hero * float64(o.BuyA.Count())))
			o.AddSpecialPrice(-max(cut, 1))
		}
	}
	if v.trade != nil {
		refreshWindow(v, m)
	}
}

// Restock is Villager.restock.
func (v *Villager) Restock(m *javamob.Mob) {
	v.updateDemand()
	for _, o := range v.offers {
		o.ResetUses()
	}
	if p, ok := v.tradingPlayerEntity(m); ok {
		v.updateSpecialPrices(m, p)
	}
	v.lastRestock = m.Tx().CurrentTick()
	v.restocksToday++
}

func (v *Villager) needsToRestock() bool {
	for _, o := range v.offers {
		if o.NeedsRestock() {
			return true
		}
	}
	return false
}

// allowedToRestock is Villager.allowedToRestock.
func (v *Villager) allowedToRestock(now int64) bool {
	return v.restocksToday == 0 || v.restocksToday < 2 && now > v.lastRestock+2400
}

// ShouldRestock is Villager.shouldRestock.
func (v *Villager) ShouldRestock(m *javamob.Mob) bool {
	tx := m.Tx()
	now := tx.CurrentTick()
	newDay := now > v.lastRestock+12000
	day := int64(tx.World().Time()) / 24000
	newDay = newDay || (v.lastRestockCheckDay > 0 && day > v.lastRestockCheckDay)
	v.lastRestockCheckDay = day
	if newDay {
		v.lastRestock = now
		v.catchUpDemand(m)
		v.restocksToday = 0
	}
	return v.allowedToRestock(now) && v.needsToRestock()
}

// catchUpDemand is Villager.catchUpDemand.
func (v *Villager) catchUpDemand(m *javamob.Mob) {
	missed := 2 - v.restocksToday
	if missed > 0 {
		for _, o := range v.offers {
			o.ResetUses()
		}
	}
	for range missed {
		v.updateDemand()
	}
	if p, ok := v.tradingPlayerEntity(m); ok {
		v.updateSpecialPrices(m, p)
	}
}

// updateDemand is Villager.updateDemand.
func (v *Villager) updateDemand() {
	for _, o := range v.offers {
		o.UpdateDemand()
	}
}
