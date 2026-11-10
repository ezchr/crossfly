//go:build crossfly_full

package jvillage

import (
	"slices"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/mob/jbrain/jmob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// WanderingTraderID is the wandering trader's identifier.
const WanderingTraderID = "minecraft:wandering_trader"

func init() {
	javamob.Register(javamob.Type{
		ID: WanderingTraderID, Category: javamob.CategoryCreature,
		Width: 0.6, Height: 1.95, EyeHeight: 1.62,
		Attributes: func() map[javamob.Attribute]float64 {
			a := javamob.LivingAttributes()
			a[javamob.AttrFollowRange] = 16
			a[javamob.AttrMovementSpeed] = 0.5
			return a
		},
		Families:    []string{"wandering_trader", "mob"},
		NoLootTable: true,
		New:         func() javamob.Behaviour { return &WanderingTrader{} },
	})
}

// WanderingTrader is Java's WanderingTrader.
type WanderingTrader struct {
	merchant
	// DespawnDelay counts down to the trader leaving (48000 from the spawner).
	DespawnDelay int
	wanderTarget *cube.Pos
	unhappy      int
}

// TraderOf returns the wandering trader behaviour of m.
func TraderOf(m *javamob.Mob) (*WanderingTrader, bool) {
	t, ok := m.Behaviour().(*WanderingTrader)
	return t, ok
}

// SetWanderTarget is WanderingTrader.setWanderTarget.
func (t *WanderingTrader) SetWanderTarget(p *cube.Pos) { t.wanderTarget = p }

// zombieClass are the subclasses of Zombie (AvoidEntityGoal(Zombie.class)).
var zombieClass = []string{"minecraft:zombie", "minecraft:husk", "minecraft:drowned", ZombieVillagerID, "minecraft:zombified_piglin"}

// RegisterGoals is WanderingTrader.registerGoals.
func (t *WanderingTrader) RegisterGoals(m *javamob.Mob) {
	m.Goals().Add(0, goal.NewFloat(m))
	m.Goals().Add(0, &useItem{GoalBase: javamob.GoalBase{}, stack: item.NewStack(item.Potion{Type: potion.Invisibility()}, 1), sound: "disappeared",
		can: func(m *javamob.Mob) bool { return !isBrightOutside(m.Tx()) && !m.Meta().Invisible }})
	m.Goals().Add(0, &useItem{GoalBase: javamob.GoalBase{}, stack: item.NewStack(item.Bucket{Content: item.MilkBucketContent()}, 1), sound: "reappeared",
		can: func(m *javamob.Mob) bool { return isBrightOutside(m.Tx()) && m.Meta().Invisible }})
	m.Goals().Add(1, &tradeWithPlayer{GoalBase: javamob.GoalBase{Control: javamob.FlagJump | javamob.FlagMove}})
	avoid := func(dist float32, ids ...string) javamob.Goal {
		return goal.NewAvoidEntity(func(e world.Entity) bool { return slices.Contains(ids, e.H().Type().EncodeEntity()) }, dist, 0.5, 0.5)
	}
	m.Goals().Add(1, avoid(8, zombieClass...))
	m.Goals().Add(1, avoid(12, "minecraft:evocation_illager", "minecraft:evoker"))
	m.Goals().Add(1, avoid(8, "minecraft:vindicator"))
	m.Goals().Add(1, avoid(8, "minecraft:vex"))
	m.Goals().Add(1, avoid(15, "minecraft:pillager"))
	m.Goals().Add(1, avoid(12, "minecraft:illusioner"))
	m.Goals().Add(1, avoid(10, "minecraft:zoglin"))
	m.Goals().Add(1, newPanic(0.5))
	m.Goals().Add(1, &lookAtTradingPlayer{GoalBase: javamob.GoalBase{Control: javamob.FlagLook | javamob.FlagMove}})
	m.Goals().Add(2, &wanderToPosition{GoalBase: javamob.GoalBase{Control: javamob.FlagMove}, stop: 2, speed: 0.35})
	m.Goals().Add(4, &moveTowardsRestriction{GoalBase: javamob.GoalBase{Control: javamob.FlagMove}, speed: 0.35})
	m.Goals().Add(8, goal.NewWaterAvoidingRandomStroll(0.35, 0.001))
	interact := goal.NewLookAtPlayer(3)
	interact.Probability = 1
	interact.Control = javamob.FlagLook | javamob.FlagMove
	m.Goals().Add(9, interact)
	look := goal.NewLookAtPlayer(8)
	look.Match = func(e world.Entity) bool { _, ok := javamob.Of(e); return ok }
	m.Goals().Add(10, look)
}

func (t *WanderingTrader) updateTrades(m *javamob.Mob, _ int) bool {
	r := m.Rand()
	AddOffersFromTradeSet(&t.offers, "wandering_trader/buying", -1, "", r)
	AddOffersFromTradeSet(&t.offers, "wandering_trader/uncommon", -1, "", r)
	AddOffersFromTradeSet(&t.offers, "wandering_trader/common", -1, "", r)
	return true
}

// rewardTradeXp is WanderingTrader.rewardTradeXp: only the orb.
func (t *WanderingTrader) rewardTradeXp(m *javamob.Mob, o *Offer) {
	if o.RewardExp {
		spawnXP(m, 3+m.Rand().IntN(4))
	}
}

func (t *WanderingTrader) screen() (string, int, int, bool) { return "Wandering Trader", 1, 0, false }

// MobInteract is WanderingTrader.mobInteract.
func (t *WanderingTrader) MobInteract(m *javamob.Mob, user item.User, held item.Stack) (item.Stack, bool) {
	p, ok := user.(*player.Player)
	if !ok || isVillagerSpawnEgg(held) || m.Dead() || t.Trading() {
		return held, false
	}
	if len(offersOf(t, m)) == 0 {
		return held, true
	}
	startTrading(t, m, p)
	return held, true
}

// Tick runs the unhappy counter.
func (t *WanderingTrader) Tick(m *javamob.Mob) {
	if t.unhappy > 0 {
		t.unhappy--
	}
}

// AiStep is WanderingTrader.aiStep: maybeDespawn.
func (t *WanderingTrader) AiStep(m *javamob.Mob) {
	checkTradeScreen(t, m)
	if t.DespawnDelay > 0 && !t.Trading() {
		t.DespawnDelay--
		if t.DespawnDelay == 0 {
			// The leashed llamas lose their trader and despawn by themselves.
			_ = m.Close()
		}
	}
}

// RemoveWhenFarAway is WanderingTrader.removeWhenFarAway: never.
func (t *WanderingTrader) RemoveWhenFarAway(*javamob.Mob, float64) bool { return false }

// EncodeNBT writes the offers, DespawnDelay and wander_target.
func (t *WanderingTrader) EncodeNBT(data map[string]any) {
	if t.hasOffers {
		data["Offers"] = t.offers.NBT()
	}
	data["DespawnDelay"] = int32(t.DespawnDelay)
	if t.wanderTarget != nil {
		data["wander_target"] = []int32{int32(t.wanderTarget[0]), int32(t.wanderTarget[1]), int32(t.wanderTarget[2])}
	}
}

// DecodeNBT reads EncodeNBT's data.
func (t *WanderingTrader) DecodeNBT(m *javamob.Mob, data map[string]any) {
	if o, ok := data["Offers"].(map[string]any); ok {
		t.offers, t.hasOffers = OffersFromNBT(o), true
	}
	t.DespawnDelay = int(i32(data["DespawnDelay"], 0))
	if p, ok := data["wander_target"].([]int32); ok && len(p) == 3 {
		pos := cube.Pos{int(p[0]), int(p[1]), int(p[2])}
		t.wanderTarget = &pos
	}
}

// tradeWithPlayer is TradeWithPlayerGoal: stand still while trading.
type tradeWithPlayer struct{ javamob.GoalBase }

func (g *tradeWithPlayer) CanUse(m *javamob.Mob) bool {
	h, ok := m.Behaviour().(merchantHost)
	if !ok || m.Dead() || m.InWater() || !m.OnGround() || m.HurtTime() > 0 {
		return false
	}
	p, ok := h.mstate().tradingPlayerEntity(m)
	return ok && p.Position().Sub(m.Position()).LenSqr() <= 16
}

func (g *tradeWithPlayer) Start(m *javamob.Mob) { m.Navigation().Stop() }

// lookAtTradingPlayer is LookAtTradingPlayerGoal.
type lookAtTradingPlayer struct{ javamob.GoalBase }

func (g *lookAtTradingPlayer) CanUse(m *javamob.Mob) bool {
	h, ok := m.Behaviour().(merchantHost)
	if !ok {
		return false
	}
	_, ok = h.mstate().tradingPlayerEntity(m)
	return ok
}

func (g *lookAtTradingPlayer) Tick(m *javamob.Mob) {
	h := m.Behaviour().(merchantHost)
	if p, ok := h.mstate().tradingPlayerEntity(m); ok {
		m.LookControl().SetLookAtEntity(p, 10, 40)
	}
}

// useItem is UseItemGoal: drink an item (the trader's invisibility potion
// at night, milk in the morning).
type useItem struct {
	javamob.GoalBase
	stack item.Stack
	sound string
	can   func(m *javamob.Mob) bool
	ticks int
}

func (g *useItem) CanUse(m *javamob.Mob) bool           { return g.can(m) }
func (g *useItem) CanContinueToUse(m *javamob.Mob) bool { return g.ticks > 0 }

func (g *useItem) Start(m *javamob.Mob) {
	m.SetItemSlot(javamob.SlotMainHand, g.stack)
	m.UpdateMeta(func(meta *mobMeta) { meta.UsingItem = true })
	g.ticks = 32
}

func (g *useItem) Tick(*javamob.Mob) { g.ticks-- }

func (g *useItem) Stop(m *javamob.Mob) {
	m.SetItemSlot(javamob.SlotMainHand, item.Stack{})
	m.UpdateMeta(func(meta *mobMeta) { meta.UsingItem = false })
	if _, ok := g.stack.Item().(item.Potion); ok {
		m.AddEffect(effect.New(effect.Invisibility, 1, ticksDuration(3600)))
	} else {
		m.RemoveEffect(effect.Invisibility)
	}
	m.PlaySound(g.sound)
}

// wanderToPosition is WanderingTrader.WanderToPositionGoal.
type wanderToPosition struct {
	javamob.GoalBase
	stop, speed float64
}

func traderTarget(m *javamob.Mob) (*cube.Pos, bool) {
	t, ok := TraderOf(m)
	if !ok || t.wanderTarget == nil {
		return nil, false
	}
	return t.wanderTarget, true
}

func (g *wanderToPosition) CanUse(m *javamob.Mob) bool {
	p, ok := traderTarget(m)
	return ok && !closerToCenter(*p, m.Position(), g.stop)
}

func closerToCenter(p cube.Pos, v mgl64.Vec3, d float64) bool {
	c := mgl64.Vec3{float64(p[0]) + .5, float64(p[1]) + .5, float64(p[2]) + .5}
	return c.Sub(v).LenSqr() < d*d
}

func (g *wanderToPosition) Stop(m *javamob.Mob) {
	if t, ok := TraderOf(m); ok {
		t.wanderTarget = nil
	}
	m.Navigation().Stop()
}

func (g *wanderToPosition) Tick(m *javamob.Mob) {
	p, ok := traderTarget(m)
	if !ok || !m.Navigation().Done() {
		return
	}
	pos := m.Position()
	target := mgl64.Vec3{float64(p[0]), float64(p[1]), float64(p[2])}
	if !closerToCenter(*p, pos, 10) {
		dir := target.Sub(pos)
		if l := dir.Len(); l > 0 {
			dir = dir.Mul(10 / l)
		}
		m.Navigation().MoveTo(m, pos.Add(dir), 0, g.speed)
	} else {
		m.Navigation().MoveTo(m, target, 0, g.speed)
	}
}

// moveTowardsRestriction is MoveTowardsRestrictionGoal: back towards the
// home the spawner set (radius 16).
type moveTowardsRestriction struct {
	javamob.GoalBase
	speed  float64
	wanted mgl64.Vec3
}

func (g *moveTowardsRestriction) CanUse(m *javamob.Mob) bool {
	if m.WithinHome(BlockPos(m)) || m.HomeRadius() < 0 {
		return false
	}
	home, _ := m.Home()
	p, ok := jmob.DefaultRandomPosTowards(m, 16, 7, bottomCenter(home), 1.5707963705062866)
	if !ok {
		return false
	}
	g.wanted = p
	return true
}

func (g *moveTowardsRestriction) CanContinueToUse(m *javamob.Mob) bool { return !m.Navigation().Done() }
func (g *moveTowardsRestriction) Start(m *javamob.Mob) {
	m.Navigation().MoveTo(m, g.wanted, 0, g.speed)
}

// panicGoal is PanicGoal: run when hurt (by a panic cause) or burning.
type panicGoal struct {
	javamob.GoalBase
	speed  float64
	wanted mgl64.Vec3
	run    bool
}

func newPanic(speed float64) *panicGoal {
	return &panicGoal{GoalBase: javamob.GoalBase{Control: javamob.FlagMove}, speed: speed}
}

func (g *panicGoal) shouldPanic(m *javamob.Mob) bool {
	_, hurt := m.LastHurtByMob()
	return hurt && m.TickCount()-m.LastHurtByMobTimestamp() < 100 || m.IsOnFire()
}

func (g *panicGoal) CanUse(m *javamob.Mob) bool {
	if !g.shouldPanic(m) {
		return false
	}
	p, ok := m.DefaultRandomPos(5, 4)
	if !ok {
		return false
	}
	g.wanted = p
	return true
}

func (g *panicGoal) Start(m *javamob.Mob) {
	m.Navigation().MoveTo(m, g.wanted, 0, g.speed)
	g.run = true
}

func (g *panicGoal) Stop(*javamob.Mob) { g.run = false }

func (g *panicGoal) CanContinueToUse(m *javamob.Mob) bool { return !m.Navigation().Done() }
