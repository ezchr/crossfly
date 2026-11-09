//go:build crossfly_full

package village

import (
	"math"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	vc "github.com/df-mc/dragonfly/server/mob/component/village"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// tradeWithPlayer is minecraft:behavior.trade_with_player (BDS
// TradeWithPlayerGoal): while a player trades with the mob (and it was not
// hurt, and filters pass) it stands still; it stops beyond
// max_distance_from_player.
type tradeWithPlayer struct {
	mob.GoalBase
	maxDistSq float64
	filter    mob.Filter
}

func newTradeWithPlayer(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		MaxDist *float64 `json:"max_distance_from_player"`
		Filters jsonRaw  `json:"filters"`
	}{}
	if err := decode(spec, &j); err != nil {
		return nil, err
	}
	g := &tradeWithPlayer{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagJump)}, maxDistSq: 64, filter: compile(j.Filters)}
	if j.MaxDist != nil {
		g.maxDistSq = *j.MaxDist * *j.MaxDist
	}
	return g, nil
}

func tradingPlayer(m *mob.Mob) (*player.Player, bool) {
	e, ok := vc.EconomyOf(m)
	if !ok {
		return nil, false
	}
	return e.Trading(m.Tx())
}

func (g *tradeWithPlayer) CanUse(m *mob.Mob) bool {
	if !hasNav(m) {
		return false
	}
	if _, ok := tradingPlayer(m); !ok {
		return false
	}
	if _, t, ok := m.LastHurtBy(); ok && m.Ticks()-t < 60 {
		return false
	}
	return pass(g.filter, m)
}

func (g *tradeWithPlayer) CanContinueToUse(m *mob.Mob) bool {
	p, ok := tradingPlayer(m)
	return ok && g.CanUse(m) && distSq(p.Position(), m.Position()) <= g.maxDistSq
}

func (g *tradeWithPlayer) Start(m *mob.Mob) { m.Navigation().Stop(m) }

// lookAtTradingPlayer is minecraft:behavior.look_at_trading_player (BDS
// LookAtTradingPlayerGoal): look at the trading player.
type lookAtTradingPlayer struct {
	mob.GoalBase
	target *world.EntityHandle
}

func newLookAtTradingPlayer(spec mob.GoalSpec) (mob.Goal, error) {
	return &lookAtTradingPlayer{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagLook)}}, nil
}

func (g *lookAtTradingPlayer) CanUse(m *mob.Mob) bool {
	p, ok := tradingPlayer(m)
	if !ok {
		return false
	}
	g.target = p.H()
	return true
}

func (g *lookAtTradingPlayer) Tick(m *mob.Mob) {
	if e, ok := g.target.Entity(m.Tx()); ok {
		lookAt(m, e, 10, maxHeadXRot)
	}
}

func (g *lookAtTradingPlayer) Stop(*mob.Mob) { g.target = nil }

// tradeInterest is minecraft:behavior.trade_interest (BDS
// TradeInterestGoal): a nearby player holding an item an unlocked offer
// wants makes the trader hold up what it would give for it, cycling
// through the matching offers every carried_item_switch_time seconds
// unless the player looks at it.
type tradeInterest struct {
	mob.GoalBase
	radius                          float64
	interest, half, remove, switch_ int
	cooldown                        int

	cooldownCtr                       int
	interestCtr, switchCtr, removeCtr int
	player                            *world.EntityHandle
	idx                               int
	shown                             item.Stack
}

func newTradeInterest(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Radius   *float64 `json:"within_radius"`
		Interest *float64 `json:"interest_time"`
		Remove   *float64 `json:"remove_item_time"`
		Switch   *float64 `json:"carried_item_switch_time"`
		Cooldown *float64 `json:"cooldown"`
	}{}
	if err := decode(spec, &j); err != nil {
		return nil, err
	}
	sec := func(p *float64, d float64) int {
		if p != nil {
			return int(*p * 20)
		}
		return int(d * 20)
	}
	g := &tradeInterest{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)},
		interest: sec(j.Interest, 45), remove: sec(j.Remove, 1), switch_: sec(j.Switch, 2), cooldown: sec(j.Cooldown, 2)}
	if j.Radius != nil {
		g.radius = *j.Radius
	}
	g.half = g.interest / 2
	g.cooldownCtr = g.cooldown
	return g, nil
}

// matching returns the offers (indices) an item would pay for.
func matching(m *mob.Mob, held item.Stack) []int {
	e, ok := vc.EconomyOf(m)
	if !ok || held.Empty() {
		return nil
	}
	var out []int
	tier := int(m.Meta().TradeTier)
	for i, o := range e.Offers() {
		if !o.OutOfUses() && o.Tier <= tier && o.BuyA.Comparable(held) {
			out = append(out, i)
		}
	}
	return out
}

func heldOf(p *player.Player) item.Stack {
	mh, _ := p.HeldItems()
	return mh
}

func (g *tradeInterest) CanUse(m *mob.Mob) bool {
	if g.cooldownCtr < g.cooldown {
		g.cooldownCtr++
		return false
	}
	e, ok := vc.EconomyOf(m)
	if !ok || len(e.Offers()) == 0 {
		return false
	}
	for _, o := range nearby(m, g.radius, g.radius) {
		p, ok := o.(*player.Player)
		if !ok || !p.GameMode().Visible() {
			continue
		}
		if ms := matching(m, heldOf(p)); len(ms) > 0 {
			g.player, g.idx = p.H(), 0
			g.show(m, e.Offers()[ms[0]].Sell)
			g.switchCtr, g.removeCtr = 0, 0
			return true
		}
	}
	g.cooldownCtr = 0
	return false
}

func (g *tradeInterest) show(m *mob.Mob, s item.Stack) {
	g.shown = s
	_, off := m.HeldItems()
	m.SetHeldItems(s, off)
}

func (g *tradeInterest) Start(m *mob.Mob) {
	m.Navigation().Stop(m)
	m.SetActorFlag(vc.FlagTradeInterest, true)
}

// looking reports whether p looks at m's head (TradeInterestGoal).
func looking(m *mob.Mob, p *player.Player) bool {
	head := entity.EyePosition(p)
	to := m.EyePosition().Add(mgl64.Vec3{0, 0.343}).Sub(head)
	d := to.Len()
	if d == 0 {
		return true
	}
	return p.Rotation().Vec3().Dot(to.Normalize()) > 1-0.02/d
}

func (g *tradeInterest) CanContinueToUse(m *mob.Mob) bool {
	pe, ok := g.player.Entity(m.Tx())
	if !ok {
		return false
	}
	p, ok := pe.(*player.Player)
	if !ok || p.Dead() || g.interestCtr > g.interest || math.Sqrt(distSq(p.Position(), m.Position())) > g.radius+1 {
		return false
	}
	held := heldOf(p)
	ms := matching(m, held)
	if held.Empty() || len(ms) == 0 {
		g.removeCtr++
		return g.removeCtr <= g.remove
	}
	g.interestCtr++
	e, _ := vc.EconomyOf(m)
	look := looking(m, p)
	if look {
		g.interestCtr = min(g.interestCtr, g.half)
	} else if g.switchCtr++; g.switchCtr >= g.switch_ {
		g.idx = (g.idx + 1) % len(ms)
		g.switchCtr = 0
	}
	sell := e.Offers()[ms[g.idx%len(ms)]].Sell
	if !sell.Equal(g.shown) {
		g.show(m, sell)
		g.switchCtr, g.removeCtr = 0, 0
	}
	return true
}

func (g *tradeInterest) Tick(m *mob.Mob) {
	if pe, ok := g.player.Entity(m.Tx()); ok {
		lookAt(m, pe, 60, maxHeadXRot)
	}
}

func (g *tradeInterest) Stop(m *mob.Mob) {
	g.player = nil
	g.show(m, item.Stack{})
	m.SetActorFlag(vc.FlagTradeInterest, false)
	g.interestCtr, g.switchCtr, g.cooldownCtr = 0, 0, 0
}
