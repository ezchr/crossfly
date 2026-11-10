//go:build crossfly_full

package hostile

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/mob/javamob/monster"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// The land mobs of this row that are built on the base's zombie, skeleton
// and spider, plus the giant and the silverfish.
func init() {
	javamob.Register(javamob.Type{
		ID: huskID, Category: javamob.CategoryMonster, Width: 0.6, Height: 1.95, EyeHeight: 1.74,
		NotInPeaceful: true, Undead: true, XPReward: 5,
		Families:   []string{"husk", "zombie", "undead", "monster", "mob"},
		Attributes: monster.ZombieAttributes,
		New:        func() javamob.Behaviour { return newHusk() },
	})
	javamob.Register(javamob.Type{
		ID: strayID, Category: javamob.CategoryMonster, Width: 0.6, Height: 1.99, EyeHeight: 1.74,
		NotInPeaceful: true, BurnsInDaylight: true, Undead: true, XPReward: 5,
		Families:   []string{"stray", "skeleton", "undead", "monster", "mob"},
		Attributes: monster.SkeletonAttributes,
		New: func() javamob.Behaviour {
			s := monster.NewAbstractSkeleton()
			s.Arrow = effectArrow(StrayArrowEffect)
			return s
		},
	})
	javamob.Register(javamob.Type{
		ID: boggedID, Category: javamob.CategoryMonster, Width: 0.6, Height: 1.99, EyeHeight: 1.74,
		NotInPeaceful: true, BurnsInDaylight: true, Undead: true, XPReward: 5,
		Families:   []string{"bogged", "skeleton", "undead", "monster", "mob"},
		Attributes: slowSkeletonAttributes,
		New:        func() javamob.Behaviour { return &Bogged{AbstractSkeleton: slowSkeleton(BoggedArrowEffect)} },
	})
	javamob.Register(javamob.Type{
		ID: parchedID, Category: javamob.CategoryMonster, Width: 0.6, Height: 1.99, EyeHeight: 1.74,
		NotInPeaceful: true, Undead: true, XPReward: 5,
		Families:   []string{"parched", "skeleton", "undead", "monster", "mob"},
		Attributes: slowSkeletonAttributes,
		New:        func() javamob.Behaviour { return slowSkeleton(ParchedArrowEffect) },
	})
	javamob.Register(javamob.Type{
		ID: caveSpiderID, Category: javamob.CategoryMonster, Width: 0.7, Height: 0.5, EyeHeight: 0.45,
		NotInPeaceful: true, Arthropod: true, XPReward: 5,
		Families: []string{"cave_spider", "arthropod", "monster", "mob"},
		Attributes: func() map[javamob.Attribute]float64 {
			a := javamob.MonsterAttributes()
			a[javamob.AttrMaxHealth] = CaveSpiderHealth
			a[javamob.AttrMovementSpeed] = float64(float32(0.3))
			return a
		},
		New: func() javamob.Behaviour { return &CaveSpider{} },
	})
	javamob.Register(javamob.Type{
		ID: giantID, Category: javamob.CategoryMonster, Width: 3.6, Height: 12, EyeHeight: 10.44,
		NotInPeaceful: true, XPReward: 5,
		Families: []string{"giant", "zombie", "monster", "mob"},
		Attributes: func() map[javamob.Attribute]float64 {
			a := javamob.MonsterAttributes()
			a[javamob.AttrMaxHealth], a[javamob.AttrMovementSpeed], a[javamob.AttrAttackDamage] = GiantHealth, GiantSpeed, GiantAttack
			return a
		},
		New: func() javamob.Behaviour { return &Giant{} },
	})
	javamob.Register(javamob.Type{
		ID: silverfishID, Category: javamob.CategoryMonster, Width: 0.4, Height: 0.3, EyeHeight: 0.13,
		NotInPeaceful: true, Arthropod: true, XPReward: 5,
		Families: []string{"silverfish", "arthropod", "monster", "mob"},
		Attributes: func() map[javamob.Attribute]float64 {
			a := javamob.MonsterAttributes()
			h, s, d := SilverfishAttributes()
			a[javamob.AttrMaxHealth], a[javamob.AttrMovementSpeed], a[javamob.AttrAttackDamage] = h, s, d
			return a
		},
		New: func() javamob.Behaviour { return &SilverfishMob{} },
	})
}

// Husk is Husk: a zombie that does not burn, starves what it hits bare-handed
// and turns into a zombie when it drowns. (Its camel husk jockeys come with
// the camel husk, which needs the pets row's camel.)
type Husk struct{ *monster.Zombie }

func newHusk() *Husk {
	z := monster.NewZombie()
	z.ConvertsTo = HuskConvertsTo
	return &Husk{Zombie: z}
}

// SunSensitive is Husk.isSunSensitive.
func (*Husk) SunSensitive(*javamob.Mob) bool { return false }

// DoHurtTarget is Husk.doHurtTarget: the zombie's attack, then Hunger.
func (h *Husk) DoHurtTarget(m *javamob.Mob, target world.Entity) bool {
	if !h.Zombie.DoHurtTarget(m, target) {
		return false
	}
	HuskHit(m.Mob, target)
	return true
}

// SetBaby is the zombie's setBaby with the husk's own baby size.
func (h *Husk) SetBaby(m *javamob.Mob, baby bool) {
	h.Zombie.SetBaby(m, baby)
	if baby {
		m.SetDimensions(HuskBabyWidth, HuskBabyHeight)
		m.SetEyeHeight(HuskBabyEyeHeight)
	}
}

// slowSkeletonAttributes are Bogged/Parched.createAttributes: 16 health.
func slowSkeletonAttributes() map[javamob.Attribute]float64 {
	a := monster.SkeletonAttributes()
	a[javamob.AttrMaxHealth] = BoggedParchedHealth
	return a
}

// slowSkeleton is the AbstractSkeleton of a bogged or parched: 70 (hard 50)
// ticks between shots, arrows with an effect.
func slowSkeleton(eff func() effect.Effect) *monster.AbstractSkeleton {
	s := monster.NewAbstractSkeleton()
	s.AttackInterval, s.HardAttackInterval = SlowSkeletonAttackInterval, SlowSkeletonHardAttackInterval
	s.Arrow = effectArrow(eff)
	return s
}

// effectArrow is getArrow of the stray, bogged and parched: an arrow with
// an extra effect (Arrow.addEffect; the pickup item is a plain arrow, so the
// effect keeps its full length) given to what it hits.
func effectArrow(eff func() effect.Effect) func(m *javamob.Mob, opts world.EntitySpawnOpts, damage float64) *world.EntityHandle {
	return func(m *javamob.Mob, opts world.EntitySpawnOpts, damage float64) *world.EntityHandle {
		conf := entity.ProjectileBehaviourConfig{
			Owner: m.H(), Gravity: 0.05, Drag: 0.01, Damage: damage,
			Sound: sound.ArrowHit{}, SurviveBlockCollision: true,
			Hit: func(_ *entity.Ent, _ *world.Tx, r trace.Result) {
				er, ok := r.(trace.EntityResult)
				if !ok {
					return
				}
				// Arrow.doPostHurtEffects: only living things that are still alive.
				if l, ok := er.Entity().(interface {
					AddEffect(effect.Effect)
					Dead() bool
				}); ok && !l.Dead() {
					l.AddEffect(eff())
				}
			},
		}
		return opts.New(entity.ArrowType, conf)
	}
}

// Bogged is Bogged: a slow skeleton with poison arrows that can be sheared
// for its mushrooms.
type Bogged struct {
	*monster.AbstractSkeleton
	sheared bool
}

// MobInteract is Bogged.mobInteract: shears on an unsheared bogged drop two
// mushrooms (loot table shearing/bogged: two rolls of brown or red) and wear
// the shears by one.
func (b *Bogged) MobInteract(m *javamob.Mob, user item.User, held item.Stack) (item.Stack, bool) {
	if !BoggedShear(m.Mob, held, func(*mob.Mob) {
		for i := 0; i < 2; i++ {
			// Crossfly has no brown or red mushroom items yet: nothing drops
			// until it does.
			if it, ok := randomMushroom(m); ok {
				m.SpawnAtLocation(item.NewStack(it, 1))
			}
		}
	}) {
		return held, false
	}
	b.sheared = true
	return held.Damage(1), true
}

// randomMushroom is one roll of shearing/bogged.
func randomMushroom(m *javamob.Mob) (world.Item, bool) {
	name := "minecraft:red_mushroom"
	if m.Rand().IntN(2) == 0 {
		name = "minecraft:brown_mushroom"
	}
	it, ok := world.ItemByName(name, 0)
	return it, ok && it != nil
}

// EncodeNBT is Bogged.addAdditionalSaveData (sheared) after the skeleton's.
func (b *Bogged) EncodeNBT(data map[string]any) {
	b.AbstractSkeleton.EncodeNBT(data)
	data["sheared"] = boolByte(b.sheared)
}

// DecodeNBT is Bogged.readAdditionalSaveData.
func (b *Bogged) DecodeNBT(m *javamob.Mob, data map[string]any) {
	b.AbstractSkeleton.DecodeNBT(m, data)
	b.sheared = nbtBool(data, "sheared")
	m.UpdateMeta(func(meta *mob.Meta) { meta.Sheared = b.sheared })
}

// CaveSpider is CaveSpider: a small spider that poisons and never spawns
// with effects.
type CaveSpider struct{ monster.Spider }

// DoHurtTarget is CaveSpider.doHurtTarget.
func (c *CaveSpider) DoHurtTarget(m *javamob.Mob, target world.Entity) bool {
	if !m.BaseDoHurtTarget(target) {
		return false
	}
	CaveSpiderHit(m.Mob, target)
	return true
}

// FinalizeSpawn is CaveSpider.finalizeSpawn: nothing (no spider effects).
func (c *CaveSpider) FinalizeSpawn(*javamob.Mob, javamob.SpawnReason) {}

// Giant is Giant: a monster with no goals that only likes dark places.
type Giant struct{}

// RegisterGoals is Monster.registerGoals: none.
func (*Giant) RegisterGoals(*javamob.Mob) {}

// WalkTargetValue is Giant.getWalkTargetValue: the light-level path cost.
func (*Giant) WalkTargetValue(m *javamob.Mob, pos cube.Pos) float64 {
	return javamob.MonsterWalkTargetValue(m, pos)
}

// SilverfishMob is Silverfish.
type SilverfishMob struct{}

// RegisterGoals is Silverfish.registerGoals.
func (s *SilverfishMob) RegisterGoals(m *javamob.Mob) {
	m.Goals().Add(1, goal.NewFloat(m))
	m.Goals().Add(1, &climbOnPowderSnowGoal{javamob.GoalBase{Control: javamob.FlagJump}})
	m.Goals().Add(3, &silverfishWakeGoal{w: &silverfishOf(m.Mob).wake})
	m.Goals().Add(4, goal.NewMeleeAttack(1, false))
	stroll := goal.NewRandomStroll(1)
	stroll.Interval = 10
	m.Goals().Add(5, &silverfishMergeGoalJ{stroll: stroll})
	m.Targets().Add(1, goal.NewHurtByTarget().SetAlertOthers())
	m.Targets().Add(2, goal.NewNearestAttackableTarget(goal.IsPlayer, true, false))
}

// FilterDamage is the start of Silverfish.hurtServer: hurt by an entity (or
// magic, #always_triggers_silverfish), it calls its friends.
func (s *SilverfishMob) FilterDamage(m *javamob.Mob, d *mob.Damage) {
	SilverfishHurt(m.Mob, d.Attacker != nil || magicDamage(d.Source))
}

// magicDamage reports whether src is of the magic damage types
// (#always_triggers_silverfish: magic, indirect magic): poison, harming and
// a guardian's beam.
func magicDamage(src world.DamageSource) bool {
	switch src.(type) {
	case effect.PoisonDamageSource, effect.InstantDamageSource, indirectMagicSource:
		return true
	}
	return false
}

// Tick is Silverfish.tick: the body follows the facing.
func (s *SilverfishMob) Tick(m *javamob.Mob) { m.SetBodyYaw(m.Yaw()) }

// WalkTargetValue is Silverfish.getWalkTargetValue.
func (s *SilverfishMob) WalkTargetValue(m *javamob.Mob, pos cube.Pos) float64 {
	return SilverfishWalkTargetValue(m.Tx(), pos, javamob.MonsterWalkTargetValue(m, pos))
}

// silverfishWakeGoal adapts SilverfishWakeUpFriendsGoal to the base.
type silverfishWakeGoal struct {
	javamob.GoalBase
	w *silverfishWakeUpFriendsGoal
}

func (g *silverfishWakeGoal) CanUse(*javamob.Mob) bool { return g.w.lookForFriends > 0 }
func (g *silverfishWakeGoal) Tick(m *javamob.Mob)      { g.w.Tick(m.Mob) }

// silverfishMergeGoalJ is SilverfishMergeWithStoneGoal on the base's
// RandomStrollGoal (speed 1, interval 10).
type silverfishMergeGoalJ struct {
	stroll    *goal.RandomStroll
	direction cube.Face
	doMerge   bool
}

func (g *silverfishMergeGoalJ) Flags() javamob.Flags { return javamob.FlagMove }

func (g *silverfishMergeGoalJ) CanUse(m *javamob.Mob) bool {
	if _, ok := m.Target(); ok {
		return false
	}
	if !m.Navigation().Done() {
		return false
	}
	r := m.Rand()
	if MobGriefing(m.Tx()) && nextInt(r, reducedTickDelay(10)) == 0 {
		g.direction = cube.Face(nextInt(r, 6))
		if _, ok := infestedByHost(m.Tx().Block(mergePos(m.Mob, g.direction))); ok {
			g.doMerge = true
			return true
		}
	}
	g.doMerge = false
	return g.stroll.CanUse(m)
}

func (g *silverfishMergeGoalJ) CanContinueToUse(m *javamob.Mob) bool {
	return !g.doMerge && g.stroll.CanContinueToUse(m)
}

func (g *silverfishMergeGoalJ) Start(m *javamob.Mob) {
	if !g.doMerge {
		g.stroll.Start(m)
		return
	}
	tx := m.Tx()
	at := mergePos(m.Mob, g.direction)
	inf, ok := infestedByHost(tx.Block(at))
	if !ok {
		return
	}
	tx.SetBlock(at, inf, nil)
	SpawnAnim(m.Mob)
	_ = m.Close()
}

func (g *silverfishMergeGoalJ) Stop(m *javamob.Mob) { g.stroll.Stop(m) }

// climbOnPowderSnowGoal is ClimbOnTopOfPowderSnowGoal (JUMP): a silverfish
// in powder snow jumps while the block above is powder snow or empty.
type climbOnPowderSnowGoal struct{ javamob.GoalBase }

func (climbOnPowderSnowGoal) RequiresUpdateEveryTick() bool { return true }

func (climbOnPowderSnowGoal) CanUse(m *javamob.Mob) bool {
	tx := m.Tx()
	pos := blockPos(m.Pos())
	if !isPowderSnow(tx, pos) {
		return false
	}
	above := pos.Side(cube.FaceUp)
	return isPowderSnow(tx, above) || len(tx.Block(above).Model().BBox(above, tx)) == 0
}

func (climbOnPowderSnowGoal) Tick(m *javamob.Mob) { m.JumpControl().Jump() }

// isPowderSnow reports whether the block at pos is powder snow.
func isPowderSnow(tx *world.Tx, pos cube.Pos) bool {
	name, _ := tx.Block(pos).EncodeBlock()
	return name == "minecraft:powder_snow"
}
