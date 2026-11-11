//go:build crossfly_full

package jnether

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/jbrain"
	"github.com/df-mc/dragonfly/server/mob/jbrain/jmob"
	"github.com/df-mc/dragonfly/server/world"
)

// The generic fighting behaviours of brain-driven mobs (package
// net.minecraft.world.entity.ai.behavior) the zoglin, hoglin and piglins
// share. Exported so other brain mobs (wardens, breezes, creakings) can use
// them too.

type oneShot = jbrain.OneShot[*javamob.Mob]

// brainCanAttack is Mob.canAttack(LivingEntity) for brain mobs: a living,
// alive target that can be seen as an enemy, allowed by the behaviour's
// TargetFilter.
func brainCanAttack(m *javamob.Mob, e world.Entity) bool {
	if !canAttackDefault(e) || !alive(e) || e.H() == m.H() {
		return false
	}
	if f, ok := m.Behaviour().(javamob.TargetFilter); ok && !f.CanAttack(m, e) {
		return false
	}
	return true
}

// AttackTarget returns the ATTACK_TARGET memory as an entity
// (Mob.getTargetFromBrain).
func AttackTarget(c *jbrain.Ctx, mem *jbrain.Memories) (world.Entity, bool) {
	if mem == nil || c.Tx == nil {
		return nil, false
	}
	h, ok := jbrain.Get(mem, jbrain.MemAttackTarget)
	if !ok || h == nil {
		return nil, false
	}
	return h.Entity(c.Tx)
}

// StartAttacking is StartAttacking.create(canAttackPredicate, targetFinder).
// cond may be nil (always).
func StartAttacking(cond func(c *jbrain.Ctx, m *javamob.Mob) bool, find func(c *jbrain.Ctx, m *javamob.Mob) (world.Entity, bool)) *oneShot {
	return jmob.Trigger("StartAttacking", []jbrain.Req{jbrain.Absent(jbrain.MemAttackTarget), jbrain.Reg(jbrain.MemCantReachWalkTargetSince)}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		if cond != nil && !cond(c, m) {
			return false
		}
		t, ok := find(c, m)
		if !ok || !brainCanAttack(m, t) {
			return false
		}
		jbrain.Set(c.Mem, jbrain.MemAttackTarget, t.H())
		c.Mem.Erase(jbrain.MemCantReachWalkTargetSince.ID())
		return true
	})
}

// StopAttackingIfTargetInvalid is StopAttackingIfTargetInvalid.create(
// stopAttackingWhen, onTargetErased, canGrowTiredOfTryingToReachTarget).
// stopWhen and onErased may be nil.
func StopAttackingIfTargetInvalid(stopWhen func(c *jbrain.Ctx, target world.Entity) bool, onErased func(c *jbrain.Ctx, m *javamob.Mob, target world.Entity), canTire bool) *oneShot {
	return jmob.Trigger("StopAttackingIfTargetInvalid", []jbrain.Req{jbrain.Present(jbrain.MemAttackTarget), jbrain.Reg(jbrain.MemCantReachWalkTargetSince)}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		// A removed target or one in another world is not found in the
		// transaction (target.level() == body.level()).
		t, ok := AttackTarget(c, c.Mem)
		if ok && brainCanAttack(m, t) &&
			(!canTire || !tiredOfTryingToReach(c)) &&
			(stopWhen == nil || !stopWhen(c, t)) {
			return true
		}
		if onErased != nil && ok {
			onErased(c, m, t)
		}
		c.Mem.Erase(jbrain.MemAttackTarget.ID())
		return true
	})
}

// tiredOfTryingToReach is isTiredOfTryingToReachTarget: 200 ticks.
func tiredOfTryingToReach(c *jbrain.Ctx) bool {
	since, ok := jbrain.Get(c.Mem, jbrain.MemCantReachWalkTargetSince)
	return ok && c.Time-since > 200
}

// MeleeAttack is MeleeAttack.create(canAttackPredicate, cooldown). pred may
// be nil.
func MeleeAttack(pred func(m *javamob.Mob) bool, cooldown int) *oneShot {
	return jmob.Trigger("MeleeAttack", []jbrain.Req{
		jbrain.Reg(jbrain.MemLookTarget), jbrain.Present(jbrain.MemAttackTarget),
		jbrain.Absent(jbrain.MemAttackCoolingDown), jbrain.Present(jbrain.MemNearestVisibleLivingEntities),
	}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		t, ok := AttackTarget(c, c.Mem)
		if !ok || (pred != nil && !pred(m)) || holdingUsableNonMeleeWeapon(m) || !m.IsWithinMeleeAttackRange(t) {
			return false
		}
		if v, ok := jbrain.Get(c.Mem, jbrain.MemNearestVisibleLivingEntities); !ok || !v.Contains(c.Tx, t.H()) {
			return false
		}
		jbrain.Set[jbrain.PositionTracker](c.Mem, jbrain.MemLookTarget, jbrain.TrackEntity(t.H(), true))
		m.Swing()
		m.DoHurtTarget(t)
		jbrain.SetWithExpiry(c.Mem, jbrain.MemAttackCoolingDown, true, int64(cooldown))
		return true
	})
}

// NonMeleeWeaponUser is a behaviour whose mob can shoot what it holds
// (Mob.canUseNonMeleeWeapon: piglins with crossbows).
type NonMeleeWeaponUser interface{ CanUseNonMeleeWeapon() bool }

// holdingUsableNonMeleeWeapon is MeleeAttack.isHoldingUsableNonMeleeWeapon.
func holdingUsableNonMeleeWeapon(m *javamob.Mob) bool {
	u, ok := m.Behaviour().(NonMeleeWeaponUser)
	if !ok || !u.CanUseNonMeleeWeapon() {
		return false
	}
	main, off := m.HeldItems()
	for _, s := range []item.Stack{main, off} {
		switch s.Item().(type) {
		case item.Bow, item.Crossbow:
			return true
		}
	}
	return false
}

// SetWalkTargetFromAttackTargetIfTargetOutOfReach is
// SetWalkTargetFromAttackTargetIfTargetOutOfReach.create(speed).
func SetWalkTargetFromAttackTargetIfTargetOutOfReach(speed func(m *javamob.Mob) float32) *oneShot {
	return jmob.Trigger("SetWalkTargetFromAttackTargetIfTargetOutOfReach", []jbrain.Req{
		jbrain.Reg(jbrain.MemWalkTarget), jbrain.Reg(jbrain.MemLookTarget),
		jbrain.Present(jbrain.MemAttackTarget), jbrain.Reg(jbrain.MemNearestVisibleLivingEntities),
	}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		t, ok := AttackTarget(c, c.Mem)
		if !ok {
			return false
		}
		if v, ok := jbrain.Get(c.Mem, jbrain.MemNearestVisibleLivingEntities); ok && v.Contains(c.Tx, t.H()) && withinAttackRange(m, t) {
			c.Mem.Erase(jbrain.MemWalkTarget.ID())
			return true
		}
		jbrain.Set[jbrain.PositionTracker](c.Mem, jbrain.MemLookTarget, jbrain.TrackEntity(t.H(), true))
		jbrain.Set(c.Mem, jbrain.MemWalkTarget, jbrain.WalkTarget{Target: jbrain.TrackEntity(t.H(), false), Speed: speed(m), CloseEnough: 0})
		return true
	})
}

// withinAttackRange is BehaviorUtils.isWithinAttackRange(body, target, 1)
// for mobs without a usable projectile weapon: the melee range. Crossbow
// piglins get the projectile range check when they are added.
func withinAttackRange(m *javamob.Mob, t world.Entity) bool {
	return m.IsWithinMeleeAttackRange(t)
}

// SetEntityLookTargetSometimes is SetEntityLookTargetSometimes.create(maxDist,
// UniformInt.of(lo, hi)). match may be nil (any entity).
func SetEntityLookTargetSometimes(maxDist float32, lo, hi int, match func(e world.Entity) bool) *oneShot {
	maxSq := float64(maxDist * maxDist)
	ticks := 0
	return jmob.Trigger("SetEntityLookTargetSometimes", []jbrain.Req{jbrain.Absent(jbrain.MemLookTarget), jbrain.Present(jbrain.MemNearestVisibleLivingEntities)}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		v, _ := jbrain.Get(c.Mem, jbrain.MemNearestVisibleLivingEntities)
		pos := m.Position()
		h, ok := v.FindClosest(c.Tx, func(h *world.EntityHandle) bool {
			e, ok := h.Entity(c.Tx)
			return ok && (match == nil || match(e)) && e.Position().Sub(pos).LenSqr() <= maxSq
		})
		if !ok {
			return false
		}
		// Ticker.tickDownAndCheck.
		if ticks == 0 {
			ticks = lo + c.Rand.IntN(hi-lo+1) - 1
			return false
		}
		ticks--
		if ticks != 0 {
			return false
		}
		jbrain.Set[jbrain.PositionTracker](c.Mem, jbrain.MemLookTarget, jbrain.TrackEntity(h, true))
		return true
	})
}

// RandomStroll is RandomStroll.stroll(speed): a land position within 10
// blocks (7 up or down) as the walk target, or none.
func RandomStroll(speed float32) *oneShot {
	return jmob.Trigger("RandomStroll", []jbrain.Req{jbrain.Absent(jbrain.MemWalkTarget)}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		p, ok := m.LandRandomPos(10, 7)
		if !ok {
			c.Mem.Erase(jbrain.MemWalkTarget.ID())
			return true
		}
		jbrain.Set(c.Mem, jbrain.MemWalkTarget, jbrain.WalkTarget{Target: jbrain.TrackVec(p), Speed: speed, CloseEnough: 0})
		return true
	})
}

// otherTargetMuchFurther is
// BehaviorUtils.isOtherTargetMuchFurtherAwayThanCurrentAttackTarget.
func otherTargetMuchFurther(c *jbrain.Ctx, m *javamob.Mob, other world.Entity, by float64) bool {
	cur, ok := AttackTarget(c, c.Mem)
	if !ok {
		return false
	}
	pos := m.Position()
	return other.Position().Sub(pos).LenSqr() > cur.Position().Sub(pos).LenSqr()+by*by
}
