package javasession

import (
	"github.com/df-mc/dragonfly/server/item"
	jitem "github.com/df-mc/dragonfly/server/java/protocol/item"
	"github.com/df-mc/dragonfly/server/java/protocol/text"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/java/protocol/version"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
)

// Villager trading on Java: the merchant window (inputs in UI slots 4 and 5,
// as on Bedrock), for the server/mob villagers, which open it through
// Session.OpenTradeWindow like the Bedrock session.

// MerchantOffer is one trade of a merchant window.
type MerchantOffer struct {
	In1, In2, Out                   item.Stack
	Uses, MaxUses, XP, SpecialPrice int32
	Demand                          int32
	Multiplier                      float32
	Disabled                        bool
}

// merchantState is the open merchant window's own state.
type merchantState struct {
	offers     []MerchantOffer
	level, exp int32
	regular    bool
	window     *session.TradeWindow
	onTrade    func(index, times int) bool
	// index maps the offers shown (unlocked tiers) to the trade window's.
	index []int
}

// merchantOffers converts a trade window's offers up to its tier. In1 is
// the base price; SpecialPrice the discount or markup on top.
func merchantOffers(w session.TradeWindow) ([]MerchantOffer, []int) {
	var out []MerchantOffer
	var idx []int
	for i, o := range w.Offers {
		if o.Tier > w.Tier {
			continue
		}
		mo := MerchantOffer{In1: o.Buy, In2: o.Buy2, Out: o.Sell, Uses: int32(o.Uses), MaxUses: int32(o.MaxUses),
			XP: int32(o.TraderExp), Demand: int32(o.Demand), Multiplier: float32(o.PriceMultiplier)}
		if o.BaseBuyCount > 0 && !o.Buy.Empty() {
			mo.SpecialPrice = int32(o.Buy.Count() - o.BaseBuyCount)
			mo.In1 = o.Buy.Grow(o.BaseBuyCount - o.Buy.Count())
		}
		if mo.MaxUses <= 0 {
			mo.MaxUses = 1 << 30
		}
		mo.Disabled = mo.Uses >= mo.MaxUses
		out = append(out, mo)
		idx = append(idx, i)
	}
	return out, idx
}

// OpenTrade opens a plain trading window (no levels).
func (s *Session) OpenTrade(tx *world.Tx, trader world.Entity, name string, offers []session.TradeOffer, onTrade func(index int, times int) bool) {
	s.OpenTradeWindow(tx, trader, session.TradeWindow{Name: name, Offers: offers, Tier: 1 << 20}, onTrade)
}

// UpdateTradeOffers replaces the offers of the open trading window.
func (s *Session) UpdateTradeOffers(offers []session.TradeOffer) {
	m := s.items().open.Load()
	if m == nil || m.merchant == nil || m.merchant.window == nil {
		return
	}
	w := *m.merchant.window
	w.Offers = offers
	s.UpdateTradeWindow(w)
}

// OpenTradeWindow opens the merchant window for a trading window with
// levels.
func (s *Session) OpenTradeWindow(tx *world.Tx, _ world.Entity, w session.TradeWindow, onTrade func(index, times int) bool) {
	st := s.items()
	_, p := st.current()
	if p == nil || st.inv == nil {
		return
	}
	s.closeMenu(tx, p, true)
	cp := w
	offers, idx := merchantOffers(cp)
	m := &menu{kind: menuMerchant, size: 3, sel: -1, w: tx.World(), virtual: true,
		typ: v777.BuiltinID("minecraft:menu", "minecraft:merchant")}
	m.merchant = &merchantState{offers: offers, index: idx, level: int32(cp.Tier + 1), exp: int32(cp.Experience),
		regular: cp.Economic, window: &cp, onTrade: onTrade}
	m.onClose = func() {
		if m.merchant.window != nil && m.merchant.window.OnClose != nil {
			m.merchant.window.OnClose()
		}
	}
	if cp.Tier >= 1<<20 {
		m.merchant.level = 0
	}
	m.id = st.nextWindowID()
	st.drag = dragState{}
	st.open.Store(m)
	wr := s.packet()
	wr.VarInt(m.id)
	wr.VarInt(version.Map(st.menus, m.typ))
	t := merchantTitle(cp.Name)
	t.Write(wr)
	s.queue(v777.ClientboundPlayOpenScreen, wr)
	s.sendMerchantOffers(m)
	s.syncWindow(tx, p, m)
}

// UpdateTradeWindow refreshes the open merchant window.
func (s *Session) UpdateTradeWindow(w session.TradeWindow) {
	m := s.items().open.Load()
	if m == nil || m.merchant == nil {
		return
	}
	cp := w
	if old := m.merchant.window; old != nil {
		if cp.OnClose == nil {
			cp.OnClose = old.OnClose
		}
		if cp.OnTradeTx == nil {
			cp.OnTradeTx = old.OnTradeTx
		}
	}
	m.merchant.window = &cp
	m.merchant.offers, m.merchant.index = merchantOffers(cp)
	m.merchant.level, m.merchant.exp = int32(cp.Tier+1), int32(cp.Experience)
	s.sendMerchantOffers(m)
}

// CloseTradeWindow closes the merchant window.
func (s *Session) CloseTradeWindow(tx *world.Tx) {
	m := s.items().open.Load()
	if m == nil || m.merchant == nil {
		return
	}
	if _, c := s.items().current(); c != nil {
		s.closeMenu(tx, c, false)
	}
}

func (s *Session) sendMerchantOffers(m *menu) {
	st := s.items()
	w := s.packet()
	w.VarInt(m.id)
	ms := m.merchant
	w.VarInt(int32(len(ms.offers)))
	cost := func(it item.Stack) {
		var js jitem.Stack
		javaStack(it, &js)
		w.VarInt(st.proto.Item(js.ID))
		w.VarInt(int32(max(it.Count(), 1)))
		w.VarInt(0) // no component predicates
	}
	for _, o := range ms.offers {
		cost(o.In1)
		s.writeStack(w, o.Out)
		w.Bool(!o.In2.Empty())
		if !o.In2.Empty() {
			cost(o.In2)
		}
		w.Bool(o.Disabled)
		w.Int32(o.Uses)
		w.Int32(o.MaxUses)
		w.Int32(o.XP)
		w.Int32(o.SpecialPrice)
		w.Float32(o.Multiplier)
		w.Int32(o.Demand)
	}
	w.VarInt(ms.level)
	w.VarInt(ms.exp)
	w.Bool(ms.regular)
	w.Bool(ms.regular)
	s.queue(v777.ClientboundPlayMerchantOffers, w)
}

// MerchantSelect handles the player picking a trade (select_trade): the
// inputs are filled from the inventory, as vanilla does.
func (s *Session) MerchantSelect(i int) {
	s.do(func(tx *world.Tx, c session.Controllable) {
		st := s.items()
		m := st.open.Load()
		if m == nil || m.merchant == nil || i < 0 || i >= len(m.merchant.offers) {
			return
		}
		m.sel = i
		o := m.merchant.offers[i]
		fill := func(slot int, want item.Stack) {
			cur, _ := st.ui.Item(slot)
			if want.Empty() {
				return
			}
			if !cur.Empty() && !cur.Comparable(want) {
				if _, err := st.inv.AddItem(cur); err != nil {
					return
				}
				cur = item.Stack{}
			}
			need := want.Count() - cur.Count()
			for j := 0; j < st.inv.Size() && need > 0; j++ {
				it, _ := st.inv.Item(j)
				if it.Empty() || !it.Comparable(want) {
					continue
				}
				n := min(need, it.Count())
				_ = st.inv.SetItem(j, it.Grow(-n))
				if cur.Empty() {
					cur = it.Grow(n - it.Count())
				} else {
					cur = cur.Grow(n)
				}
				need -= n
			}
			_ = st.ui.SetItem(slot, cur)
		}
		fill(4, price(o))
		fill(5, o.In2)
		s.syncWindow(tx, c, m)
	})
}

// price is what offer o costs in its first slot (base + special price).
func price(o MerchantOffer) item.Stack {
	if o.In1.Empty() {
		return o.In1
	}
	return o.In1.Grow(int(o.SpecialPrice))
}

// merchantResult is the selected trade's output if the inputs pay for it.
func merchantResult(m *menu, in1, in2 item.Stack) item.Stack {
	if m.merchant == nil || m.sel < 0 || m.sel >= len(m.merchant.offers) {
		return item.Stack{}
	}
	o := m.merchant.offers[m.sel]
	if o.Disabled {
		return item.Stack{}
	}
	pays := func(have, want item.Stack) bool {
		if want.Empty() {
			return true
		}
		return !have.Empty() && have.Comparable(want) && have.Count() >= want.Count()
	}
	if !pays(in1, price(o)) || !pays(in2, o.In2) {
		return item.Stack{}
	}
	return o.Out
}

// merchantPay takes a trade's price from the input slots and tells the
// trader.
func merchantPay(tx *world.Tx, m *menu, slots []item.Stack) {
	ms := m.merchant
	if ms == nil || m.sel < 0 || m.sel >= len(ms.offers) {
		return
	}
	o := ms.offers[m.sel]
	slots[0] = slots[0].Grow(-price(o).Count())
	if !o.In2.Empty() {
		slots[1] = slots[1].Grow(-o.In2.Count())
	}
	idx := m.sel
	if m.sel < len(ms.index) {
		idx = ms.index[m.sel]
	}
	switch {
	case ms.window != nil && ms.window.OnTradeTx != nil:
		ms.window.OnTradeTx(tx, idx, 1)
	case ms.onTrade != nil:
		ms.onTrade(idx, 1)
	}
	ms.offers[m.sel].Uses++
	ms.offers[m.sel].Disabled = ms.offers[m.sel].Uses >= ms.offers[m.sel].MaxUses
}

// bedrockTraderKeys maps the Bedrock language keys the trader files use as display_name to the
// Java translation keys (the Java client shows a key it does not know as is).
var bedrockTraderKeys = map[string]string{
	"entity.villager.armor":        "entity.minecraft.villager.armorer",
	"entity.villager.butcher":      "entity.minecraft.villager.butcher",
	"entity.villager.cartographer": "entity.minecraft.villager.cartographer",
	"entity.villager.cleric":       "entity.minecraft.villager.cleric",
	"entity.villager.farmer":       "entity.minecraft.villager.farmer",
	"entity.villager.fisherman":    "entity.minecraft.villager.fisherman",
	"entity.villager.fletcher":     "entity.minecraft.villager.fletcher",
	"entity.villager.leather":      "entity.minecraft.villager.leatherworker",
	"entity.villager.librarian":    "entity.minecraft.villager.librarian",
	"entity.villager.mason":        "entity.minecraft.villager.mason",
	"entity.villager.shepherd":     "entity.minecraft.villager.shepherd",
	"entity.villager.tool":         "entity.minecraft.villager.toolsmith",
	"entity.villager.weapon":       "entity.minecraft.villager.weaponsmith",
	"entity.wandering_trader.name": "entity.minecraft.wandering_trader",
}

// merchantTitle is the merchant screen title for a trade window name: a Bedrock trader key is
// sent as the matching Java translation, anything else as text.
func merchantTitle(name string) text.Component {
	if key, ok := bedrockTraderKeys[name]; ok {
		return text.Translatable(key)
	}
	return bedrockText(name)
}
