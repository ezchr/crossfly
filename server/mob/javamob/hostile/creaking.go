//go:build crossfly_full

package hostile

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

const creakingID = "minecraft:creaking"

// Creaking constants (Creaking).
const (
	creakingActivationRangeSq     = 144
	creakingAttackAnimation       = 15
	creakingInvulnerabilityTicks  = 8
	creakingTwitchDeathDuration   = 45
	creakingMaxPlayerStuckCounter = 4
)

// CreakingAttributes are Creaking.createAttributes: 1 health, 0.4 speed, 3
// attack damage, 32 follow range, 1.0625 step height.
func CreakingAttributes() (health, speed, attack, followRange, step float64) {
	return 1, float64(float32(0.4)), 3, 32, 1.0625
}

// CreakingHeart is what a creaking needs from the creaking heart block
// entity it is bound to (CreakingHeartBlockEntity).
type CreakingHeart interface {
	// IsProtector is CreakingHeartBlockEntity.isProtector(creaking).
	IsProtector(creaking *world.EntityHandle) bool
	// CreakingHurt is CreakingHeartBlockEntity.creakingHurt: a player hit
	// the creaking (the heart shows the trail and drops resin).
	CreakingHurt()
}

// HeartAt returns the creaking heart at pos, if there is one (the block
// package sets it once the creaking heart block exists).
var HeartAt = func(tx *world.Tx, pos cube.Pos) (CreakingHeart, bool) { return nil, false }

// Creaking is the per-mob state of a creaking (Creaking).
type Creaking struct {
	canMove, active, tearingDown bool
	home                         *cube.Pos
	attackAnimation              int
	invulnerability              int
	playerStuck                  int
	deathTime                    int
}

// creakingOf returns the creaking state of m.
func creakingOf(m *mob.Mob) *Creaking {
	return stateOf(m, func() *Creaking { return &Creaking{canMove: true} })
}

// CanMove is Creaking.canMove (CAN_MOVE).
func (c *Creaking) CanMove() bool { return c.canMove }

// Active is Creaking.isActive (IS_ACTIVE).
func (c *Creaking) Active() bool { return c.active }

// HeartBound is Creaking.isHeartBound.
func (c *Creaking) HeartBound() bool { return c.home != nil }

// creakingState is the Bedrock actor property minecraft:creaking_state for
// the Java data (javasession turns it back into CAN_MOVE, IS_ACTIVE and
// IS_TEARING_DOWN).
func (c *Creaking) creakingState() string {
	switch {
	case c.tearingDown:
		return "twitching"
	case c.active && !c.canMove:
		return "hostile_observed"
	case c.active:
		return "hostile_unobserved"
	}
	return "neutral"
}

// syncCreaking shows the state to both clients.
func syncCreaking(m *mob.Mob) {
	st := creakingOf(m).creakingState()
	if v, _ := m.Property("minecraft:creaking_state"); v != st {
		_ = m.SetProperty("minecraft:creaking_state", st)
	}
}

// SetCreakingTransient is Creaking.setTransient: bound to the heart at pos
// (it saves home_pos and avoids damaging blocks, powder snow and lava more,
// fire not at all: the base's path malus, set by the caller).
func SetCreakingTransient(m *mob.Mob, pos cube.Pos) {
	c := creakingOf(m)
	c.home = &pos
	syncCreaking(m)
}

// creakingWatcher is a player a creaking checks: whether it may be attacked
// and whether it looks at the creaking.
type creakingWatcher struct {
	attackable bool
	// disguised is LivingEntity.PLAYER_NOT_WEARING_DISGUISE_ITEM failing: a
	// carved pumpkin on the head (only counts once the creaking is active).
	disguised bool
	looking   bool
	distSq    float64
	h         *world.EntityHandle
}

// checkCanMove is Creaking.checkCanMove over the nearest players (the
// NEAREST_PLAYERS memory, nearest first): a player that may be attacked and
// looks at it freezes it; when not yet active, such a player within 12
// blocks activates it (made its attack target). With no attackable player
// left an active creaking deactivates. It returns whether it may move and
// the player to activate on (nil for none) or deactivate.
func (c *Creaking) checkCanMove(players []creakingWatcher) (canMove bool, activate *world.EntityHandle, deactivate bool) {
	if len(players) == 0 {
		return true, nil, c.active
	}
	potential := false
	for _, p := range players {
		if !p.attackable {
			continue
		}
		potential = true
		if (!c.active || !p.disguised) && p.looking {
			if c.active {
				return false, nil, false
			}
			if p.distSq < creakingActivationRangeSq {
				return false, p.h, false
			}
		}
	}
	return true, nil, !potential && c.active
}

// lookingAt is LivingEntity.isLookingAtMe(target, coneSize, adjustForDistance
// false, seeThroughTransparentBlocks): the target's view direction is within
// the cone of one of the gaze heights on the creaking, with a clear line.
func lookingAt(creakingPos mgl64.Vec3, look, eye mgl64.Vec3, cone float64, gazeHeights []float64, clear func(to mgl64.Vec3) bool) bool {
	look = look.Normalize()
	for _, h := range gazeHeights {
		dir := mgl64.Vec3{creakingPos[0] - eye[0], h - eye[1], creakingPos[2] - eye[2]}
		if dir.Len() == 0 {
			continue
		}
		if look.Dot(dir.Normalize()) > 1-cone && clear(mgl64.Vec3{creakingPos[0], h, creakingPos[2]}) {
			return true
		}
	}
	return false
}

// creakingGazeHeights are the heights Creaking.checkCanMove tests: its eyes,
// half a block up, and halfway between.
func creakingGazeHeights(y, eyeY, scale float64) []float64 {
	return []float64{eyeY, y + 0.5*scale, (eyeY + y) / 2}
}

// viewVector is Entity.getViewVector from yaw and pitch in degrees.
func viewVector(yaw, pitch float64) mgl64.Vec3 {
	p := pitch * math.Pi / 180
	y := -yaw * math.Pi / 180
	cy, sy := math.Cos(y), math.Sin(y)
	cp, sp := math.Cos(p), math.Sin(p)
	return mgl64.Vec3{sy * cp, -sp, cy * cp}
}

// CreakingAIStep is the server part of Creaking.aiStep: the animations count
// down and CAN_MOVE follows checkCanMove (stopping in place when frozen; a
// change is an entity_action game event). activate/deactivate set or erase
// the brain's attack target.
func CreakingAIStep(m *mob.Mob, players []creakingWatcher, activate func(h *world.EntityHandle), deactivate func(), stopInPlace func(), gameEvent func()) {
	c := creakingOf(m)
	if c.invulnerability > 0 {
		c.invulnerability--
	}
	if c.attackAnimation > 0 {
		c.attackAnimation--
	}
	now, act, deact := c.checkCanMove(players)
	if act != nil {
		activate(act)
		gameEvent()
		c.active = true
	}
	if deact {
		deactivate()
		gameEvent()
		c.active = false
	}
	if now != c.canMove {
		gameEvent()
		if !now {
			stopInPlace()
		}
	}
	c.canMove = now
	syncCreaking(m)
}

// CreakingTick is the server part of Creaking.tick: a heart-bound creaking
// whose heart no longer protects it dies at once.
func CreakingTick(m *mob.Mob) {
	c := creakingOf(m)
	if c.home == nil {
		return
	}
	if h, ok := HeartAt(m.Tx(), *c.home); !ok || !h.IsProtector(m.H()) {
		m.SetHealth(0)
	}
}

// CreakingHurt is Creaking.hurtServer for a heart-bound creaking (an
// unbound one takes damage normally: it reports false then): only living
// attackers, projectiles or a responsible player count, then it plays the
// invulnerable shake (entity event 66) for 8 ticks, during which it ignores
// hits; a player's hit tells the heart. It never loses health this way; it
// reports true (the hurt is handled) and whether the hit "landed".
func CreakingHurt(m *mob.Mob, bypassesInvulnerability, livingOrProjectile, byPlayer bool, shake func()) (handled, landed bool) {
	c := creakingOf(m)
	if c.home == nil || bypassesInvulnerability {
		return false, false
	}
	if c.invulnerability > 0 || m.Dead() {
		return true, false
	}
	if !livingOrProjectile && !byPlayer {
		return true, false
	}
	c.invulnerability = creakingInvulnerabilityTicks
	shake()
	if h, ok := HeartAt(m.Tx(), *c.home); ok && h.IsProtector(m.H()) {
		if byPlayer {
			h.CreakingHurt()
		}
		playSound(m, "entity.creaking.sway", 1, voicePitch(m.Rand()))
	}
	return true, true
}

// CreakingDoHurtTarget is Creaking.doHurtTarget's start: the attack
// animation (entity event 4) for 15 ticks.
func CreakingDoHurtTarget(m *mob.Mob) { creakingOf(m).attackAnimation = creakingAttackAnimation }

// SetCreakingTearingDown is Creaking.setTearingDown: its heart was broken;
// it twitches for 45 ticks and crumbles (tickDeath).
func SetCreakingTearingDown(m *mob.Mob) {
	creakingOf(m).tearingDown = true
	syncCreaking(m)
}

// CreakingTickDeath is Creaking.tickDeath for a heart-bound creaking tearing
// down: it reports true while the creaking handles its own death (twitching,
// then tearDown after 45 ticks: crumbling particles, the death sound, gone).
func CreakingTickDeath(m *mob.Mob, tearDown func()) bool {
	c := creakingOf(m)
	if c.home == nil || !c.tearingDown {
		return false
	}
	c.deathTime++
	if c.deathTime > creakingTwitchDeathDuration {
		playSound(m, "entity.creaking.death", 1, voicePitch(m.Rand()))
		tearDown()
		_ = m.Close()
	}
	return true
}

// creakingPlayerStuck is Creaking.playerIsStuckInYou: a player whose eyes
// are inside the creaking for more than 4 checks in a row.
func (c *Creaking) creakingPlayerStuck(box cube.BBox, eyes []mgl64.Vec3) bool {
	if len(eyes) == 0 {
		c.playerStuck = 0
		return false
	}
	for _, e := range eyes {
		if box.Vec3Within(e) {
			c.playerStuck++
			return c.playerStuck > creakingMaxPlayerStuckCounter
		}
	}
	c.playerStuck = 0
	return false
}

// EncodeNBT writes Creaking.addAdditionalSaveData (home_pos).
func (c *Creaking) EncodeNBT(data map[string]any) {
	if c.home != nil {
		data["home_pos"] = []int32{int32(c.home[0]), int32(c.home[1]), int32(c.home[2])}
	}
}

// DecodeNBT reads home_pos (setTransient).
func (c *Creaking) DecodeNBT(m *mob.Mob, data map[string]any) {
	if a, ok := data["home_pos"].([]int32); ok && len(a) == 3 {
		SetCreakingTransient(m, cube.Pos{int(a[0]), int(a[1]), int(a[2])})
	}
}
