//go:build crossfly_full

package jvillage

import (
	"math"
	"slices"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/path"
	"github.com/df-mc/dragonfly/server/mob/jbrain"
	"github.com/df-mc/dragonfly/server/mob/jbrain/jmob"
	"github.com/df-mc/dragonfly/server/mob/jpoi"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// villagerPanicTrigger is VillagerPanicTrigger.
type villagerPanicTrigger struct{ jmob.TaskBase }

func isHurt(c *jbrain.Ctx) bool     { return c.Mem.Has(jbrain.MemHurtBy.ID()) }
func hasHostile(c *jbrain.Ctx) bool { return c.Mem.Has(jbrain.MemNearestHostile.ID()) }

// VillagerPanicTrigger is new VillagerPanicTrigger(): hurt or near a hostile,
// switch to PANIC; every 100 ticks of panic, 3 villagers suffice for a golem.
func VillagerPanicTrigger() jmob.Control {
	return jmob.Behaviour("VillagerPanicTrigger", nil, jbrain.DefaultDuration, jbrain.DefaultDuration, villagerPanicTrigger{})
}

func (villagerPanicTrigger) CanStillUse(c *jbrain.Ctx, _ *javamob.Mob) bool {
	return isHurt(c) || hasHostile(c)
}

func (villagerPanicTrigger) Start(c *jbrain.Ctx, m *javamob.Mob) {
	if !isHurt(c) && !hasHostile(c) {
		return
	}
	b, ok := jmob.Of(m)
	if !ok {
		return
	}
	if !b.IsActive(jbrain.ActPanic) {
		for _, id := range []jbrain.MemoryID{jbrain.MemPath.ID(), jbrain.MemWalkTarget.ID(), jbrain.MemLookTarget.ID(), jbrain.MemBreedTarget.ID(), jbrain.MemInteractionTarget.ID()} {
			c.Mem.Erase(id)
		}
	}
	b.SetActiveActivityIfPossible(jbrain.ActPanic)
}

func (villagerPanicTrigger) Tick(c *jbrain.Ctx, m *javamob.Mob) {
	if c.Time%100 == 0 {
		if v, ok := Of(m); ok {
			v.SpawnGolemIfNeeded(m, c.Time, 3)
		}
	}
}

// VillagerCalmDown is VillagerCalmDown.create: no longer scared (no damage,
// no hostile, the attacker 6+ blocks away), forget the attacker and follow
// the schedule again.
func VillagerCalmDown() jmob.Control {
	return jmob.Trigger("VillagerCalmDown", []jbrain.Req{jbrain.Reg(jbrain.MemHurtBy), jbrain.Reg(jbrain.MemHurtByEntity), jbrain.Reg(jbrain.MemNearestHostile)}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		scared := isHurt(c) || hasHostile(c)
		if !scared {
			if h, ok := jbrain.Get(c.Mem, jbrain.MemHurtByEntity); ok {
				if e, ok := h.Entity(c.Tx); ok && e.Position().Sub(m.Position()).LenSqr() <= 36 {
					scared = true
				}
			}
		}
		if !scared {
			c.Mem.Erase(jbrain.MemHurtBy.ID())
			c.Mem.Erase(jbrain.MemHurtByEntity.ID())
			if b, ok := jmob.Of(m); ok {
				b.UpdateActivityFromSchedule(c)
			}
		}
		return true
	})
}

// WakeUp is WakeUp.create: not resting any more, get out of bed.
func WakeUp() jmob.Control {
	return jmob.Trigger("WakeUp", nil, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		v, ok := Of(m)
		b, ok2 := jmob.Of(m)
		if ok && ok2 && !b.IsActive(jbrain.ActRest) && v.sleeping {
			v.StopSleeping(m)
			return true
		}
		return false
	})
}

// sleepInBed is SleepInBed.
type sleepInBed struct {
	jmob.TaskBase
	nextOk int64
}

// SleepInBed is new SleepInBed(): within 2 blocks of a free home bed while
// resting, sleep (LAST_SLEPT is what golem summoning needs).
func SleepInBed() jmob.Control {
	return jmob.Behaviour("SleepInBed", []jbrain.Req{jbrain.Present(jbrain.MemHome), jbrain.Reg(jbrain.MemLastWoken), jbrain.Reg(jbrain.MemLastSlept), jbrain.Reg(jbrain.MemWalkTarget), jbrain.Reg(jbrain.MemCantReachWalkTargetSince)}, jbrain.DefaultDuration, jbrain.DefaultDuration, &sleepInBed{})
}

func (s *sleepInBed) TimedOut(int64, int64) bool { return false }

func (s *sleepInBed) CanStart(c *jbrain.Ctx, m *javamob.Mob) bool {
	if m.Passenger() {
		return false
	}
	home, _ := jbrain.Get(c.Mem, jbrain.MemHome)
	if !sameDim(c.Tx, home) {
		return false
	}
	if w, ok := jbrain.Get(c.Mem, jbrain.MemLastWoken); ok {
		if d := c.Time - w; d > 0 && d < 100 {
			return false
		}
	}
	b, loaded := c.Tx.BlockLoaded(home.Pos)
	if !loaded {
		return false
	}
	bed, ok := b.(block.Bed)
	return ok && jmob.CloserToCenterThan(home.Pos, m.Position(), 2) && bed.Sleeper == nil
}

func (s *sleepInBed) CanStillUse(c *jbrain.Ctx, m *javamob.Mob) bool {
	home, ok := jbrain.Get(c.Mem, jbrain.MemHome)
	if !ok {
		return false
	}
	b, ok := jmob.Of(m)
	return ok && b.IsActive(jbrain.ActRest) && m.Position()[1] > float64(home.Pos[1])+0.4 && jmob.CloserToCenterThan(home.Pos, m.Position(), 1.14)
}

func (s *sleepInBed) Start(c *jbrain.Ctx, m *javamob.Mob) {
	if c.Time <= s.nextOk {
		return
	}
	v, ok := Of(m)
	if !ok {
		return
	}
	if doors, ok := jbrain.Get(c.Mem, jbrain.MemDoorsToClose); ok {
		closeDoorsThatIHaveOpened(c, m, doors)
	}
	home, _ := jbrain.Get(c.Mem, jbrain.MemHome)
	if v.StartSleeping(m, home.Pos) {
		jbrain.Set(c.Mem, jbrain.MemLastSlept, c.Time)
	}
	c.Mem.Erase(jbrain.MemWalkTarget.ID())
	c.Mem.Erase(jbrain.MemCantReachWalkTargetSince.ID())
}

func (s *sleepInBed) Stop(c *jbrain.Ctx, m *javamob.Mob) {
	if v, ok := Of(m); ok && v.sleeping {
		v.StopSleeping(m)
		s.nextOk = c.Time + 40
	}
}

// tradeWithVillager is TradeWithVillager.
type tradeWithVillager struct {
	jmob.TaskBase
	trades []string
}

// TradeWithVillager is new TradeWithVillager(): walk up to the interaction
// target villager, gossip, and throw it food, wheat and the items its
// profession wants.
func TradeWithVillager() jmob.Control {
	return jmob.Behaviour("TradeWithVillager", []jbrain.Req{jbrain.Present(jbrain.MemInteractionTarget), jbrain.Present(jbrain.MemNearestVisibleLivingEntities)}, jbrain.DefaultDuration, jbrain.DefaultDuration, &tradeWithVillager{})
}

func (t *tradeWithVillager) CanStart(c *jbrain.Ctx, _ *javamob.Mob) bool {
	return jmob.TargetIsValid(c, jbrain.MemInteractionTarget, VillagerID)
}

func (t *tradeWithVillager) CanStillUse(c *jbrain.Ctx, m *javamob.Mob) bool { return t.CanStart(c, m) }

func interactionVillager(c *jbrain.Ctx) (*javamob.Mob, *Villager, bool) {
	h, ok := jbrain.Get(c.Mem, jbrain.MemInteractionTarget)
	if !ok {
		return nil, nil, false
	}
	om, ok := javamob.OfHandle(c.Tx, h)
	if !ok {
		return nil, nil, false
	}
	ov, ok := Of(om)
	return om, ov, ok
}

func (t *tradeWithVillager) Start(c *jbrain.Ctx, m *javamob.Mob) {
	om, ov, ok := interactionVillager(c)
	if !ok {
		return
	}
	jmob.LockGazeAndWalkToEachOther(c.Tx, m, om, 0.5, 2)
	v, _ := Of(m)
	t.trades = t.trades[:0]
	for _, it := range ov.profession().RequestedItems {
		if !slices.Contains(v.profession().RequestedItems, it) {
			t.trades = append(t.trades, it)
		}
	}
}

func (t *tradeWithVillager) Tick(c *jbrain.Ctx, m *javamob.Mob) {
	om, ov, ok := interactionVillager(c)
	if !ok || m.Position().Sub(om.Position()).LenSqr() > 5 {
		return
	}
	v, _ := Of(m)
	jmob.LockGazeAndWalkToEachOther(c.Tx, m, om, 0.5, 2)
	v.Gossip(m, om, c.Time)
	farmer := v.profession() == ProfFarmer
	if v.HasExcessFood() && (farmer || ov.WantsMoreFood()) {
		v.throwHalfStack(m, func(s item.Stack) bool { return food(s) > 0 }, om)
	}
	if farmer && v.CountItem("wheat") > 32 {
		v.throwHalfStack(m, func(s item.Stack) bool { return javaNameOf(s) == "wheat" }, om)
	}
	if len(t.trades) > 0 {
		v.throwHalfStack(m, func(s item.Stack) bool { return slices.Contains(t.trades, javaNameOf(s)) }, om)
	}
}

func (t *tradeWithVillager) Stop(c *jbrain.Ctx, _ *javamob.Mob) {
	c.Mem.Erase(jbrain.MemInteractionTarget.ID())
}

// throwHalfStack is TradeWithVillager.throwHalfStack.
func (v *Villager) throwHalfStack(m *javamob.Mob, pred func(item.Stack) bool, target world.Entity) {
	for i, s := range v.inventory {
		if s.Empty() || !pred(s) {
			continue
		}
		var n int
		if s.Count() > s.MaxCount()/2 {
			n = s.Count() / 2
		} else {
			if s.Count() <= 24 {
				continue
			}
			n = s.Count() - 24
		}
		v.inventory[i] = s.Grow(-n)
		throwItem(m, s.Grow(n-s.Count()), target.Position())
		return
	}
}

// throwItem is BehaviorUtils.throwItem: drop an item from the hands moving
// 0.3 towards target.
func throwItem(m *javamob.Mob, s item.Stack, target mgl64.Vec3) {
	if s.Empty() {
		return
	}
	from := mgl64.Vec3{m.Position()[0], m.EyeY() - 0.3, m.Position()[2]}
	dir := target.Sub(m.Position())
	if l := dir.Len(); l > 0 {
		dir = dir.Mul(0.3 / l)
	}
	ent := entity.NewItemPickupDelay(world.EntitySpawnOpts{Position: from, Velocity: dir}, s, 2*time.Second)
	markThrown(ent, m.Tx().CurrentTick()+40)
	m.Tx().AddEntity(ent)
}

// villagerMakeLove is VillagerMakeLove.
type villagerMakeLove struct {
	jmob.TaskBase
	birth int64
}

// VillagerMakeLove is new VillagerMakeLove(): two willing villagers walk up
// to each other for 275-325 ticks, eat, and have a baby if a free bed can
// be reached.
func VillagerMakeLove() jmob.Control {
	return jmob.Behaviour("VillagerMakeLove", []jbrain.Req{jbrain.Present(jbrain.MemBreedTarget), jbrain.Present(jbrain.MemNearestVisibleLivingEntities)}, 350, 350, &villagerMakeLove{})
}

func breedPartner(c *jbrain.Ctx) (*javamob.Mob, *Villager, bool) {
	h, ok := jbrain.Get(c.Mem, jbrain.MemBreedTarget)
	if !ok {
		return nil, nil, false
	}
	om, ok := javamob.OfHandle(c.Tx, h)
	if !ok {
		return nil, nil, false
	}
	ov, ok := Of(om)
	return om, ov, ok
}

func (l *villagerMakeLove) possible(c *jbrain.Ctx, m *javamob.Mob) bool {
	_, ov, ok := breedPartner(c)
	v, ok2 := Of(m)
	return ok && ok2 && jmob.TargetIsValid(c, jbrain.MemBreedTarget, VillagerID) && v.CanBreed() && ov.CanBreed()
}

func (l *villagerMakeLove) CanStart(c *jbrain.Ctx, m *javamob.Mob) bool { return l.possible(c, m) }

func (l *villagerMakeLove) CanStillUse(c *jbrain.Ctx, m *javamob.Mob) bool {
	return c.Time <= l.birth && l.possible(c, m)
}

func (l *villagerMakeLove) Start(c *jbrain.Ctx, m *javamob.Mob) {
	om, _, _ := breedPartner(c)
	jmob.LockGazeAndWalkToEachOther(c.Tx, m, om, 0.5, 2)
	broadcastEvent(om, EventInLove)
	broadcastEvent(m, EventInLove)
	l.birth = c.Time + 275 + int64(m.Rand().IntN(50))
}

func (l *villagerMakeLove) Tick(c *jbrain.Ctx, m *javamob.Mob) {
	om, ov, ok := breedPartner(c)
	if !ok || m.Position().Sub(om.Position()).LenSqr() > 5 {
		return
	}
	v, _ := Of(m)
	jmob.LockGazeAndWalkToEachOther(c.Tx, m, om, 0.5, 2)
	if c.Time >= l.birth {
		v.EatAndDigestFood()
		ov.EatAndDigestFood()
		l.giveBirth(c, m, om)
	} else if m.Rand().IntN(35) == 0 {
		broadcastEvent(om, EventHearts)
		broadcastEvent(m, EventHearts)
	}
}

func (l *villagerMakeLove) giveBirth(c *jbrain.Ctx, m, partner *javamob.Mob) {
	pm := jpoi.For(c.Tx)
	bed, ok := pm.Take(c.Tx, jpoi.Is(jpoi.Home), func(t *jpoi.Type, p cube.Pos) bool {
		path := m.Navigation().CreatePath(m, p, t.ValidRange)
		return path != nil && path.CanReach()
	}, BlockPos(m), 48)
	if !ok {
		broadcastEvent(partner, EventAngry)
		broadcastEvent(m, EventAngry)
		return
	}
	child, ok := breedVillagers(c, m, partner)
	if !ok {
		pm.Release(c.Tx, bed)
		return
	}
	if mem := jmob.Mem(child); mem != nil {
		jbrain.Set(mem, jbrain.MemHome, jbrain.GlobalPos{Dim: c.Tx.World().Dimension(), Pos: bed})
	}
}

func (l *villagerMakeLove) Stop(c *jbrain.Ctx, _ *javamob.Mob) {
	c.Mem.Erase(jbrain.MemBreedTarget.ID())
}

// breedVillagers is VillagerMakeLove.breed with Villager.getBreedOffspring:
// the baby's type comes from the biome (50%) or either parent (25% each).
func breedVillagers(c *jbrain.Ctx, a, b *javamob.Mob) (*javamob.Mob, bool) {
	va, _ := Of(a)
	vb, _ := Of(b)
	roll := a.Rand().Float64()
	typ := va.Data.Type
	switch {
	case roll < 0.5:
		typ = TypeByBedrockBiome(biomeName(c.Tx, BlockPos(a)))
	case roll >= 0.75:
		typ = vb.Data.Type
	}
	child, err := javamob.Spawn(c.Tx, VillagerID, a.Position(), javamob.SpawnBreeding, javamob.SpawnOptions{
		Rotation: cube.Rotation{},
		Configure: func(cm *javamob.Mob) {
			if cv, ok := Of(cm); ok {
				cv.Data = Data{Type: typ, Profession: ProfNone, Level: 1}
				cv.DataFinalized = true
				cv.SetAge(-24000)
				cm.SetBaby(true)
			}
		},
	})
	if err != nil {
		return nil, false
	}
	va.SetAge(6000)
	vb.SetAge(6000)
	broadcastEvent(child, EventHearts)
	return child, true
}

func biomeName(tx *world.Tx, p cube.Pos) string {
	return tx.Biome(p).String()
}

// showTradesToPlayer is ShowTradesToPlayer: hold up what the villager would
// sell for the item the nearby player holds, cycling every 40 ticks.
type showTradesToPlayer struct {
	jmob.TaskBase
	playerItem   item.Stack
	hasItem      bool
	display      []item.Stack
	cycle, index int
	lookTime     int
}

// ShowTradesToPlayer is new ShowTradesToPlayer(min, max).
func ShowTradesToPlayer(minDuration, maxDuration int) jmob.Control {
	return jmob.Behaviour("ShowTradesToPlayer", []jbrain.Req{jbrain.Present(jbrain.MemInteractionTarget)}, minDuration, maxDuration, &showTradesToPlayer{})
}

func interactionPlayer(c *jbrain.Ctx) (*player.Player, bool) {
	h, ok := jbrain.Get(c.Mem, jbrain.MemInteractionTarget)
	if !ok {
		return nil, false
	}
	e, ok := h.Entity(c.Tx)
	if !ok {
		return nil, false
	}
	p, ok := e.(*player.Player)
	return p, ok
}

func (s *showTradesToPlayer) CanStart(c *jbrain.Ctx, m *javamob.Mob) bool {
	p, ok := interactionPlayer(c)
	v, ok2 := Of(m)
	return ok && ok2 && !m.Dead() && !p.Dead() && !v.Baby() && p.Position().Sub(m.Position()).LenSqr() <= 17
}

func (s *showTradesToPlayer) CanStillUse(c *jbrain.Ctx, m *javamob.Mob) bool {
	return s.CanStart(c, m) && s.lookTime > 0
}

func (s *showTradesToPlayer) Start(c *jbrain.Ctx, m *javamob.Mob) {
	s.look(c)
	s.cycle, s.index, s.lookTime = 0, 0, 40
}

func (s *showTradesToPlayer) look(c *jbrain.Ctx) (*player.Player, bool) {
	p, ok := interactionPlayer(c)
	if ok {
		jmob.LookAtEntity(c.Mem, p.H())
	}
	return p, ok
}

func (s *showTradesToPlayer) Tick(c *jbrain.Ctx, m *javamob.Mob) {
	p, ok := s.look(c)
	if !ok {
		return
	}
	v, _ := Of(m)
	held, _ := p.HeldItems()
	if !s.hasItem || !sameItemType(s.playerItem, held) {
		s.playerItem, s.hasItem = held, true
		s.display = s.display[:0]
		if !held.Empty() {
			for _, o := range v.Offers(m) {
				if !o.IsOutOfStock() && (sameItemType(held, o.CostA()) || sameItemType(held, o.BuyB)) {
					s.display = append(s.display, o.Sell)
				}
			}
			if len(s.display) > 0 {
				s.lookTime = 900
				setMainHand(m, s.display[0])
			}
		}
	}
	if len(s.display) > 0 {
		if len(s.display) >= 2 {
			s.cycle++
			if s.cycle >= 40 {
				s.index = (s.index + 1) % len(s.display)
				s.cycle = 0
				setMainHand(m, s.display[s.index])
			}
		}
	} else {
		setMainHand(m, item.Stack{})
		s.lookTime = min(s.lookTime, 40)
	}
	s.lookTime--
}

func (s *showTradesToPlayer) Stop(c *jbrain.Ctx, m *javamob.Mob) {
	c.Mem.Erase(jbrain.MemInteractionTarget.ID())
	setMainHand(m, item.Stack{})
	s.hasItem = false
}

func sameItemType(a, b item.Stack) bool {
	if a.Empty() || b.Empty() {
		return a.Empty() && b.Empty()
	}
	an, am := a.Item().EncodeItem()
	bn, bm := b.Item().EncodeItem()
	return an == bn && am == bm
}

// setMainHand is setItemSlot(MAINHAND) with the drop chance the display
// uses (0 while showing, 0.085 after).
func setMainHand(m *javamob.Mob, s item.Stack) {
	m.SetItemSlot(javamob.SlotMainHand, s)
	if s.Empty() {
		m.SetDropChance(javamob.SlotMainHand, 0.085)
	} else {
		m.SetDropChance(javamob.SlotMainHand, 0)
	}
}

// lookAndFollowTradingPlayer is LookAndFollowTradingPlayerSink.
type lookAndFollowTradingPlayer struct {
	jmob.TaskBase
	speed float32
}

// LookAndFollowTradingPlayerSink is new LookAndFollowTradingPlayerSink(speed).
func LookAndFollowTradingPlayerSink(speed float32) jmob.Control {
	return jmob.Behaviour("LookAndFollowTradingPlayerSink", []jbrain.Req{jbrain.Reg(jbrain.MemWalkTarget), jbrain.Reg(jbrain.MemLookTarget)}, math.MaxInt32, math.MaxInt32, &lookAndFollowTradingPlayer{speed: speed})
}

func (l *lookAndFollowTradingPlayer) TimedOut(int64, int64) bool { return false }

func (l *lookAndFollowTradingPlayer) CanStart(c *jbrain.Ctx, m *javamob.Mob) bool {
	v, ok := Of(m)
	if !ok || m.Dead() {
		return false
	}
	p, ok := v.tradingPlayerEntity(m)
	return ok && !m.InWater() && m.HurtTime() <= 0 && p.Position().Sub(m.Position()).LenSqr() <= 16
}

func (l *lookAndFollowTradingPlayer) CanStillUse(c *jbrain.Ctx, m *javamob.Mob) bool {
	return l.CanStart(c, m)
}
func (l *lookAndFollowTradingPlayer) Start(c *jbrain.Ctx, m *javamob.Mob) { l.follow(c, m) }
func (l *lookAndFollowTradingPlayer) Tick(c *jbrain.Ctx, m *javamob.Mob)  { l.follow(c, m) }

func (l *lookAndFollowTradingPlayer) Stop(c *jbrain.Ctx, _ *javamob.Mob) {
	c.Mem.Erase(jbrain.MemWalkTarget.ID())
	c.Mem.Erase(jbrain.MemLookTarget.ID())
}

func (l *lookAndFollowTradingPlayer) follow(c *jbrain.Ctx, m *javamob.Mob) {
	v, _ := Of(m)
	if v.tradingPlayer == nil {
		return
	}
	jbrain.Set(c.Mem, jbrain.MemWalkTarget, jbrain.WalkTarget{Target: jbrain.TrackEntity(v.tradingPlayer, false), Speed: l.speed, CloseEnough: 2})
	jbrain.Set[jbrain.PositionTracker](c.Mem, jbrain.MemLookTarget, jbrain.TrackEntity(v.tradingPlayer, true))
}

// GoToWantedItem is GoToWantedItem.create(predicate, speed, interrupt, maxDist).
func GoToWantedItem(pred func(m *javamob.Mob) bool, speed float32, interrupt bool, maxDist int) jmob.Control {
	walk := jbrain.Absent(jbrain.MemWalkTarget)
	if interrupt {
		walk = jbrain.Reg(jbrain.MemWalkTarget)
	}
	return jmob.Trigger("GoToWantedItem", []jbrain.Req{jbrain.Reg(jbrain.MemLookTarget), walk, jbrain.Present(jbrain.MemNearestVisibleWantedItem), jbrain.Reg(jbrain.MemItemPickupCooldownTicks)}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		h, _ := jbrain.Get(c.Mem, jbrain.MemNearestVisibleWantedItem)
		e, ok := h.Entity(c.Tx)
		if !ok || c.Mem.Has(jbrain.MemItemPickupCooldownTicks.ID()) || (pred != nil && !pred(m)) || !m.CanPickUpLoot() {
			return false
		}
		if e.Position().Sub(m.Position()).LenSqr() >= float64(maxDist*maxDist) {
			return false
		}
		jbrain.Set[jbrain.PositionTracker](c.Mem, jbrain.MemLookTarget, jbrain.TrackEntity(h, true))
		jbrain.Set(c.Mem, jbrain.MemWalkTarget, jbrain.WalkTarget{Target: jbrain.TrackEntity(h, false), Speed: speed})
		return true
	})
}

// VillageBoundRandomStroll is VillageBoundRandomStroll.create(speed, xz, y):
// stroll inside the village, or towards the closest village section.
func VillageBoundRandomStroll(speed float32, maxXZ, maxY int) *jbrain.OneShot[*javamob.Mob] {
	return jmob.Trigger("VillageBoundRandomStroll", []jbrain.Req{jbrain.Absent(jbrain.MemWalkTarget)}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		pm := jpoi.For(c.Tx)
		pos := BlockPos(m)
		var land mgl64.Vec3
		var ok bool
		if pm.IsVillage(c.Tx, pos) {
			land, ok = m.LandRandomPos(maxXZ, maxY)
		} else if best, moved := closestVillageSection(c.Tx, pm, pos, 2); moved {
			center := mgl64.Vec3{float64(best[0]<<4) + 8, float64(best[1] << 4), float64(best[2]<<4) + 8}
			land, ok = jmob.DefaultRandomPosTowards(m, maxXZ, maxY, center, math.Pi/2)
		} else {
			land, ok = m.LandRandomPos(maxXZ, maxY)
		}
		if ok {
			jbrain.Set(c.Mem, jbrain.MemWalkTarget, jbrain.WalkTarget{Target: jbrain.TrackVec(land), Speed: speed})
		} else {
			c.Mem.Erase(jbrain.MemWalkTarget.ID())
		}
		return true
	})
}

// closestVillageSection is BehaviorUtils.findSectionClosestToVillage.
func closestVillageSection(tx *world.Tx, pm *jpoi.Manager, pos cube.Pos, radius int) (cube.Pos, bool) {
	center := cube.Pos{pos[0] >> 4, pos[1] >> 4, pos[2] >> 4}
	at := func(s cube.Pos) int { return pm.SectionsToVillage(tx, cube.Pos{s[0] << 4, s[1] << 4, s[2] << 4}) }
	dist := at(center)
	best, bestD := center, dist
	for x := -radius; x <= radius; x++ {
		for y := -radius; y <= radius; y++ {
			for z := -radius; z <= radius; z++ {
				s := center.Add(cube.Pos{x, y, z})
				if d := at(s); d < dist && d < bestD {
					best, bestD = s, d
				}
			}
		}
	}
	return best, best != center
}

// SetClosestHomeAsWalkTarget is SetClosestHomeAsWalkTarget.create(speed).
func SetClosestHomeAsWalkTarget(speed float32) jmob.Control {
	cache := map[cube.Pos]int64{}
	var last int64
	return jmob.Trigger("SetClosestHomeAsWalkTarget", []jbrain.Req{jbrain.Absent(jbrain.MemWalkTarget), jbrain.Absent(jbrain.MemHome)}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		if c.Time-last < 20 {
			return false
		}
		pm := jpoi.For(c.Tx)
		pos := BlockPos(m)
		closest, ok := pm.FindClosest(c.Tx, jpoi.Is(jpoi.Home), nil, pos, 48, jpoi.AnyOccupancy)
		if !ok || distSqPos(closest.Pos, pos) <= 4 {
			return false
		}
		tried := 0
		last = c.Time + int64(c.Rand.IntN(20))
		test := func(p cube.Pos) bool {
			if _, ok := cache[p]; ok {
				return false
			}
			tried++
			if tried >= 5 {
				return false
			}
			cache[p] = last + 40
			return true
		}
		pois := pm.FindAllWithType(c.Tx, jpoi.Is(jpoi.Home), test, pos, 48, jpoi.AnyOccupancy)
		if target, ok := pathToPois(m, pois); ok {
			if _, ok := pm.Type(c.Tx, target); ok {
				jbrain.Set(c.Mem, jbrain.MemWalkTarget, jbrain.WalkTarget{Target: jbrain.TrackBlock(target), Speed: speed, CloseEnough: 1})
			}
		} else if tried < 5 {
			for p, t := range cache {
				if t < last {
					delete(cache, p)
				}
			}
		}
		return true
	})
}

func distSqPos(a, b cube.Pos) float64 {
	dx, dy, dz := float64(a[0]-b[0]), float64(a[1]-b[1]), float64(a[2]-b[2])
	return dx*dx + dy*dy + dz*dz
}

// InsideBrownianWalk is InsideBrownianWalk.create(speed): indoors, step to a
// random covered neighbour block.
func InsideBrownianWalk(speed float32) jmob.Control {
	return jmob.Trigger("InsideBrownianWalk", []jbrain.Req{jbrain.Absent(jbrain.MemWalkTarget)}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		pos := BlockPos(m)
		if path.CanSeeSky(c.Tx, pos) {
			return false
		}
		var poses []cube.Pos
		for x := -1; x <= 1; x++ {
			for y := -1; y <= 1; y++ {
				for z := -1; z <= 1; z++ {
					poses = append(poses, pos.Add(cube.Pos{x, y, z}))
				}
			}
		}
		for i := len(poses) - 1; i > 0; i-- {
			j := c.Rand.IntN(i + 1)
			poses[i], poses[j] = poses[j], poses[i]
		}
		for _, p := range poses {
			if !path.CanSeeSky(c.Tx, p) && entityCanStandOn(c.Tx, p) && !path.Collides(c.Tx, m.BBox()) {
				jbrain.Set(c.Mem, jbrain.MemWalkTarget, jbrain.WalkTarget{Target: jbrain.TrackBlock(p), Speed: speed})
				break
			}
		}
		return true
	})
}

// GoToClosestVillage is GoToClosestVillage.create(speed, closeEnough).
func GoToClosestVillage(speed float32, closeEnough int) jmob.Control {
	return jmob.Trigger("GoToClosestVillage", []jbrain.Req{jbrain.Absent(jbrain.MemWalkTarget)}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		pm := jpoi.For(c.Tx)
		pos := BlockPos(m)
		if pm.IsVillage(c.Tx, pos) {
			return false
		}
		cur := pm.SectionsToVillage(c.Tx, pos)
		var target mgl64.Vec3
		found := false
		for range 5 {
			p, ok := m.LandRandomPos(15, 7)
			if !ok {
				continue
			}
			d := pm.SectionsToVillage(c.Tx, jbrain.Containing(p))
			if d < cur {
				target, found = p, true
				break
			}
			if d == cur {
				target, found = p, true
			}
		}
		if found {
			jbrain.Set(c.Mem, jbrain.MemWalkTarget, jbrain.WalkTarget{Target: jbrain.TrackVec(target), Speed: speed, CloseEnough: closeEnough})
		}
		return true
	})
}

// jumpOnBed is JumpOnBed (babies).
type jumpOnBed struct {
	jmob.TaskBase
	speed                      float32
	bed                        cube.Pos
	hasBed                     bool
	timeToReach, jumps, cooldn int
}

// JumpOnBed is new JumpOnBed(speed).
func JumpOnBed(speed float32) jmob.Control {
	return jmob.Behaviour("JumpOnBed", []jbrain.Req{jbrain.Present(jbrain.MemNearestBed), jbrain.Absent(jbrain.MemWalkTarget)}, jbrain.DefaultDuration, jbrain.DefaultDuration, &jumpOnBed{speed: speed})
}

func (j *jumpOnBed) TimedOut(int64, int64) bool { return false }

func isBed(tx *world.Tx, p cube.Pos) bool {
	_, ok := tx.Block(p).(block.Bed)
	return ok
}

func (j *jumpOnBed) onOrOver(c *jbrain.Ctx, m *javamob.Mob) bool {
	p := BlockPos(m)
	return isBed(c.Tx, p) || isBed(c.Tx, p.Side(cube.FaceDown))
}

func (j *jumpOnBed) CanStart(c *jbrain.Ctx, m *javamob.Mob) bool {
	v, ok := Of(m)
	return ok && v.Baby() && (j.onOrOver(c, m) || c.Mem.Has(jbrain.MemNearestBed.ID()))
}

func (j *jumpOnBed) Start(c *jbrain.Ctx, m *javamob.Mob) {
	bed, ok := jbrain.Get(c.Mem, jbrain.MemNearestBed)
	if !ok {
		return
	}
	j.bed, j.hasBed = bed, true
	j.timeToReach, j.jumps, j.cooldn = 100, 3+c.Rand.IntN(4), 0
	jbrain.Set(c.Mem, jbrain.MemWalkTarget, jbrain.WalkTarget{Target: jbrain.TrackBlock(bed), Speed: j.speed})
}

func (j *jumpOnBed) Stop(*jbrain.Ctx, *javamob.Mob) {
	j.hasBed, j.timeToReach, j.jumps, j.cooldn = false, 0, 0, 0
}

func (j *jumpOnBed) CanStillUse(c *jbrain.Ctx, m *javamob.Mob) bool {
	v, ok := Of(m)
	if !ok || !v.Baby() || !j.hasBed || !isBed(c.Tx, j.bed) {
		return false
	}
	over := j.onOrOver(c, m)
	return !(!over && j.timeToReach <= 0) && !(over && j.jumps <= 0)
}

func (j *jumpOnBed) Tick(c *jbrain.Ctx, m *javamob.Mob) {
	switch {
	case !j.onOrOver(c, m):
		j.timeToReach--
	case j.cooldn > 0:
		j.cooldn--
	case isBed(c.Tx, BlockPos(m)):
		m.JumpControl().Jump()
		j.jumps--
		j.cooldn = 5
	}
}

// GiveGiftToHero is new GiveGiftToHero(timeout): a player with Hero of the
// Village gets a gift from the profession's gift loot table.
type giveGiftToHero struct {
	jmob.TaskBase
	untilNext int
	given     bool
	started   int64
}

func GiveGiftToHero(timeout int) jmob.Control {
	return jmob.Behaviour("GiveGiftToHero", []jbrain.Req{jbrain.Reg(jbrain.MemWalkTarget), jbrain.Reg(jbrain.MemLookTarget), jbrain.Reg(jbrain.MemInteractionTarget), jbrain.Present(jbrain.MemNearestVisiblePlayer)}, timeout, timeout, &giveGiftToHero{untilNext: 600})
}

func heroNearby(c *jbrain.Ctx) (*player.Player, bool) {
	h, ok := jbrain.Get(c.Mem, jbrain.MemNearestVisiblePlayer)
	if !ok {
		return nil, false
	}
	e, ok := h.Entity(c.Tx)
	if !ok {
		return nil, false
	}
	p, ok := e.(*player.Player)
	if !ok {
		return nil, false
	}
	_, hero := HeroAmplifier(p)
	return p, hero
}

func (g *giveGiftToHero) CanStart(c *jbrain.Ctx, _ *javamob.Mob) bool {
	if _, ok := heroNearby(c); !ok {
		return false
	}
	if g.untilNext > 0 {
		g.untilNext--
		return false
	}
	return true
}

func (g *giveGiftToHero) Start(c *jbrain.Ctx, m *javamob.Mob) {
	g.given, g.started = false, c.Time
	p, _ := heroNearby(c)
	jbrain.Set(c.Mem, jbrain.MemInteractionTarget, p.H())
	jmob.LookAtEntity(c.Mem, p.H())
}

func (g *giveGiftToHero) CanStillUse(c *jbrain.Ctx, _ *javamob.Mob) bool {
	_, ok := heroNearby(c)
	return ok && !g.given
}

func (g *giveGiftToHero) Tick(c *jbrain.Ctx, m *javamob.Mob) {
	p, ok := heroNearby(c)
	if !ok {
		return
	}
	jmob.LookAtEntity(c.Mem, p.H())
	if distSqPos(jbrain.Containing(p.Position()), BlockPos(m)) < 25 {
		if c.Time-g.started > 20 {
			if v, ok := Of(m); ok {
				for _, s := range v.giftLoot(m) {
					throwItem(m, s, p.Position())
				}
			}
			g.given = true
		}
	} else {
		jmob.SetWalkAndLookTargetMemories(c.Mem, jbrain.TrackEntity(p.H(), true), 0.5, 5)
	}
}

func (g *giveGiftToHero) Stop(c *jbrain.Ctx, _ *javamob.Mob) {
	g.untilNext = 600 + c.Rand.IntN(6001)
	c.Mem.Erase(jbrain.MemInteractionTarget.ID())
	c.Mem.Erase(jbrain.MemWalkTarget.ID())
	c.Mem.Erase(jbrain.MemLookTarget.ID())
}

// PlayTagWithOtherKids is PlayTagWithOtherKids.create.
func PlayTagWithOtherKids() jmob.Control {
	return jmob.Trigger("PlayTagWithOtherKids", []jbrain.Req{jbrain.Present(jbrain.MemVisibleVillagerBabies), jbrain.Absent(jbrain.MemWalkTarget), jbrain.Reg(jbrain.MemLookTarget), jbrain.Reg(jbrain.MemInteractionTarget)}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		if c.Rand.IntN(10) != 0 {
			return false
		}
		friends, _ := jbrain.Get(c.Mem, jbrain.MemVisibleVillagerBabies)
		chasing := func(h *world.EntityHandle) (*world.EntityHandle, bool) {
			fm, ok := javamob.OfHandle(c.Tx, h)
			if !ok {
				return nil, false
			}
			return jbrain.Get(jmob.Mem(fm), jbrain.MemInteractionTarget)
		}
		chased := false
		for _, f := range friends {
			if t, ok := chasing(f); ok && t == m.H() {
				chased = true
				break
			}
		}
		chase := func(h *world.EntityHandle) {
			jbrain.Set(c.Mem, jbrain.MemInteractionTarget, h)
			jbrain.Set[jbrain.PositionTracker](c.Mem, jbrain.MemLookTarget, jbrain.TrackEntity(h, true))
			jbrain.Set(c.Mem, jbrain.MemWalkTarget, jbrain.WalkTarget{Target: jbrain.TrackEntity(h, false), Speed: 0.6, CloseEnough: 1})
		}
		if !chased {
			counts := map[*world.EntityHandle]int{}
			var order []*world.EntityHandle
			for _, f := range friends {
				if t, ok := chasing(f); ok {
					if counts[t] == 0 {
						order = append(order, t)
					}
					counts[t]++
				}
			}
			slices.SortStableFunc(order, func(a, b *world.EntityHandle) int { return counts[a] - counts[b] })
			for _, t := range order {
				if counts[t] > 0 && counts[t] <= 5 {
					chase(t)
					return true
				}
			}
			if len(friends) > 0 {
				chase(friends[0])
			}
			return true
		}
		pm := jpoi.For(c.Tx)
		for range 10 {
			if p, ok := m.LandRandomPos(20, 8); ok && pm.IsVillage(c.Tx, jbrain.Containing(p)) {
				jbrain.Set(c.Mem, jbrain.MemWalkTarget, jbrain.WalkTarget{Target: jbrain.TrackVec(p), Speed: 0.6})
				break
			}
		}
		return true
	})
}

// SocializeAtBell is SocializeAtBell.create.
func SocializeAtBell() *jbrain.OneShot[*javamob.Mob] {
	return jmob.Trigger("SocializeAtBell", []jbrain.Req{jbrain.Reg(jbrain.MemWalkTarget), jbrain.Reg(jbrain.MemLookTarget), jbrain.Present(jbrain.MemMeetingPoint), jbrain.Present(jbrain.MemNearestVisibleLivingEntities), jbrain.Absent(jbrain.MemInteractionTarget)}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		g, _ := jbrain.Get(c.Mem, jbrain.MemMeetingPoint)
		if c.Rand.IntN(100) != 0 || !sameDim(c.Tx, g) || !jmob.CloserToCenterThan(g.Pos, m.Position(), 4) {
			return false
		}
		isVillager := func(h *world.EntityHandle) bool { return h.Type().EncodeEntity() == VillagerID }
		v, _ := jbrain.Get(c.Mem, jbrain.MemNearestVisibleLivingEntities)
		if !v.ContainsMatch(c.Tx, isVillager) {
			return false
		}
		if h, ok := v.FindClosest(c.Tx, func(h *world.EntityHandle) bool {
			e, ok := h.Entity(c.Tx)
			return ok && isVillager(h) && e.Position().Sub(m.Position()).LenSqr() <= 32
		}); ok {
			jbrain.Set(c.Mem, jbrain.MemInteractionTarget, h)
			jbrain.Set[jbrain.PositionTracker](c.Mem, jbrain.MemLookTarget, jbrain.TrackEntity(h, true))
			jbrain.Set(c.Mem, jbrain.MemWalkTarget, jbrain.WalkTarget{Target: jbrain.TrackEntity(h, false), Speed: 0.3, CloseEnough: 1})
		}
		return true
	})
}

// ReactToBell is ReactToBell.create: a bell heard outside a raid sends the
// villager to HIDE.
func ReactToBell() jmob.Control {
	return jmob.Trigger("ReactToBell", []jbrain.Req{jbrain.Present(jbrain.MemHeardBellTime)}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		if RaidHook == nil || !RaidHook(c.Tx, BlockPos(m)).Exists {
			if b, ok := jmob.Of(m); ok {
				b.SetActiveActivityIfPossible(jbrain.ActHide)
			}
		}
		return true
	})
}

// SetRaidStatus is SetRaidStatus.create.
func SetRaidStatus() jmob.Control {
	return jmob.Trigger("SetRaidStatus", nil, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		if c.Rand.IntN(20) != 0 {
			return false
		}
		if RaidHook == nil {
			return true
		}
		r := RaidHook(c.Tx, BlockPos(m))
		if b, ok := jmob.Of(m); ok && r.Exists {
			if r.FirstWaveSpawned && !r.BetweenWaves {
				b.SetDefaultActivity(jbrain.ActRaid)
				b.SetActiveActivityIfPossible(jbrain.ActRaid)
			} else {
				b.SetDefaultActivity(jbrain.ActPreRaid)
				b.SetActiveActivityIfPossible(jbrain.ActPreRaid)
			}
		}
		return true
	})
}

// ResetRaidStatus is ResetRaidStatus.create.
func ResetRaidStatus() jmob.Control {
	return jmob.Trigger("ResetRaidStatus", nil, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		if c.Rand.IntN(20) != 0 {
			return false
		}
		r := RaidStatus{}
		if RaidHook != nil {
			r = RaidHook(c.Tx, BlockPos(m))
		}
		if !r.Exists || r.Stopped || r.Loss {
			if b, ok := jmob.Of(m); ok {
				b.SetDefaultActivity(jbrain.ActIdle)
				b.UpdateActivityFromSchedule(c)
			}
		}
		return true
	})
}

// RaidStatus is what villagers read of the raid at their position
// (ServerLevel.getRaidAt).
type RaidStatus struct {
	Exists, Active, Over, Victory, Loss, Stopped, FirstWaveSpawned, BetweenWaves bool
}

// RaidHook is set by the raids package: the raid at pos.
var RaidHook func(tx *world.Tx, pos cube.Pos) RaidStatus
