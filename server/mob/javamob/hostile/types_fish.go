//go:build crossfly_full

package hostile

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/mob/javamob/path"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// fishDims are the EntityType sizes of the fish (width, height, eye height).
var fishDims = map[string][3]float64{
	codID:          {0.5, 0.3, 0.195},
	salmonID:       {0.7, 0.4, 0.26},
	pufferfishID:   {0.7, 0.7, 0.455},
	tropicalFishID: {0.5, 0.4, 0.26},
}

func init() {
	for _, id := range []string{codID, salmonID, pufferfishID, tropicalFishID} {
		d := fishDims[id]
		javamob.Register(javamob.Type{
			ID: id, Category: javamob.CategoryWaterAmbient, Width: d[0], Height: d[1], EyeHeight: d[2], Aquatic: true,
			XPReward: 1, // XPReward below rolls WaterAnimal.getBaseExperienceReward
			Families: []string{id[len("minecraft:"):], "fish", "mob"},
			Attributes: func() map[javamob.Attribute]float64 {
				a := javamob.LivingAttributes()
				a[javamob.AttrMaxHealth] = FishMaxHealth
				return a
			},
			New: func() javamob.Behaviour { return &FishMob{} },
		})
	}
}

// FishMob is AbstractFish, AbstractSchoolingFish, Cod, Salmon, Pufferfish
// and TropicalFish on the base. The per-fish state is the Fish of fish.go.
type FishMob struct {
	f *Fish
	// lastDamage is the tick count of the last damage that makes it panic
	// (getLastDamageSource is kept for 40 ticks).
	panicUntil int
}

// RegisterGoals is AbstractFish.registerGoals and its subclasses'.
func (b *FishMob) RegisterGoals(m *javamob.Mob) {
	b.f = fishOf(m.Mob)
	m.SetNavigation(path.NewWaterNavigation(m, false))
	m.SetPathfindingMalus(path.Water, 0) // WaterAnimal
	m.MoveControl().Custom = fishMoveTick
	m.Goals().Add(0, newPanic(1.25, func(m *javamob.Mob) bool { return m.TickCount() <= b.panicUntil }))
	m.Goals().Add(2, goal.NewAvoidEntity(func(e world.Entity) bool {
		p, ok := e.(*player.Player)
		return ok && p.GameMode() != world.GameModeSpectator
	}, 8, 1.6, 1.4))
	swim := goal.NewRandomStroll(1)
	swim.Interval = 40
	inner := swimPosition
	swim.Position = func(m *javamob.Mob) (mgl64.Vec3, bool) {
		if !canRandomSwim(m.Mob) {
			return mgl64.Vec3{}, false
		}
		return inner(m)
	}
	m.Goals().Add(4, swim)
	if b.f.schooling() {
		m.Goals().Add(5, goalOf(newFollowFlockLeaderGoal(m.Rand())))
	}
	if b.f.id == pufferfishID {
		m.Goals().Add(1, goalOf(pufferfishPuffGoal{}))
		SetPuffState(m.Mob, PuffSmall) // the box of a deflated fish (getScale 0.5)
		m.SetDimensions(0.7*pufferScale(PuffSmall), 0.7*pufferScale(PuffSmall))
	}
}

// swimPosition is BehaviorUtils.getRandomSwimmablePos(mob, 10, 7): up to 11
// tries of DefaultRandomPos for a position in water.
func swimPosition(m *javamob.Mob) (mgl64.Vec3, bool) {
	p, ok := m.DefaultRandomPos(10, 7)
	for count := 0; ok && !isWaterAt(m.Tx(), p) && count < 10; count++ {
		p, ok = m.DefaultRandomPos(10, 7)
	}
	return p, ok
}

func isWaterAt(tx *world.Tx, p mgl64.Vec3) bool {
	l, ok := tx.Liquid(blockPos(p))
	return ok && isWaterLiquid(l)
}

// Tick is the start of the fish's tick: Pufferfish.tick's puffing,
// AbstractSchoolingFish.tick, and WaterAnimal.baseTick's air.
func (b *FishMob) Tick(m *javamob.Mob) {
	if b.f.id == pufferfishID {
		PufferfishTick(m.Mob)
	}
	SchoolingFishTick(m.Mob)
	FishBaseTick(m.Mob, func(bm *mob.Mob, dmg float64) { bm.Hurt(dmg, DryOutDamageSource{}) })
}

// AiStep is AbstractFish.aiStep: the flop on land.
func (b *FishMob) AiStep(m *javamob.Mob) { FishAIStep(m.Mob) }

// PostTick is the end of Pufferfish.aiStep: puffed, it stings the mobs
// around it.
func (b *FishMob) PostTick(m *javamob.Mob) {
	if b.f.id != pufferfishID || m.Dead() || b.f.puffState == 0 {
		return
	}
	for _, e := range PufferfishTouchTargets(m.Mob) {
		PufferfishTouch(m.Mob, e, poison, nil)
	}
}

// PlayerTouch is Pufferfish.playerTouch.
func (b *FishMob) PlayerTouch(m *javamob.Mob, p *player.Player) {
	if b.f.id == pufferfishID {
		PufferfishTouch(m.Mob, p, poison, nil)
	}
}

func poison(target world.Entity, n int) {
	if l, ok := target.(interface{ AddEffect(effect.Effect) }); ok {
		l.AddEffect(effect.New(effect.Poison, 1, ticks(n)))
	}
}

// Travel is AbstractFish.travel: travelInWater in water, the base's travel
// otherwise.
func (b *FishMob) Travel(m *javamob.Mob, input mgl64.Vec3) bool {
	if !m.InWater() {
		return false
	}
	FishTravelInWater(m.Mob, input)
	return true
}

// OnHurt keeps the last damage for PanicGoal.shouldPanic (40 ticks).
func (b *FishMob) OnHurt(m *javamob.Mob, _ float64, src world.DamageSource) {
	if panicCause(src) {
		b.panicUntil = m.TickCount() + 40
	}
}

// RemoveWhenFarAway is AbstractFish.removeWhenFarAway.
func (b *FishMob) RemoveWhenFarAway(m *javamob.Mob, _ float64) bool {
	return b.f.RemoveWhenFarAway(m.NameTag() != "")
}

// MaxSpawnClusterSize and IsMaxGroupSizeReached are AbstractFish's and
// AbstractSchoolingFish's.
func (b *FishMob) MaxSpawnClusterSize() int { return b.f.MaxSpawnClusterSize() }
func (b *FishMob) IsMaxGroupSizeReached(n int) bool {
	return b.f.schooling() && n >= b.f.MaxSpawnClusterSize()
}

// schoolGroup is SchoolSpawnGroupData (the leader) and TropicalFishGroupData
// (with the variant).
type schoolGroup struct {
	leader   *world.EntityHandle
	tropical bool
	variant  TropicalVariant
}

// FinalizeSpawnGroup is finalizeSpawn of AbstractSchoolingFish (lead or
// follow), Salmon (a random size) and TropicalFish (9 in 10 schools share a
// common variant; the rest get a random one and do not school).
func (b *FishMob) FinalizeSpawnGroup(m *javamob.Mob, _ javamob.SpawnReason, group any) any {
	g, _ := group.(*schoolGroup)
	if b.f.schooling() {
		if g == nil {
			g = &schoolGroup{leader: m.H()}
		} else if e, ok := g.leader.Entity(m.Tx()); ok {
			if l, ok := mob.Of(e); ok {
				startFollowing(m.Mob, l)
			}
		}
	}
	switch b.f.id {
	case salmonID:
		SetSalmonSize(m.Mob, randomSalmonSize(m.Rand()))
	case tropicalFishID:
		if g != nil && g.tropical {
			SetTropicalVariant(m.Mob, g.variant)
		} else {
			v, school := randomTropicalVariant(m.Rand())
			b.f.isSchool = school
			SetTropicalVariant(m.Mob, v)
			if school {
				g = &schoolGroup{leader: m.H(), tropical: true, variant: v}
			}
		}
	}
	if g == nil {
		return nil
	}
	return g
}

// EncodeNBT and DecodeNBT save the fish (FromBucket, salmon type, puff
// state, tropical variant).
func (b *FishMob) EncodeNBT(data map[string]any)                 { b.f.EncodeNBT(data) }
func (b *FishMob) DecodeNBT(m *javamob.Mob, data map[string]any) { b.f.DecodeNBT(m.Mob, data) }

// panicCause is DamageTypeTags.PANIC_CAUSES: damage from an entity
// (mob and player attacks, projectiles) and the panic_environmental_causes
// (fire, lava, cactus, freezing, lightning, magma).
func panicCause(src world.DamageSource) bool {
	if javamob.SourceEntity(src) != nil {
		return true
	}
	if src.Fire() {
		return true
	}
	switch src.(type) {
	case block.DamageSource, block.MagmaDamageSource, block.LavaDamageSource, entity.LightningDamageSource:
		return true // cactus, sweet berry bush, magma, lava, lightning
	}
	return false
}
