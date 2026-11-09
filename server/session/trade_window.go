package session

import (
	"github.com/df-mc/dragonfly/server/world"
)

// TradeWindow is a villager trading window with levels: the offers of every
// tier (the client shows those up to Tier), the experience each tier needs
// and the trader's experience. Not part of upstream Dragonfly; added for the
// definition-driven villagers in server/mob.
type TradeWindow struct {
	Name   string
	Offers []TradeOffer
	// Tier is the trader's current tier (0 novice .. 4 master); TierExp the
	// total experience each tier needs (TierExpRequirements).
	Tier       int
	TierExp    []int
	Experience int
	// Economic marks demand based prices (economy trade tables).
	Economic bool
	// OnClose runs when the window closes, from whichever goroutine closes
	// it: keep it to setting a flag.
	OnClose func()
	// OnTradeTx, when set, is called instead of the onTrade callback with
	// the transaction the trade is handled in.
	OnTradeTx func(tx *world.Tx, index, times int) bool
}

// OpenTradeWindow opens a trading window with levels for the session.
func (s *Session) OpenTradeWindow(tx *world.Tx, trader world.Entity, w TradeWindow, onTrade func(index, times int) bool) {
	s.OpenTrade(tx, trader, w.Name, w.Offers, onTrade)
	if st := s.trade.Load(); st != nil {
		cp := w
		st.window = &cp
		s.sendTradeOffers(st, byte(s.openedWindowID.Load()))
	}
}

// UpdateTradeWindow replaces the offers and level of the open trading
// window, if any.
func (s *Session) UpdateTradeWindow(w TradeWindow) {
	old := s.trade.Load()
	if old == nil {
		return
	}
	cp := w
	if cp.OnClose == nil && old.window != nil {
		cp.OnClose = old.window.OnClose
	}
	if cp.OnTradeTx == nil && old.window != nil {
		cp.OnTradeTx = old.window.OnTradeTx
	}
	st := &tradeState{opened: old.opened, traderRID: old.traderRID, traderID: old.traderID, name: w.Name,
		offers: append([]TradeOffer(nil), w.Offers...), onTrade: old.onTrade, window: &cp}
	s.trade.Store(st)
	s.sendTradeOffers(st, byte(s.openedWindowID.Load()))
}

// CloseTradeWindow closes the open trading window, if any.
func (s *Session) CloseTradeWindow(tx *world.Tx) {
	if s.trade.Load() == nil {
		return
	}
	s.closeCurrentContainer(tx, false)
}
