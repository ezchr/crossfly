//go:build crossfly_full

package jvillage

import (
	"slices"
	"time"

	"github.com/df-mc/dragonfly/server/entity/effect"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/path"
	"github.com/df-mc/dragonfly/server/mob/jbrain"
	"github.com/df-mc/dragonfly/server/mob/jbrain/jmob"
	"github.com/df-mc/dragonfly/server/mob/jpoi"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// Event is a Java entity event (Level.broadcastEntityEvent) as a
// world.EntityAction: Bedrock viewers get ActorEvent, Java viewers the
// EntityEvent id (same shape as janimal.Event).
type Event byte

// The entity events villagers and golems send.
const (
	EventAttack     Event = 4
	EventFlower     Event = 11
	EventHearts     Event = 12
	EventAngry      Event = 13
	EventHappy      Event = 14
	EventInLove     Event = 18
	EventStopFlower Event = 34
	EventSplash     Event = 42
)

// EntityAction ...
func (Event) EntityAction() {}

// JavaEvent is the Java entity event id.
func (e Event) JavaEvent() byte { return byte(e) }

// ActorEvent is the Bedrock actor event shown for it.
func (e Event) ActorEvent() uint8 {
	switch e {
	case EventAttack:
		return packet.ActorEventStartAttacking
	case EventFlower:
		return packet.ActorEventStartOfferFlower
	case EventStopFlower:
		return packet.ActorEventStopOfferFlower
	case EventHearts, EventInLove:
		return packet.ActorEventLoveHearts
	case EventAngry:
		return packet.ActorEventVillagerAngry
	case EventHappy:
		return packet.ActorEventVillagerHappy
	}
	return 0
}

// broadcastEvent is Level.broadcastEntityEvent.
func broadcastEvent(m *javamob.Mob, e Event) {
	for _, v := range m.Tx().Viewers(m.Position()) {
		v.ViewEntityAction(m, e)
	}
}

// villagerProfessionIndex and villagerTypeIndex are the villager_v2 Variant
// and MarkVariant orders both clients read (javasession mob_tables.go).
var (
	villagerProfessionIndex = []string{"none", "farmer", "fisherman", "shepherd", "fletcher", "librarian", "cartographer", "cleric", "armorer", "weaponsmith", "toolsmith", "butcher", "leatherworker", "mason", "nitwit"}
	villagerTypeIndex       = []string{TypePlains, TypeDesert, TypeJungle, TypeSavanna, TypeSnow, TypeSwamp, TypeTaiga}
)

// syncData shows the villager data (DATA_VILLAGER_DATA): profession,
// biome type and level through the villager_v2 variants.
func (v *Villager) syncData(m *javamob.Mob) {
	prof := slices.Index(villagerProfessionIndex, v.profession().Name)
	typ := slices.Index(villagerTypeIndex, v.Data.Type)
	m.UpdateMeta(func(meta *mob.Meta) {
		meta.Variant = int32(max(prof, 0))
		meta.MarkVariant = int32(max(typ, 0))
		meta.TradeTier = int32(max(v.Data.Level-1, 0))
		meta.TradeExperience = int32(v.xp)
	})
}

// professionChanged is Villager.setVillagerData with a new profession: the
// offers are dropped.
func (v *Villager) professionChanged(m *javamob.Mob) {
	v.offers, v.hasOffers = nil, false
	v.syncData(m)
}

// releasePoi is Villager.releasePoi.
func (v *Villager) releasePoi(m *javamob.Mob, mem posMem) {
	if v.brain == nil {
		return
	}
	g, ok := jbrain.Get(v.brain.Mem(), mem)
	if !ok || !sameDim(m.Tx(), g) {
		return
	}
	pm := jpoi.For(m.Tx())
	t, ok := pm.Type(m.Tx(), g.Pos)
	if ok && v.poiMemoryAccepts(mem, t) {
		pm.Release(m.Tx(), g.Pos)
	}
}

// poiMemoryAccepts is Villager.POI_MEMORIES.
func (v *Villager) poiMemoryAccepts(mem posMem, t *jpoi.Type) bool {
	switch mem {
	case jbrain.MemHome:
		return t == jpoi.Home
	case jbrain.MemJobSite:
		return v.profession().HeldJobSite(t)
	case jbrain.MemPotentialJobSite:
		return jpoi.IsAcquirableJobSite(t)
	case jbrain.MemMeetingPoint:
		return t == jpoi.Meeting
	}
	return false
}

// ReleaseAllPois is Villager.releaseAllPois.
func (v *Villager) ReleaseAllPois(m *javamob.Mob) {
	for _, mem := range []posMem{jbrain.MemHome, jbrain.MemJobSite, jbrain.MemPotentialJobSite, jbrain.MemMeetingPoint} {
		v.releasePoi(m, mem)
	}
}

// reclaimPois takes the tickets of the PoIs a villager loaded from disk
// remembers: Java saves claims with the PoI data, Crossfly rebuilds it.
func (v *Villager) reclaimPois(m *javamob.Mob) {
	if v.brain == nil {
		return
	}
	pm := jpoi.For(m.Tx())
	for _, mem := range []posMem{jbrain.MemHome, jbrain.MemJobSite, jbrain.MemPotentialJobSite, jbrain.MemMeetingPoint} {
		if g, ok := jbrain.Get(v.brain.Mem(), mem); ok && sameDim(m.Tx(), g) {
			pm.Claim(m.Tx(), g.Pos, func(t *jpoi.Type) bool { return v.poiMemoryAccepts(mem, t) })
		}
	}
}

// OnHurt is Villager.setLastHurtByMob: a living attacker loses reputation,
// players make the villager angry.
func (v *Villager) OnHurt(m *javamob.Mob, dealt float64, src world.DamageSource) {
	att, ok := jmob.SourceEntity(src)
	if !ok || !jmob.IsLiving(att) {
		return
	}
	v.OnReputationEvent(m, ReputationVillagerHurt, att)
	if _, ok := att.(*player.Player); ok && !m.Dead() {
		broadcastEvent(m, EventAngry)
	}
}

// OnDeath is Villager.die: witnesses hear of the murder, PoIs are freed,
// trading stops.
func (v *Villager) OnDeath(m *javamob.Mob, src world.DamageSource) {
	if murderer, ok := jmob.SourceEntity(src); ok && v.brain != nil {
		if vis, ok := jbrain.Get(v.brain.Mem(), jbrain.MemNearestVisibleLivingEntities); ok {
			for _, h := range vis.FindAll(m.Tx(), func(*world.EntityHandle) bool { return true }) {
				if wm, ok := javamob.OfHandle(m.Tx(), h); ok {
					if wv, ok := Of(wm); ok {
						wv.OnReputationEvent(wm, ReputationVillagerKilled, murderer)
					}
				}
			}
		}
	}
	v.ReleaseAllPois(m)
	if v.tradingPlayer != nil {
		v.stopTrading(m)
	}
	// Zombie.killedEntity: a zombie's victim may rise as a zombie villager.
	if killer, ok := jmob.SourceEntity(src); ok {
		villagerKilledBy(m, killer)
	}
}

// RemoveWhenFarAway is Villager.removeWhenFarAway: never.
func (v *Villager) RemoveWhenFarAway(*javamob.Mob, float64) bool { return false }

// FinalizeSpawn is Villager.finalizeSpawn: bred villagers have no
// profession; the type follows the biome unless set.
func (v *Villager) FinalizeSpawn(m *javamob.Mob, reason javamob.SpawnReason) {
	if reason == javamob.SpawnBreeding {
		v.Data.Profession = ProfNone
	}
	if !v.DataFinalized {
		v.Data.Type = TypeByBedrockBiome(biomeName(m.Tx(), BlockPos(m)))
		v.DataFinalized = true
	}
	v.syncData(m)
}

// HeroAmplifier is the amplifier of the player's Hero of the Village
// effect (Bedrock effect id 29), if it has one.
func HeroAmplifier(p *player.Player) (int, bool) {
	t, ok := effect.ByID(29)
	if !ok {
		return 0, false
	}
	for _, e := range p.Effects() {
		if e.Type() == t {
			return e.Level() - 1, true
		}
	}
	return 0, false
}

// mobMeta is the client-visible state of a mob.
type mobMeta = mob.Meta

// ticksDuration converts ticks to a duration.
func ticksDuration(t int) time.Duration { return time.Duration(t) * 50 * time.Millisecond }

// giftLoot rolls the gameplay/hero_of_the_village/<profession>_gift loot
// table.
func (v *Villager) giftLoot(m *javamob.Mob) []item.Stack {
	name := "unemployed_gift"
	if v.Baby() {
		name = "baby_gift"
	} else if p := v.profession(); p != ProfNone && p != ProfNitwit {
		name = p.Name + "_gift"
	}
	// The gameplay/hero_of_the_village loot tables are not bundled yet.
	_ = name
	return nil
}

// spawnXP drops an experience orb of n points at the villager (+0.5 up).
func spawnXP(m *javamob.Mob, n int) {
	pos := m.Position().Add(mgl64.Vec3{0, 0.5, 0})
	for _, orb := range entity.NewExperienceOrbs(pos, n) {
		m.Tx().AddEntity(orb)
	}
}

// InteractWithDoor is InteractWithDoor.create: open the wooden doors on the
// path and close the ones passed.
func InteractWithDoor() jmob.Control {
	var last *path.Node
	cooldown := 0
	return jmob.Trigger("InteractWithDoor", []jbrain.Req{jbrain.Present(jbrain.MemPath), jbrain.Reg(jbrain.MemDoorsToClose), jbrain.Reg(jbrain.MemNearestLivingEntities)}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		pv, _ := jbrain.Get(c.Mem, jbrain.MemPath)
		pa, ok := pv.(*path.Path)
		if !ok || pa == nil || pa.NotStarted() || pa.Done() {
			return false
		}
		next := pa.NextNode()
		if last == next {
			cooldown = 20
		} else {
			cooldown--
			if cooldown > 0 {
				return false
			}
		}
		last = next
		from := pa.PreviousNode()
		doors, _ := jbrain.Get(c.Mem, jbrain.MemDoorsToClose)
		remember := func(p cube.Pos) {
			g := jbrain.GlobalPos{Dim: c.Tx.World().Dimension(), Pos: p}
			if !slices.Contains(doors, g) {
				doors = append(doors, g)
			}
		}
		if from != nil {
			if d, ok := c.Tx.Block(from.Pos()).(block.WoodDoor); ok {
				if !d.Open {
					setDoorOpen(c.Tx, from.Pos(), d, true)
				}
				remember(from.Pos())
			}
		}
		if d, ok := c.Tx.Block(next.Pos()).(block.WoodDoor); ok && !d.Open {
			setDoorOpen(c.Tx, next.Pos(), d, true)
			remember(next.Pos())
		}
		var fp *cube.Pos
		if from != nil {
			p := from.Pos()
			fp = &p
		}
		np := next.Pos()
		doors = closeDoors(c, m, fp, &np, doors)
		jbrain.Set(c.Mem, jbrain.MemDoorsToClose, doors)
		return true
	})
}

// setDoorOpen is DoorBlock.setOpen: both halves, with the sound.
func setDoorOpen(tx *world.Tx, pos cube.Pos, d block.WoodDoor, open bool) {
	other := pos.Side(cube.FaceUp)
	if d.Top {
		other = pos.Side(cube.FaceDown)
	}
	d.Open = open
	tx.SetBlock(pos, d, nil)
	if od, ok := tx.Block(other).(block.WoodDoor); ok {
		od.Open = open
		tx.SetBlock(other, od, nil)
	}
	if open {
		tx.PlaySound(pos.Vec3Centre(), sound.DoorOpen{Block: d})
	} else {
		tx.PlaySound(pos.Vec3Centre(), sound.DoorClose{Block: d})
	}
}

// closeDoorsThatIHaveOpened is InteractWithDoor.closeDoorsThatIHaveOpenedOrPassedThrough
// without the nodes (SleepInBed).
func closeDoorsThatIHaveOpened(c *jbrain.Ctx, m *javamob.Mob, doors []jbrain.GlobalPos) {
	jbrain.Set(c.Mem, jbrain.MemDoorsToClose, closeDoors(c, m, nil, nil, doors))
}

func closeDoors(c *jbrain.Ctx, m *javamob.Mob, from, to *cube.Pos, doors []jbrain.GlobalPos) []jbrain.GlobalPos {
	out := doors[:0]
	for _, g := range doors {
		if (from != nil && *from == g.Pos) || (to != nil && *to == g.Pos) {
			out = append(out, g)
			continue
		}
		if !sameDim(c.Tx, g) || !jmob.CloserToCenterThan(g.Pos, m.Position(), 3) {
			continue
		}
		d, ok := c.Tx.Block(g.Pos).(block.WoodDoor)
		if !ok || !d.Open || othersComingThrough(c, m, g.Pos) {
			continue
		}
		setDoorOpen(c.Tx, g.Pos, d, false)
	}
	return out
}

func othersComingThrough(c *jbrain.Ctx, m *javamob.Mob, door cube.Pos) bool {
	list, _ := jbrain.Get(c.Mem, jbrain.MemNearestLivingEntities)
	for _, h := range list {
		om, ok := javamob.OfHandle(c.Tx, h)
		if !ok || om.JavaType() != m.JavaType() || !jmob.CloserToCenterThan(door, om.Position(), 2) {
			continue
		}
		mem := jmob.Mem(om)
		pv, ok := jbrain.Get(mem, jbrain.MemPath)
		pa, _ := pv.(*path.Path)
		if !ok || pa == nil || pa.Done() {
			continue
		}
		prev := pa.PreviousNode()
		if prev != nil && (prev.Pos() == door || pa.NextNode().Pos() == door) {
			return true
		}
	}
	return false
}

// Persistence (Villager.addAdditionalSaveData / readAdditionalSaveData).

// EncodeNBT writes the villager's save data with Java's keys.
func (v *Villager) EncodeNBT(data map[string]any) {
	v.Ageable.EncodeNBT(data)
	data["VillagerData"] = v.Data.NBT()
	data["VillagerDataFinalized"] = boolByte(v.DataFinalized)
	data["FoodLevel"] = uint8(v.foodLevel)
	data["Gossips"] = v.gossips.NBT()
	data["Xp"] = int32(v.xp)
	data["LastRestock"] = v.lastRestock
	data["LastGossipDecay"] = v.lastGossipDecayTime
	data["RestocksToday"] = int32(v.restocksToday)
	if v.hasOffers {
		data["Offers"] = v.offers.NBT()
	}
	inv := make([]any, 0, len(v.inventory))
	for _, s := range v.inventory {
		if !s.Empty() {
			inv = append(inv, item.WriteNBT(s, true))
		}
	}
	data["Inventory"] = inv
	if v.brain != nil {
		data["Brain"] = v.brain.Pack().NBT()
	}
}

// DecodeNBT reads what EncodeNBT wrote; the brain is rebuilt with the
// saved memories and the claimed PoIs are taken again.
func (v *Villager) DecodeNBT(m *javamob.Mob, data map[string]any) {
	v.Ageable.DecodeNBT(data)
	if d, ok := data["VillagerData"].(map[string]any); ok {
		v.Data = DataFromNBT(d)
		v.DataFinalized = true
	} else if b, ok := data["VillagerDataFinalized"].(uint8); ok && b != 0 {
		v.DataFinalized = true
	}
	if f, ok := data["FoodLevel"].(uint8); ok {
		v.foodLevel = int(int8(f))
	}
	l, _ := data["Gossips"].([]any)
	v.gossips = *GossipsFromNBT(l)
	v.xp = int(i32(data["Xp"], 0))
	v.lastRestock, _ = data["LastRestock"].(int64)
	v.lastGossipDecayTime, _ = data["LastGossipDecay"].(int64)
	v.restocksToday = int(i32(data["RestocksToday"], 0))
	if o, ok := data["Offers"].(map[string]any); ok {
		v.offers, v.hasOffers = OffersFromNBT(o), true
	}
	if inv, ok := data["Inventory"].([]any); ok {
		i := 0
		for _, x := range inv {
			if c, ok := x.(map[string]any); ok && i < len(v.inventory) {
				if s := item.ReadNBT(c, nil); !s.Empty() {
					v.inventory[i] = s
					i++
				}
			}
		}
	}
	if b, ok := data["Brain"].(map[string]any); ok {
		v.saved = jbrain.Unpack(b)
	}
	m.SetBaby(v.Baby())
	v.refreshBrain(m)
	v.reclaimPois(m)
	v.syncData(m)
}
