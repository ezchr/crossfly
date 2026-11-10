//go:build crossfly_full

package hostile

import (
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// The few Mob/LivingEntity methods of Java this package needs, on top of the
// *mob.Mob API. Where the Java mob base has its own version, the hooks below
// are pointed at it (Hooks).

// SoundPlayer plays a Java sound event (name without "minecraft:") at the
// mob, heard by both editions (Entity.playSound).
type SoundPlayer func(m *mob.Mob, name string, volume, pitch float64)

// Hooks are the parts of the Java mob base this package calls. The base sets
// them when the Java mob mode is on; the defaults are good enough for tests.
var Hooks = struct {
	// PlaySound is Entity.playSound.
	PlaySound SoundPlayer
	// LineOfSight is Mob.hasLineOfSight (Sensing).
	LineOfSight func(m *mob.Mob, target world.Entity) bool
	// MobAttack hurts target with a melee hit of the mob for dmg
	// (target.hurtServer(mobAttack(m), dmg)) and reports whether it landed.
	MobAttack func(m *mob.Mob, target world.Entity, dmg float64) bool
	// DoHurtTarget is Mob.doHurtTarget: the mob's melee attack with its
	// attack damage, knockback and weapon effects.
	DoHurtTarget func(m *mob.Mob, target world.Entity) bool
	// Move is Entity.move(MoverType.SELF, delta): move with block
	// collisions and return the velocity left after them.
	Move func(m *mob.Mob, delta mgl64.Vec3) mgl64.Vec3
	// HurtRecently is LivingEntity.wasHurtRecently (hurtTime > 0: hurt in
	// the last 10 ticks, by anything).
	HurtRecently func(m *mob.Mob) bool
}{
	PlaySound: func(m *mob.Mob, name string, volume, pitch float64) { m.PlaySound(bedrockSoundEvent(name)) },
	LineOfSight: func(m *mob.Mob, target world.Entity) bool {
		if jm, ok := javamob.Of(m); ok {
			return jm.HasLineOfSight(target)
		}
		return sense.CanSee(m, target)
	},
	MobAttack: func(m *mob.Mob, target world.Entity, dmg float64) bool {
		l, ok := target.(interface {
			Hurt(float64, world.DamageSource) (float64, world.HurtResult)
		})
		if !ok {
			return false
		}
		n, _ := l.Hurt(dmg, entity.AttackDamageSource{Attacker: m})
		return n > 0
	},
	Move: func(m *mob.Mob, delta mgl64.Vec3) mgl64.Vec3 {
		m.SetPosition(m.Pos().Add(delta))
		return delta
	},
	HurtRecently: func(m *mob.Mob) bool {
		if jm, ok := javamob.Of(m); ok {
			return jm.HurtTime() > 0
		}
		_, tick, ok := m.LastHurtBy()
		return ok && m.Ticks()-tick < 10
	},
}

func init() {
	Hooks.DoHurtTarget = func(m *mob.Mob, target world.Entity) bool {
		if jm, ok := javamob.Of(m); ok {
			return jm.DoHurtTarget(target)
		}
		return Hooks.MobAttack(m, target, m.Attr(mob.AttrAttackDamage))
	}
}

// bedrockSoundEvent turns a Java sound event name (entity.slime.squish_small)
// into the Bedrock level sound event the sessions play for the mob's type
// (squish): javasession turns it back into the Java sound of the type.
func bedrockSoundEvent(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		name = name[i+1:]
	}
	return strings.TrimSuffix(name, "_small")
}

// playSound is Entity.playSound through Hooks.
func playSound(m *mob.Mob, name string, volume, pitch float64) {
	Hooks.PlaySound(m, name, volume, pitch)
}

// hasLineOfSight is Mob.hasLineOfSight through Hooks.
func hasLineOfSight(m *mob.Mob, target world.Entity) bool { return Hooks.LineOfSight(m, target) }

// mobAttack hurts target with a melee hit of m through Hooks.
func mobAttack(m *mob.Mob, target world.Entity, dmg float64) bool {
	return Hooks.MobAttack(m, target, dmg)
}

// doHurtTarget is Mob.doHurtTarget through Hooks.
func doHurtTarget(m *mob.Mob, target world.Entity) bool { return Hooks.DoHurtTarget(m, target) }

// alive is Entity.isAlive.
func alive(e world.Entity) bool { return e != nil && sense.Alive(e) }

// isPlayer reports whether e is a player.
func isPlayer(e world.Entity) bool { return sense.IsPlayer(e) }

// gameModer is a player's game mode.
type gameModer interface{ GameMode() world.GameMode }

// creativeOrSpectator reports whether e is a player in creative or spectator.
func creativeOrSpectator(e world.Entity) bool {
	g, ok := e.(gameModer)
	if !ok {
		return false
	}
	gm := g.GameMode()
	return gm == world.GameModeCreative || gm == world.GameModeSpectator
}

// spectator reports whether e is a player in spectator mode.
func spectator(e world.Entity) bool {
	g, ok := e.(gameModer)
	return ok && g.GameMode() == world.GameModeSpectator
}

// canAttack is LivingEntity.canAttack(target): no players on peaceful, and
// the target must be seen as an enemy (canBeSeenAsEnemy: alive, not
// invulnerable, not a creative or spectator player).
func canAttack(m *mob.Mob, target world.Entity) bool {
	if isPlayer(target) && difficultyID(m.Tx()) == difficultyPeaceful {
		return false
	}
	return alive(target) && !sense.Invulnerable(target) && !creativeOrSpectator(target)
}

// hasLevitation reports whether m has the levitation effect.
func hasLevitation(m *mob.Mob) bool {
	_, ok := m.Effect(effect.Levitation)
	return ok
}

// lookAtEntity is Mob.lookAt(entity, yMax, xMax): turn straight towards the
// target's eyes (its middle for non-living entities), at most yMax/xMax
// degrees this tick. Unlike the look control it sets the rotation at once.
func lookAtEntity(m *mob.Mob, e world.Entity, yMax, xMax float64) {
	pos := m.Pos()
	tp := e.Position()
	xd := tp[0] - pos[0]
	zd := tp[2] - pos[2]
	yd := sense.EyePos(e)[1] - m.EyePosition()[1]
	if _, living := e.(entity.Living); !living {
		box := sense.BBox(e)
		yd = (box.Min()[1]+box.Max()[1])/2 - m.EyePosition()[1]
	}
	sd := math.Sqrt(xd*xd + zd*zd)
	yRotD := math.Atan2(zd, xd)*180/math.Pi - 90
	xRotD := -(math.Atan2(yd, sd) * 180 / math.Pi)
	m.SetPitch(rotlerp(m.Pitch(), xRotD, xMax))
	m.SetYaw(rotlerp(m.Yaw(), yRotD, yMax))
}

// defaultAttackReach is Mob.DEFAULT_ATTACK_REACH: sqrt(2.04) - 0.6.
var defaultAttackReach = math.Sqrt(2.04) - 0.6

// withinMeleeAttackRange is Mob.isWithinMeleeAttackRange: the mob's attack
// box (its box grown by the default reach sideways) touches the target's
// hitbox.
func withinMeleeAttackRange(m *mob.Mob, target world.Entity) bool {
	box := m.BBox().GrowVec3(mgl64.Vec3{defaultAttackReach, 0, defaultAttackReach})
	return box.IntersectsWith(sense.BBox(target))
}

// distanceSqr is Entity.distanceToSqr between two entities' positions.
func distanceSqr(a, b world.Entity) float64 { return a.Position().Sub(b.Position()).LenSqr() }

// NBT helpers for the Persister parts of the states.

func boolByte(v bool) uint8 {
	if v {
		return 1
	}
	return 0
}

func nbtInt(data map[string]any, key string) (int, bool) {
	switch v := data[key].(type) {
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case int16:
		return int(v), true
	case uint8:
		return int(v), true
	case int:
		return v, true
	}
	return 0, false
}

func nbtBool(data map[string]any, key string) bool {
	v, _ := nbtInt(data, key)
	return v != 0
}

// Targeting is TargetingConditions.
type Targeting struct {
	// Combat is forCombat (else forNonCombat).
	Combat bool
	// Range is range(r); 0 is no range check.
	Range float64
	// IgnoreLineOfSight and IgnoreInvisibility are ignoreLineOfSight and
	// ignoreInvisibilityTesting.
	IgnoreLineOfSight, IgnoreInvisibility bool
	// Selector is selector(...).
	Selector func(target world.Entity) bool
}

// Test is TargetingConditions.test(level, m, target).
func (c Targeting) Test(m *mob.Mob, target world.Entity) bool {
	if target == nil || target.H() == m.H() || !alive(target) || spectator(target) {
		return false // canBeSeenByAnyone
	}
	if c.Selector != nil && !c.Selector(target) {
		return false
	}
	if c.Combat && !canAttack(m, target) {
		return false
	}
	if c.Range > 0 {
		mod := 1.0
		if !c.IgnoreInvisibility {
			mod = visibilityPercent(target)
		}
		d := math.Max(c.Range*mod, 2)
		if distanceSqr(m, target) > d*d {
			return false
		}
	}
	return c.IgnoreLineOfSight || hasLineOfSight(m, target)
}

// visibilityPercent is LivingEntity.getVisibilityPercent without the mob
// heads: 0.8 while sneaking, 0.7 x the armour cover (at least 0.1) while
// invisible.
func visibilityPercent(e world.Entity) float64 {
	v := 1.0
	if sense.Sneaking(e) {
		v *= 0.8
	}
	if sense.Invisible(e) {
		v *= 0.7 * math.Max(sense.ArmourCover(e), 0.1)
	}
	return v
}
