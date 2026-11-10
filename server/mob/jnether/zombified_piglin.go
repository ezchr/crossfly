//go:build crossfly_full

package jnether

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/mob/javamob/monster"
	"github.com/df-mc/dragonfly/server/mob/javamob/path"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// The zombified piglin (ZombifiedPiglin, a Zombie and a NeutralMob, Paper
// 26.3): neutral until hurt; then angry for 20-39 seconds at whoever hurt it
// (and forgiving players who die), with the zombified piglins around that
// have no target joining in: those within its follow range (35) and 10
// blocks up or down at once (HurtByTargetGoal.setAlertOthers), and again
// every 4-6 seconds while it sees its target (maybeAlertOthers). Angry
// adults get +0.05 speed. They never turn into drowned, do not call
// reinforcements, avoid lava (malus 8) and hold a golden sword (1 in 20 a
// golden spear). Gold farms rely on the alerting, the turtle eggs zombies
// go for and the portal spawning (portal_spawn.go).

// ZombifiedPiglinID is the Bedrock identifier the type is registered under
// (Java: minecraft:zombified_piglin).
const ZombifiedPiglinID = "minecraft:zombie_pigman"

func init() {
	javamob.Register(javamob.Type{
		ID: ZombifiedPiglinID, Category: javamob.CategoryMonster,
		Width: 0.6, Height: 1.95, EyeHeight: 1.74,
		FireImmune: true, NotInPeaceful: true, Undead: true,
		LootTable: "entities/zombified_piglin",
		Attributes: func() map[javamob.Attribute]float64 {
			a := monster.ZombieAttributes()
			a[javamob.AttrSpawnReinforcements] = 0
			a[javamob.AttrMovementSpeed] = float64(float32(0.23))
			a[javamob.AttrAttackDamage] = 5
			return a
		},
		Families: []string{"zombie_pigman", "undead", "monster", "mob"},
		XPReward: 5,
		New:      func() javamob.Behaviour { return NewZombifiedPiglin() },
	})
}

// ZombifiedPiglin is the ZombifiedPiglin class.
type ZombifiedPiglin struct {
	*monster.Zombie
	javamob.NeutralMob

	playFirstAngerSoundIn int
	ticksUntilNextAlert   int
	hadTarget             bool
}

// NewZombifiedPiglin returns a zombified piglin's behaviour.
func NewZombifiedPiglin() *ZombifiedPiglin {
	z := &ZombifiedPiglin{Zombie: monster.NewZombie()}
	// convertsInWater false.
	z.Zombie.ConvertsTo = ""
	// PERSISTENT_ANGER_TIME = rangeOfSeconds(20, 39).
	z.NeutralMob.AngerTime = [2]int{20 * 20, 39 * 20}
	z.Zombie.BehaviourGoals = func(_ *monster.Zombie, m *javamob.Mob) { z.addBehaviourGoals(m) }
	z.Zombie.ExtraEquipment = func(_ *monster.Zombie, m *javamob.Mob, _ javamob.DifficultyInstance) {
		// populateDefaultEquipmentSlots: a golden sword, 1 in 20 a golden
		// spear.
		weapon := item.NewStack(item.Sword{Tier: item.ToolTierGold}, 1)
		if m.Rand().IntN(20) == 0 {
			if s, ok := goldenSpear(); ok {
				weapon = s
			}
		}
		m.SetItemSlot(javamob.SlotMainHand, weapon)
	}
	return z
}

// RegisterGoals is Zombie.registerGoals with the lava malus of the
// constructor.
func (z *ZombifiedPiglin) RegisterGoals(m *javamob.Mob) {
	m.SetPathfindingMalus(path.Lava, 8)
	z.Zombie.RegisterGoals(m)
}

// addBehaviourGoals is ZombifiedPiglin.addBehaviourGoals.
func (z *ZombifiedPiglin) addBehaviourGoals(m *javamob.Mob) {
	m.Goals().Add(2, monster.NewZombieAttack(1, false))
	m.Goals().Add(7, goal.NewWaterAvoidingRandomStroll(1, 0.001))
	m.Targets().Add(1, goal.NewHurtByTarget().SetAlertOthers())
	angry := goal.NewNearestAttackableTarget(nil, true, false)
	angry.Selector = func(e world.Entity, tx *world.Tx) bool {
		jm, ok := javamob.OfHandle(tx, m.H())
		return ok && z.IsAngryAt(jm, e)
	}
	m.Targets().Add(2, angry)
	// ResetUniversalAngerTargetGoal(3) only acts with the universalAnger
	// game rule, which is off.
}

// FinalizeSpawn is Zombie.finalizeSpawn with ZombifiedPiglin's
// randomizeReinforcementsChance: never reinforcements.
func (z *ZombifiedPiglin) FinalizeSpawn(m *javamob.Mob, reason javamob.SpawnReason) {
	z.Zombie.FinalizeSpawn(m, reason)
	m.Attributes().Instance(javamob.AttrSpawnReinforcements).SetBase(0)
	m.SyncAttributes()
}

// goldenSpear is the golden spear item, if this version has it.
func goldenSpear() (item.Stack, bool) {
	if it, ok := world.ItemByName("minecraft:golden_spear", 0); ok {
		return item.NewStack(it, 1), true
	}
	return item.Stack{}, false
}

// CustomServerAiStep is ZombifiedPiglin.customServerAiStep: the attacking
// speed bonus, the first anger sound, persistent anger and alerting.
func (z *ZombifiedPiglin) CustomServerAiStep(m *javamob.Mob) {
	z.noticeNewTarget(m)
	speed := m.Attributes().Instance(javamob.AttrMovementSpeed)
	if z.IsAngry(m) {
		if !m.Baby() && !speed.HasModifier("minecraft:attacking") {
			speed.AddModifier(javamob.Modifier{ID: "minecraft:attacking", Amount: 0.05, Op: javamob.OpAddValue, Transient: true})
			m.SyncAttributes()
		}
		z.maybePlayFirstAngerSound(m)
	} else if speed.HasModifier("minecraft:attacking") {
		speed.RemoveModifier("minecraft:attacking")
		m.SyncAttributes()
	}
	z.forgiveDeadPlayer(m)
	z.UpdatePersistentAnger(m, true)
	if _, ok := m.Target(); ok {
		z.maybeAlertOthers(m)
	}
	z.setAngryMeta(m)
}

// forgiveDeadPlayer is NeutralMob.playerDied (forgiveDeadPlayers, on by
// default), which Java runs from ServerPlayer.die for the neutral mobs within
// 32 x 10 x 32: the dead player the mob is angry at is forgiven.
func (z *ZombifiedPiglin) forgiveDeadPlayer(m *javamob.Mob) {
	e, ok := z.AngerTarget(m)
	if !ok {
		return
	}
	p, isPlayer := e.(*player.Player)
	if !isPlayer || !p.Dead() {
		return
	}
	d := p.Position().Sub(m.Position())
	if d[0] >= -32 && d[0] <= 32 && d[2] >= -32 && d[2] <= 32 && d[1] >= -10 && d[1] <= 10 {
		z.PlayerDied(m, p)
	}
}

// noticeNewTarget is ZombifiedPiglin.setTarget: a first target rolls the
// first anger sound (0-1 s) and the first alert (4-6 s).
func (z *ZombifiedPiglin) noticeNewTarget(m *javamob.Mob) {
	_, has := m.Target()
	if has && !z.hadTarget {
		r := m.Rand()
		z.playFirstAngerSoundIn = r.IntN(21)
		z.ticksUntilNextAlert = 80 + r.IntN(41)
	}
	z.hadTarget = has
}

func (z *ZombifiedPiglin) maybePlayFirstAngerSound(m *javamob.Mob) {
	if z.playFirstAngerSoundIn > 0 {
		z.playFirstAngerSoundIn--
		if z.playFirstAngerSoundIn == 0 {
			m.PlaySound("mad")
		}
	}
}

// maybeAlertOthers: every 4-6 seconds while the target is in sight.
func (z *ZombifiedPiglin) maybeAlertOthers(m *javamob.Mob) {
	if z.ticksUntilNextAlert > 0 {
		z.ticksUntilNextAlert--
		return
	}
	if t, ok := m.Target(); ok && m.Sensing().HasLineOfSight(m, t) {
		AlertZombifiedPiglins(m, t)
	}
	z.ticksUntilNextAlert = 80 + m.Rand().IntN(41)
}

// AlertZombifiedPiglins is ZombifiedPiglin.alertOthers: every zombified
// piglin in the unit cube at m's feet grown by its follow range sideways and
// 10 up and down, without a target, takes m's target.
func AlertZombifiedPiglins(m *javamob.Mob, target world.Entity) {
	d := m.FollowRange()
	p := m.Position()
	box := cube.Box(p[0]-d, p[1]-10, p[2]-d, p[0]+1+d, p[1]+11, p[2]+1+d)
	tx := m.Tx()
	var others []*javamob.Mob
	for e := range tx.EntitiesWithin(box.Grow(1)) {
		if e.H() == m.H() || e.H().Type().EncodeEntity() != ZombifiedPiglinID {
			continue
		}
		if !e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) {
			continue
		}
		o, ok := javamob.Of(e)
		if !ok || o.Dead() {
			continue
		}
		if _, has := o.Target(); has {
			continue
		}
		others = append(others, o)
	}
	for _, o := range others {
		o.SetTarget(target)
	}
}

// EncodeNBT saves the zombie's and the anger's data.
func (z *ZombifiedPiglin) EncodeNBT(data map[string]any) {
	z.Zombie.EncodeNBT(data)
	z.EncodeAnger(data)
}

// DecodeNBT loads them.
func (z *ZombifiedPiglin) DecodeNBT(m *javamob.Mob, data map[string]any) {
	z.Zombie.DecodeNBT(m, data)
	z.DecodeAnger(m, data)
}

// setAngryMeta shows isAngry to the clients (Bedrock's angry flag, Java's
// aggressive arms).
func (z *ZombifiedPiglin) setAngryMeta(m *javamob.Mob) {
	if a := z.IsAngry(m); m.Meta().Angry != a {
		m.UpdateMeta(func(meta *mob.Meta) { meta.Angry = a })
	}
}
