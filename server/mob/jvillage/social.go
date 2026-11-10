//go:build crossfly_full

package jvillage

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/jbrain"
	"github.com/df-mc/dragonfly/server/mob/jbrain/jmob"
	"github.com/df-mc/dragonfly/server/mob/jpoi"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// ReputationEvent is Java's ReputationEventType.
type ReputationEvent uint8

const (
	ReputationZombieVillagerCured ReputationEvent = iota
	ReputationGolemKilled
	ReputationVillagerHurt
	ReputationVillagerKilled
	ReputationTrade
)

// OnReputationEvent is Villager.onReputationEventFrom (ServerLevel.onReputationEvent).
func (v *Villager) OnReputationEvent(m *javamob.Mob, t ReputationEvent, source world.Entity) {
	id := source.H().UUID()
	switch t {
	case ReputationZombieVillagerCured:
		v.gossips.Add(id, MajorPositive, 20)
		v.gossips.Add(id, MinorPositive, 25)
	case ReputationTrade:
		v.gossips.Add(id, Trading, 2)
	case ReputationVillagerHurt:
		v.gossips.Add(id, MinorNegative, 25)
	case ReputationVillagerKilled:
		v.gossips.Add(id, MajorNegative, 25)
	}
	if p, ok := v.tradingPlayerEntity(m); ok && p.H().UUID() == id {
		v.updateSpecialPrices(m, p)
	}
}

// PlayerReputation is Villager.getPlayerReputation.
func (v *Villager) PlayerReputation(p *player.Player) int { return v.gossips.Reputation(p.UUID(), nil) }

// Gossip is Villager.gossip: two villagers that have not gossiped for 1200
// ticks share up to 10 gossips, then the speaker may summon a golem.
func (v *Villager) Gossip(m *javamob.Mob, target *javamob.Mob, now int64) {
	tv, ok := Of(target)
	if !ok {
		return
	}
	free := func(last int64) bool { return now < last || now >= last+gossipCooldown }
	if !free(v.lastGossipTime) || !free(tv.lastGossipTime) {
		return
	}
	n := v.gossips.TransferFrom(&tv.gossips, m.Rand(), maxGossipTopics)
	v.lastGossipTime, tv.lastGossipTime = now, now
	v.SpawnGolemIfNeeded(m, now, villagersNeededForGolem)
	if p, ok := v.tradingPlayerEntity(m); ok && n > 0 {
		v.updateSpecialPrices(m, p)
	}
}

// golemSpawnConditionsMet is Villager.golemSpawnConditionsMet: slept within
// the last day.
func (v *Villager) golemSpawnConditionsMet(now int64) bool {
	if v.brain == nil {
		return false
	}
	t, ok := jbrain.Get(v.brain.Mem(), jbrain.MemLastSlept)
	return ok && now-t < sleptForGolemSpawning
}

// WantsToSpawnGolem is Villager.wantsToSpawnGolem.
func (v *Villager) WantsToSpawnGolem(now int64) bool {
	return v.golemSpawnConditionsMet(now) && !v.brain.Mem().Has(jbrain.MemGolemDetectedRecently.ID())
}

// golemDetected is GolemSensor.golemDetected.
func golemDetected(m *javamob.Mob) {
	if mem := jmob.Mem(m); mem != nil {
		jbrain.SetWithExpiry(mem, jbrain.MemGolemDetectedRecently, true, 599)
	}
}

// villagersIn lists the villagers whose boxes touch box
// (getEntitiesOfClass(Villager.class, box)).
func villagersIn(tx *world.Tx, box cube.BBox) []*javamob.Mob {
	var out []*javamob.Mob
	for e := range tx.EntitiesWithin(box) {
		jm, ok := javamob.Of(e)
		if !ok {
			continue
		}
		if _, ok := Of(jm); ok && jm.BBox().IntersectsWith(box) {
			out = append(out, jm)
		}
	}
	return out
}

// SpawnGolemIfNeeded is Villager.spawnGolemIfNeeded: if this villager and
// enough others within 10 blocks want a golem (slept in the last day, no
// golem seen recently), try to summon one (SpawnUtil.trySpawnMob with the
// legacy iron golem strategy: 10 attempts within 8 blocks, 6 up/down).
func (v *Villager) SpawnGolemIfNeeded(m *javamob.Mob, now int64, needed int) {
	if !v.WantsToSpawnGolem(now) {
		return
	}
	tx := m.Tx()
	nearby := villagersIn(tx, m.BBox().Grow(golemTalkDistance))
	want := 0
	for _, o := range nearby {
		if ov, ok := Of(o); ok && ov.WantsToSpawnGolem(now) {
			want++
			if want >= 5 {
				break
			}
		}
	}
	if want < needed {
		return
	}
	if _, ok := trySpawnGolem(tx, BlockPos(m), m.Rand()); ok {
		for _, o := range nearby {
			golemDetected(o)
		}
	}
}

// trySpawnGolem is SpawnUtil.trySpawnMob(IRON_GOLEM, MOB_SUMMONED, level, pos,
// 10, 8, 6, LEGACY_IRON_GOLEM, false).
func trySpawnGolem(tx *world.Tx, start cube.Pos, r interface{ IntN(int) int }) (*javamob.Mob, bool) {
	const attempts, rangeXZ, rangeY = 10, 8, 6
	for range attempts {
		dx := r.IntN(2*rangeXZ+1) - rangeXZ
		dz := r.IntN(2*rangeXZ+1) - rangeXZ
		pos := start.Add(cube.Pos{dx, rangeY, dz})
		pos, ok := moveToGolemSpawnPosition(tx, pos, rangeY)
		if !ok {
			continue
		}
		at := mgl64.Vec3{float64(pos[0]) + .5, float64(pos[1]), float64(pos[2]) + .5}
		if !golemSpawnUnobstructed(tx, pos, at) {
			continue
		}
		g, err := javamob.Spawn(tx, IronGolemID, at, javamob.SpawnMobSummoned, javamob.SpawnOptions{})
		if err != nil {
			continue
		}
		g.PlaySound("ambient")
		return g, true
	}
	return nil, false
}

// moveToGolemSpawnPosition is SpawnUtil.moveToPossibleSpawnPosition with the
// LEGACY_IRON_GOLEM strategy: walk down from pos until a block the golem
// may stand on with air or liquid above.
func moveToGolemSpawnPosition(tx *world.Tx, pos cube.Pos, rangeY int) (cube.Pos, bool) {
	above := tx.Block(pos)
	for y := rangeY; y >= -rangeY; y-- {
		pos = pos.Side(cube.FaceDown)
		cur := tx.Block(pos)
		if legacyGolemCanSpawnOn(tx, pos, cur, above) {
			return pos.Side(cube.FaceUp), true
		}
		above = cur
	}
	return pos, false
}

// golemSpawnUnobstructed is the rest of SpawnUtil.trySpawnMob for a golem
// at pos: checkSpawnRules (a golem has none) and
// IronGolem.checkSpawnObstruction.
func golemSpawnUnobstructed(tx *world.Tx, pos cube.Pos, at mgl64.Vec3) bool {
	if !entityCanStandOn(tx, pos.Side(cube.FaceDown)) {
		return false // Paper iron-golems-can-spawn-in-air: false
	}
	for i := 1; i < 3; i++ {
		if !validEmptySpawnBlock(tx, pos.Add(cube.Pos{0, i, 0}), true) {
			return false
		}
	}
	if !validEmptySpawnBlock(tx, pos, false) {
		return false
	}
	return isUnobstructed(tx, ironGolemBox(at))
}

func ironGolemBox(at mgl64.Vec3) cube.BBox {
	return cube.Box(at[0]-0.7, at[1], at[2]-0.7, at[0]+0.7, at[1]+2.7, at[2]+0.7)
}

var _ = jpoi.Home
