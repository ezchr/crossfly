//go:build crossfly_full

package jvillage

import (
	"slices"
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/janimal"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/path"
	"github.com/df-mc/dragonfly/server/mob/jbrain"
	"github.com/df-mc/dragonfly/server/mob/jbrain/jmob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// VillagerID is the villager's entity identifier.
const VillagerID = "minecraft:villager_v2"

func init() {
	javamob.Register(javamob.Type{
		ID: VillagerID, Category: javamob.CategoryMisc,
		Width: 0.6, Height: 1.95, EyeHeight: 1.62,
		// Villager.createAttributes: Mob.createMobAttributes + speed 0.5.
		Attributes: func() map[javamob.Attribute]float64 {
			a := javamob.LivingAttributes()
			a[javamob.AttrFollowRange] = 16
			a[javamob.AttrMovementSpeed] = 0.5
			return a
		},
		Families:    []string{"villager", "peasant", "mob"},
		NoLootTable: true,
		New:         func() javamob.Behaviour { return &Villager{Data: DefaultData()} },
	})
}

// The Villager constants (Villager.java).
const (
	breedingFoodThreshold   = 12
	maxGossipTopics         = 10
	gossipCooldown          = 1200
	gossipDecayInterval     = 24000
	golemTalkDistance       = 10
	villagersNeededForGolem = 5
	sleptForGolemSpawning   = 24000
	speedModifier           = 0.5
)

// Villager is Java's Villager (and AbstractVillager): its data, food,
// gossips, trades and brain.
type Villager struct {
	janimal.Ageable
	merchant

	Data          Data
	DataFinalized bool

	foodLevel           int
	gossips             Gossips
	lastGossipTime      int64
	lastGossipDecayTime int64
	xp                  int
	lastRestock         int64
	restocksToday       int
	lastRestockCheckDay int64

	inventory [8]item.Stack
	unhappy   int

	sleeping bool
	sleepPos cube.Pos

	brain *jmob.Brain
	saved *jbrain.Packed
}

// Of returns the villager behaviour of m.
func Of(m *javamob.Mob) (*Villager, bool) {
	v, ok := m.Behaviour().(*Villager)
	return v, ok
}

// RegisterGoals is the villager constructor and makeBrain: navigation that
// opens doors and floats, picking up loot, the brain.
func (v *Villager) RegisterGoals(m *javamob.Mob) {
	nav := m.Navigation()
	nav.SetCanOpenDoors(true)
	nav.SetCanFloat(true)
	nav.SetRequiredPathLength(m, 48)
	m.SetCanPickUpLoot(true)
	// AbstractVillager: fire in a neighbour costs 16, fire is blocked.
	m.SetPathfindingMalus(path.FireInNeighbour, 16)
	m.SetPathfindingMalus(path.Fire, -1)
	v.makeBrain(m)
}

// Baby is AgeableMob.isBaby.
func (v *Villager) Baby() bool { return v.Ageable.Baby() }

func (v *Villager) makeBrain(m *javamob.Mob) {
	v.brain = jmob.Attach(m, provider, v.saved)
	v.saved = nil
	v.registerBrainGoals(m)
}

// refreshBrain is Villager.refreshBrain: stop everything and rebuild the
// brain (new profession, grown up) keeping the memories.
func (v *Villager) refreshBrain(m *javamob.Mob) {
	if v.brain != nil {
		v.brain.StopAll(jmob.Ctx(m), m)
		v.saved = v.brain.Pack()
	}
	v.makeBrain(m)
}

// registerBrainGoals is Villager.registerBrainGoals: the schedule by age,
// then an immediate schedule update.
func (v *Villager) registerBrainGoals(m *javamob.Mob) {
	baby := v.Baby()
	v.brain.SetSchedule(func(c *jbrain.Ctx) (jbrain.Activity, bool) { return scheduled(c.Tx, baby) })
	v.brain.UpdateActivityFromSchedule(v.brain.Ctx(jmob.Ctx(m)))
}

// scheduled reads the minecraft:gameplay/(baby_)villager_activity tracks of
// the overworld timeline (timeline/villager_schedule.json). Other
// dimensions have no timeline: idle.
func scheduled(tx *world.Tx, baby bool) (jbrain.Activity, bool) {
	if tx == nil || tx.World().Dimension() != world.Overworld {
		return jbrain.ActIdle, true
	}
	t := int64(tx.World().Time()) % 24000
	if t < 0 {
		t += 24000
	}
	keys := adultSchedule
	if baby {
		keys = babySchedule
	}
	a := keys[len(keys)-1].a
	for _, k := range keys {
		if t >= k.t {
			a = k.a
		}
	}
	return a, true
}

type scheduleKey struct {
	t int64
	a jbrain.Activity
}

var (
	adultSchedule = []scheduleKey{{10, jbrain.ActIdle}, {2000, jbrain.ActWork}, {9000, jbrain.ActMeet}, {11000, jbrain.ActIdle}, {12000, jbrain.ActRest}}
	babySchedule  = []scheduleKey{{10, jbrain.ActIdle}, {3000, jbrain.ActPlay}, {6000, jbrain.ActIdle}, {10000, jbrain.ActPlay}, {12000, jbrain.ActRest}}
)

// Tick is Villager.tick: the unhappy counter and gossip decay (before
// super.tick in Java order does not matter: neither reads the other).
func (v *Villager) Tick(m *javamob.Mob) {
	if v.unhappy > 0 {
		v.unhappy--
	}
	v.maybeDecayGossip(m.Tx().CurrentTick())
}

// AiStep is Mob.aiStep's item pickup and AgeableMob.aiStep: growing up;
// crossing the boundary refreshes the brain (ageBoundaryReached).
func (v *Villager) AiStep(m *javamob.Mob) {
	v.pickUpItems(m)
	if t := v.Ageable.Tick(!m.Dead()); t.Boundary {
		m.SetBaby(v.Baby())
		v.refreshBrain(m)
	}
}

// CustomServerAiStep is Villager.customServerAiStep after the brain tick:
// the trade reputation event, and closing an empty trading screen.
func (v *Villager) CustomServerAiStep(m *javamob.Mob) {
	tx := m.Tx()
	if v.lastTradedPlayer != nil {
		if p, ok := v.lastTradedPlayer.Entity(tx); ok {
			v.OnReputationEvent(m, ReputationTrade, p)
			broadcastEvent(m, EventHappy)
		}
		v.lastTradedPlayer = nil
	}
	if RaidHook != nil && m.Rand().IntN(100) == 0 {
		if r := RaidHook(tx, BlockPos(m)); r.Active && !r.Over {
			broadcastEvent(m, EventSplash)
		}
	}
	if (!v.hasOffers || len(v.offers) == 0) && v.tradingPlayer != nil {
		v.stopTrading(m)
	}
	checkTradeScreen(v, m)
}

// maybeDecayGossip is Villager.maybeDecayGossip.
func (v *Villager) maybeDecayGossip(now int64) {
	if v.lastGossipDecayTime == 0 {
		v.lastGossipDecayTime = now
	} else if now >= v.lastGossipDecayTime+gossipDecayInterval {
		v.gossips.Decay()
		v.lastGossipDecayTime = now
	}
}

// BlockPos is Entity.blockPosition.
func BlockPos(m *javamob.Mob) cube.Pos { return jmob.BlockPos(m) }

// Gossips returns the villager's gossips.
func (v *Villager) Gossips() *Gossips { return &v.gossips }

// XP returns the villager's trading experience.
func (v *Villager) XP() int { return v.xp }

// FoodLevel returns the food the villager has eaten.
func (v *Villager) FoodLevel() int { return v.foodLevel }

// Inventory returns the villager's 8 slots.
func (v *Villager) Inventory() []item.Stack { return v.inventory[:] }

// SetUnhappy is Villager.setUnhappy: shake the head and say no.
func (v *Villager) SetUnhappy(m *javamob.Mob) {
	v.unhappy = 40
	m.PlaySound("no")
}

// Sleeping is LivingEntity.isSleeping.
func (v *Villager) Sleeping() bool { return v.sleeping }

// StartSleeping is LivingEntity.startSleeping: lie in the bed at bed (the
// bed becomes occupied).
func (v *Villager) StartSleeping(m *javamob.Mob, bed cube.Pos) bool {
	tx := m.Tx()
	b, ok := tx.Block(bed).(block.Bed)
	if !ok {
		return false
	}
	b.Sleeper = m.H()
	tx.SetBlock(bed, b, nil)
	v.sleeping, v.sleepPos = true, bed
	m.SetPosition(mgl64.Vec3{float64(bed[0]) + .5, float64(bed[1]) + 0.6875, float64(bed[2]) + .5})
	m.SetVelocity(mgl64.Vec3{})
	m.SetActorFlag(protocol.EntityDataFlagSleeping, true)
	return true
}

// StopSleeping is Villager.stopSleeping (LivingEntity.stopSleeping + the
// LAST_WOKEN memory): leave the bed free and stand up beside it.
func (v *Villager) StopSleeping(m *javamob.Mob) {
	if !v.sleeping {
		return
	}
	tx := m.Tx()
	if b, ok := tx.Block(v.sleepPos).(block.Bed); ok {
		b.Sleeper = nil
		tx.SetBlock(v.sleepPos, b, nil)
		if p, ok := standUpPosition(tx, v.sleepPos, b); ok {
			m.SetPosition(p)
		}
	} else {
		m.SetPosition(mgl64.Vec3{float64(v.sleepPos[0]) + .5, float64(v.sleepPos[1]) + 1, float64(v.sleepPos[2]) + .5})
	}
	v.sleeping = false
	m.SetActorFlag(protocol.EntityDataFlagSleeping, false)
	if v.brain != nil {
		jbrain.Set(v.brain.Mem(), jbrain.MemLastWoken, tx.CurrentTick())
	}
}

// standUpPosition is BedBlock.findStandUpPosition, simplified to the first
// free spot around the bed's head, then on top of it.
func standUpPosition(tx *world.Tx, bed cube.Pos, b block.Bed) (mgl64.Vec3, bool) {
	for _, d := range []cube.Pos{{1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}} {
		p := bed.Add(d)
		if isAirish(tx, p) && isAirish(tx, p.Side(cube.FaceUp)) && !isAirish(tx, p.Side(cube.FaceDown)) {
			return mgl64.Vec3{float64(p[0]) + .5, float64(p[1]), float64(p[2]) + .5}, true
		}
	}
	return mgl64.Vec3{float64(bed[0]) + .5, float64(bed[1]) + 0.5625, float64(bed[2]) + .5}, true
}

func isAirish(tx *world.Tx, p cube.Pos) bool {
	_, ok := tx.Block(p).(block.Air)
	return ok
}

// food is the minecraft:villager_food component (26.3 item components).
func food(s item.Stack) int {
	if s.Empty() {
		return 0
	}
	switch javaNameOf(s) {
	case "bread":
		return 4
	case "carrot", "potato", "beetroot":
		return 1
	}
	return 0
}

func (v *Villager) countFoodPoints() int {
	n := 0
	for _, s := range v.inventory {
		n += s.Count() * food(s)
	}
	return n
}

// CanBreed is Villager.canBreed.
func (v *Villager) CanBreed() bool {
	return v.foodLevel+v.countFoodPoints() >= breedingFoodThreshold && !v.sleeping && v.Age() == 0
}

// HasExcessFood is Villager.hasExcessFood.
func (v *Villager) HasExcessFood() bool { return v.countFoodPoints() >= 24 }

// WantsMoreFood is Villager.wantsMoreFood.
func (v *Villager) WantsMoreFood() bool { return v.countFoodPoints() < 12 }

// eatUntilFull is Villager.eatUntilFull.
func (v *Villager) eatUntilFull() {
	if v.foodLevel >= breedingFoodThreshold {
		return
	}
	for i, s := range v.inventory {
		n := food(s)
		if n == 0 {
			continue
		}
		remove := 0
		for c := s.Count(); c > 0; c-- {
			v.foodLevel += n
			remove++
			if v.foodLevel >= breedingFoodThreshold {
				v.inventory[i] = s.Grow(-remove)
				return
			}
		}
		v.inventory[i] = s.Grow(-remove)
	}
}

// EatAndDigestFood is Villager.eatAndDigestFood.
func (v *Villager) EatAndDigestFood() {
	v.eatUntilFull()
	v.foodLevel -= breedingFoodThreshold
}

// javaNameOf returns the Java item name of a stack, "" for none.
func javaNameOf(s item.Stack) string {
	if s.Empty() {
		return ""
	}
	n, _ := JavaName(s.Item())
	return n
}

// villagerPicksUp is the #minecraft:villager_picks_up item tag.
var villagerPicksUp = []string{"wheat_seeds", "potato", "carrot", "beetroot_seeds", "torchflower_seeds", "pitcher_pod", "bread", "wheat", "beetroot"}

// villagerPlantableSeeds is #minecraft:villager_plantable_seeds.
var villagerPlantableSeeds = []string{"wheat_seeds", "potato", "carrot", "beetroot_seeds", "torchflower_seeds", "pitcher_pod"}

// WantsToPickUp is Villager.wantsToPickUp.
func (v *Villager) WantsToPickUp(s item.Stack) bool {
	n := javaNameOf(s)
	if !(slices.Contains(villagerPicksUp, n) || food(s) > 0 || slices.Contains(v.profession().RequestedItems, n)) {
		return false
	}
	return v.canAddItem(s)
}

func (v *Villager) profession() *Profession {
	if v.Data.Profession == nil {
		return ProfNone
	}
	return v.Data.Profession
}

// canAddItem is SimpleContainer.canAddItem.
func (v *Villager) canAddItem(s item.Stack) bool {
	for _, x := range v.inventory {
		if x.Empty() || (x.Comparable(s) && x.Count() < x.MaxCount()) {
			return true
		}
	}
	return false
}

// AddItem is SimpleContainer.addItem: merge into matching stacks, then into
// empty slots; the rest is returned.
func (v *Villager) AddItem(s item.Stack) item.Stack {
	for i, x := range v.inventory {
		if s.Empty() {
			break
		}
		if !x.Empty() && x.Comparable(s) {
			n := min(s.Count(), x.MaxCount()-x.Count())
			if n > 0 {
				v.inventory[i] = x.Grow(n)
				s = s.Grow(-n)
			}
		}
	}
	for i, x := range v.inventory {
		if s.Empty() {
			break
		}
		if x.Empty() {
			v.inventory[i] = s
			s = item.Stack{}
		}
	}
	return s
}

// CountItem is SimpleContainer.countItem by Java item name.
func (v *Villager) CountItem(javaName string) int {
	n := 0
	for _, x := range v.inventory {
		if javaNameOf(x) == javaName {
			n += x.Count()
		}
	}
	return n
}

// HasFarmSeeds is Villager.hasFarmSeeds.
func (v *Villager) HasFarmSeeds() bool {
	for _, x := range v.inventory {
		if slices.Contains(villagerPlantableSeeds, javaNameOf(x)) {
			return true
		}
	}
	return false
}

// thrownUntil holds the item entities villagers threw and the tick until
// which they may not be picked up (ItemEntity.setDefaultPickUpDelay, 40
// ticks): Dragonfly keeps the pickup delay to itself.
var (
	thrownMu    sync.Mutex
	thrownUntil = map[*world.EntityHandle]int64{}
)

func markThrown(h *world.EntityHandle, until int64) {
	thrownMu.Lock()
	defer thrownMu.Unlock()
	if len(thrownUntil) > 4096 {
		for k, t := range thrownUntil {
			if t < until-40 {
				delete(thrownUntil, k)
			}
		}
	}
	thrownUntil[h] = until
}

func pickupBlocked(h *world.EntityHandle, now int64) bool {
	thrownMu.Lock()
	defer thrownMu.Unlock()
	until, ok := thrownUntil[h]
	if !ok {
		return false
	}
	if now < until {
		return true
	}
	delete(thrownUntil, h)
	return false
}

// pickUpItems is the item part of Mob.aiStep: take wanted items within
// reach (1, 0, 1) (InventoryCarrier.pickUpItem).
func (v *Villager) pickUpItems(m *javamob.Mob) {
	if !m.CanPickUpLoot() || m.Dead() || !mobGriefing(m.Tx()) {
		return
	}
	tx := m.Tx()
	now := tx.CurrentTick()
	reach := m.BBox().GrowVec3(mgl64.Vec3{1, 0, 1})
	for e := range tx.EntitiesWithin(reach) {
		ent, ok := e.(*entity.Ent)
		if !ok {
			continue
		}
		b, ok := ent.Behaviour().(*entity.ItemBehaviour)
		if !ok {
			continue
		}
		if pickupBlocked(e.H(), now) {
			continue
		}
		s := b.Item()
		if s.Empty() || !v.WantsToPickUp(s) {
			continue
		}
		rest := v.AddItem(s)
		pos, vel := ent.Position(), ent.Velocity()
		_ = ent.Close()
		if !rest.Empty() {
			tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: pos, Velocity: vel}, rest))
		}
	}
}
