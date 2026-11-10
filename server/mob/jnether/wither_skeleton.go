//go:build crossfly_full

package jnether

import (
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/mob/javamob/monster"
	"github.com/df-mc/dragonfly/server/mob/javamob/path"
	"github.com/df-mc/dragonfly/server/world"
)

// The wither skeleton (WitherSkeleton, an AbstractSkeleton, Paper 26.3):
// a stone sword and no armour (populateDefaultEquipmentSlots), 4 attack
// damage after finalizeSpawn, Wither for 10 s on every hit, piglins as
// extra targets, lava avoided (malus 8). Its skull (2.5% + 1% per Looting
// level, player kills only) and coal and bones come from its loot table.
// It never holds a bow (wither_skeleton_disliked_weapons), so its burning
// arrows (getArrow) never fly.

// WitherSkeletonID is the wither skeleton's identifier.
const WitherSkeletonID = "minecraft:wither_skeleton"

func init() {
	javamob.Register(javamob.Type{
		ID: WitherSkeletonID, Category: javamob.CategoryMonster,
		Width: 0.7, Height: 2.4, EyeHeight: 2.1,
		FireImmune: true, NotInPeaceful: true, Undead: true,
		Attributes: func() map[javamob.Attribute]float64 {
			a := javamob.MonsterAttributes()
			a[javamob.AttrMovementSpeed] = 0.25
			return a
		},
		Families: []string{"wither_skeleton", "skeleton", "undead", "monster", "mob"},
		XPReward: 5,
		New:      func() javamob.Behaviour { return &WitherSkeleton{AbstractSkeleton: monster.NewAbstractSkeleton()} },
	})
}

// WitherSkeleton is the WitherSkeleton class.
type WitherSkeleton struct{ *monster.AbstractSkeleton }

// RegisterGoals is WitherSkeleton.registerGoals: piglins as targets, then
// the skeleton's goals.
func (w *WitherSkeleton) RegisterGoals(m *javamob.Mob) {
	m.SetPathfindingMalus(path.Lava, 8)
	m.Targets().Add(3, goal.NewNearestAttackableTarget(goal.IsType("minecraft:piglin", "minecraft:piglin_brute"), true, false))
	w.AbstractSkeleton.RegisterGoals(m)
}

// FinalizeSpawn is AbstractSkeleton.finalizeSpawn with WitherSkeleton's
// populateDefaultEquipmentSlots (a stone sword, no armour) and no
// equipment enchantments, then attack damage 4 and the weapon goal.
func (w *WitherSkeleton) FinalizeSpawn(m *javamob.Mob, _ javamob.SpawnReason) {
	d := m.Difficulty()
	m.SetItemSlot(javamob.SlotMainHand, item.NewStack(item.Sword{Tier: item.ToolTierStone}, 1))
	m.SetCanPickUpLoot(m.Rand().Float32() < 0.55*float32(d.Special))
	m.Attributes().Instance(javamob.AttrAttackDamage).SetBase(4)
	m.SyncAttributes()
	w.ReassessWeaponGoal(m)
}

// FilterDamage: fire immune.
func (w *WitherSkeleton) FilterDamage(_ *javamob.Mob, d *mob.Damage) { fireImmune(d) }

// DoHurtTarget is WitherSkeleton.doHurtTarget: Wither I for 10 s.
func (w *WitherSkeleton) DoHurtTarget(m *javamob.Mob, target world.Entity) bool {
	if !m.BaseDoHurtTarget(target) {
		return false
	}
	if l, ok := target.(entity.Living); ok {
		l.AddEffect(effect.New(effect.Wither, 1, 10*time.Second))
	}
	return true
}
