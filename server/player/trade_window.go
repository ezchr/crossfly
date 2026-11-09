package player

import (
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
)

// Villager trading windows with levels (server/mob villagers). Sessions
// that do not support them get a plain OpenTrade window.

type tradeWindowOpener interface {
	OpenTradeWindow(tx *world.Tx, trader world.Entity, w session.TradeWindow, onTrade func(index, times int) bool)
	UpdateTradeWindow(w session.TradeWindow)
	CloseTradeWindow(tx *world.Tx)
}

// OpenTradeWindow opens a villager trading window with trader.
func (p *Player) OpenTradeWindow(trader world.Entity, w session.TradeWindow, onTrade func(index, times int) bool) {
	s := p.conn()
	if s == session.Nop {
		return
	}
	if o, ok := s.(tradeWindowOpener); ok {
		o.OpenTradeWindow(p.tx, trader, w, onTrade)
		return
	}
	s.OpenTrade(p.tx, trader, w.Name, w.Offers, onTrade)
}

// UpdateTradeWindow refreshes the open trading window (uses, tier).
func (p *Player) UpdateTradeWindow(w session.TradeWindow) {
	if o, ok := p.conn().(tradeWindowOpener); ok {
		o.UpdateTradeWindow(w)
	}
}

// CloseTradeWindow closes the open trading window.
func (p *Player) CloseTradeWindow() {
	if o, ok := p.conn().(tradeWindowOpener); ok {
		o.CloseTradeWindow(p.tx)
	}
}
