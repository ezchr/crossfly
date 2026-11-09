//go:build crossfly_full

package physics

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

const (
	// gravity is MobMovementGravity::tickAirGravity (0.08), slowFallGravity
	// its slow falling branch.
	gravity         = 0.08
	slowFallGravity = 0.01
	// liquidGravity is tickMobWaterGravity / tickLavaGravity.
	liquidGravity = 0.02
	// verticalDrag is the Y drag outside liquids (Java travelInAir 0.98).
	verticalDrag = 0.98
	// airFriction is the horizontal drag in the air ("Mob Friction - Air").
	airFriction = 0.91
	// frictionModified is Mob::FRICTION_CONSTANT_MODIFIED, the ground
	// friction (block friction 0.6 x 0.91) the move speed is relative to in
	// GroundTravelTypeSystemImpl::_calcMoveRelativeSpeed.
	frictionModified = 0.546
	defaultFriction  = 0.6
	// airSpeed is MobMovement::getAirSpeed (AirSpeedComponent, 0.02).
	airSpeed = 0.02
	// inputDecay is MobTravelIntentSystemImpl::updatedMoveVelocity: the move
	// input shrinks by 2% before travel.
	inputDecay = 0.98
	// liquidJump is MobJumpSystem's swim-up impulse (0.04).
	liquidJump = 0.04
	// jumpDelay is MobJumpComponent noJumpDelay after a jump from the ground.
	jumpDelay = 10
	// climbSpeed is MobMovementClimb::applyAutoClimbing (0.2).
	climbSpeed = 0.2
	// climbClamp limits horizontal and downwards speed on climbable blocks
	// (source: java handleOnClimbable 0.15).
	climbClamp = 0.15
	// climbOutSpeed is MobMovementClimbOutOfLiquid (0.3), climbOutHeight the
	// height that must be free (0.6).
	climbOutSpeed  = 0.3
	climbOutHeight = 0.6
	// lavaDrag is the lava slowdown on every axis (source: java travelInLava).
	lavaDrag = 0.5
	// waterVerticalDrag is MobMovementDrag::tickApplyWaterDrag's Y factor.
	waterVerticalDrag = 0.8
	// maxVelocity caps a velocity component, like entity.limitVelocity.
	maxVelocity = 32
)

// Travel is the mob travel step (StageTravel), installed with mob.SetTravel.
// Per tick, in BDS system order:
//  1. liquid state at the current position (water, lava, eyes in water);
//  2. jumping: in a liquid +0.04, from the ground the jump power, then
//     a 10 tick delay while the jump is held;
//  3. the move input (decayed by 0.98) accelerates the mob: on the ground by
//     speed x (0.546/friction)^3, in the air by 0.02, in water by
//     underwater_movement, in lava by lava_movement;
//  4. climbable blocks clamp the speed; after a horizontal collision a
//     climber goes up at 0.2 without gravity;
//  5. the move: Y, X, Z collision with the block boxes, auto step up to
//     MaxAutoStep when walking into a block;
//  6. landing: fall damage (ceil(distance - 3 - jump boost)) through the
//     blocks' EntityLand (hay bale, slime, bed);
//  7. friction, drag and gravity of the travel type; in a liquid, a mob
//     against a wall with free space 0.6 above jumps out at 0.3;
//  8. pushing the entities it touches.
func Travel(m *mob.Mob) {
	tx := m.Tx()
	s := stateOf(m)
	p := m.Physics()
	in := *m.Input()
	if m.Dead() {
		// BDS Mob::aiStep: an immobile (dead) mob has no move input.
		in = mob.Input{}
	}

	pos := m.Pos()
	vel := limitVelocity(m.Vel())
	box := m.BBox()
	p.InWater, p.InLava, p.Underwater = liquidState(tx, box, m.EyePosition())
	if p.InWater || p.InLava {
		// CheckFallDamage resets in water, LavaResetFallDistanceSystem in lava.
		p.FallDistance = 0
	}
	// ImmuneToLavaDragFlag (magma cube): ActorMobilityUtils::shouldApplyLava
	// is false, so the mob jumps and travels in lava as on land.
	inLava := p.InLava && !s.lavaDragImmune

	// MobJumpSystem / MobJumpFromGroundSystemImpl.
	if s.noJumpDelay > 0 {
		s.noJumpDelay--
	}
	if in.Jump {
		switch {
		case p.InWater, inLava:
			vel[1] += liquidJump
		case p.OnGround && s.noJumpDelay == 0:
			vel[1] = jumpVelocity(m)
			s.noJumpDelay = jumpDelay
		}
	} else {
		s.noJumpDelay = 0
	}

	input := mgl64.Vec3{in.Strafe * inputDecay, in.Vertical * inputDecay, in.Forward * inputDecay}
	yaw := m.Yaw()
	wasOnGround := p.OnGround
	startY := pos[1]

	liquid := p.InWater || inLava
	friction := airFriction
	switch {
	case p.InWater:
		vel = moveRelative(vel, input, m.Attr(mob.AttrUnderwaterMovement), yaw)
	case inLava:
		vel = moveRelative(vel, input, m.Attr(mob.AttrLavaMovement), yaw)
	default:
		speed := airSpeed
		if wasOnGround {
			friction = groundFriction(tx, pos, box)
			r := frictionModified / friction
			speed = in.Speed * r * r * r
		}
		vel = moveRelative(vel, input, speed, yaw)
	}

	wall := wallClimber(m)
	autoClimb := false
	if !liquid && (wall && s.horizontal || s.canClimb && climbableAt(tx, pos, box)) {
		// source: java handleOnClimbable.
		vel[0] = mgl64.Clamp(vel[0], -climbClamp, climbClamp)
		vel[2] = mgl64.Clamp(vel[2], -climbClamp, climbClamp)
		vel[1] = math.Max(vel[1], -climbClamp)
		p.FallDistance = 0
		if s.horizontal {
			// MobMovementClimb::applyAutoClimbing: AutoClimbTravelFlag skips
			// gravity this tick.
			vel[1] = climbSpeed
			autoClimb = true
		}
	}

	moved, c := move(tx, s, box, vel, wasOnGround)
	pos = pos.Add(moved)
	m.SetPosition(pos)

	if c.x {
		vel[0] = 0
	}
	if c.z {
		vel[2] = 0
	}
	onGround := c.y && vel[1] < 0
	if onGround {
		if p.FallDistance > 0 && !liquid {
			// Blocks may bounce the mob (slime, bed): they see the velocity
			// it landed with.
			m.SetVelocity(vel)
			land(m, p.FallDistance)
			vel = limitVelocity(m.Vel())
		}
		p.FallDistance = 0
		if vel[1] < 0 {
			vel[1] = 0
		}
	} else {
		if c.y {
			vel[1] = 0
		}
		if moved[1] < 0 && !liquid && HasGravity(m) {
			// Flying and gravityless mobs build up no fall distance.
			p.FallDistance -= moved[1]
		}
	}

	switch {
	case p.InWater:
		vel[0] *= s.waterDrag
		vel[1] *= waterVerticalDrag
		vel[2] *= s.waterDrag
		if s.liquidGravity(m, tx, pos) {
			vel[1] -= liquidGravity
		}
	case inLava:
		vel = vel.Mul(lavaDrag)
		if s.liquidGravity(m, tx, pos) {
			vel[1] -= liquidGravity
		}
	default:
		if HasGravity(m) && !autoClimb {
			if _, slow := m.Effect(effect.SlowFalling); slow && vel[1] < 0 {
				vel[1] -= slowFallGravity
				p.FallDistance = 0
			} else {
				vel[1] -= gravity
			}
		}
		if s.flying {
			vel[1] *= friction
		} else {
			vel[1] *= verticalDrag
		}
		vel[0] *= friction
		vel[2] *= friction
	}
	if liquid && (c.x || c.z) {
		test := box.Translate(moved).Translate(mgl64.Vec3{vel[0], vel[1] + climbOutHeight - (pos[1] - startY), vel[2]})
		if free(tx, test) {
			vel[1] = climbOutSpeed
		}
	}

	p.OnGround = onGround
	p.HorizontalCollision = c.x || c.z
	p.VerticalCollision = c.y
	s.horizontal = p.HorizontalCollision
	m.SetVelocity(vel)

	if wall {
		if climbing := p.HorizontalCollision; climbing != m.Meta().Climbing {
			m.UpdateMeta(func(meta *mob.Meta) { meta.Climbing = climbing })
		}
	}
	if !m.Dead() {
		pushEntities(m, s)
	}
}

// moveRelative is Java's Entity.moveRelative (BDS MobMovement local move
// velocity): input is (strafe, up, forward), normalised when longer than 1,
// scaled by speed and turned by yaw (degrees).
func moveRelative(vel, input mgl64.Vec3, speed, yaw float64) mgl64.Vec3 {
	d := input.LenSqr()
	if d < 1.0e-7 || speed == 0 {
		return vel
	}
	if d > 1 {
		input = input.Mul(1 / math.Sqrt(d))
	}
	input = input.Mul(speed)
	sin, cos := math.Sincos(mgl64.DegToRad(yaw))
	return mgl64.Vec3{
		vel[0] + input[0]*cos - input[2]*sin,
		vel[1] + input[1],
		vel[2] + input[2]*cos + input[0]*sin,
	}
}

// groundFriction is the friction factor on the ground: the friction of the
// block under the feet (0.6 unless it has Friction) times 0.91, or 0.546
// above air (GroundTravelTypeSystemImpl::_calcMoveRelativeSpeed tests
// floor(minY - 0.1)).
func groundFriction(tx *world.Tx, pos mgl64.Vec3, box cube.BBox) float64 {
	b := tx.Block(cube.Pos{int(math.Floor(pos[0])), int(math.Floor(box.Min()[1] - 0.1)), int(math.Floor(pos[2]))})
	if _, air := b.(block.Air); air {
		return frictionModified
	}
	f := defaultFriction
	if fr, ok := b.(interface{ Friction() float64 }); ok {
		f = fr.Friction()
	}
	if f <= 0 {
		return frictionModified
	}
	return f * airFriction
}

// jumpVelocity is the Y velocity of a jump from the ground: the jump
// control's power (0.42 by default) plus 0.1 per jump boost level. A magma
// cube jumps 0.42 + 0.1 x its size whatever its jump control
// (MobJumpFromGroundSystemImpl::doLavaSlimeJumpFromGround).
func jumpVelocity(m *mob.Mob) float64 {
	power := DefaultJumpPower
	if stateOf(m).lavaSlime {
		power += 0.1 * float64(m.Meta().Variant)
	} else if jp, ok := m.JumpControl().(interface{ JumpPower() float64 }); ok {
		power = jp.JumpPower()
	}
	if e, ok := m.Effect(effect.JumpBoost); ok {
		power += 0.1 * float64(e.Level())
	}
	return power
}

// liquidGravity reports whether water or lava pulls m down
// (MobMovementGravity::shouldApplyLiquidGravity): a mob whose navigation
// can_sink is false (fish) floats unless its top or bottom is in air.
func (s *state) liquidGravity(m *mob.Mob, tx *world.Tx, pos mgl64.Vec3) bool {
	hasNav, canSink := s.navigation(m)
	if hasNav {
		return canSink || !topAndBottomNotInAir(tx, m, pos)
	}
	return HasGravity(m) || !topAndBottomNotInAir(tx, m, pos)
}

// topAndBottomNotInAir is MobMovementGravity::isCenterTopAndBottomNotInAir:
// the blocks half the mob's height above and below its position are not
// air.
func topAndBottomNotInAir(tx *world.Tx, m *mob.Mob, pos mgl64.Vec3) bool {
	h := m.BBox().Height() * 0.5
	top := tx.Block(cube.PosFromVec3(pos.Add(mgl64.Vec3{0, h})))
	bottom := tx.Block(cube.PosFromVec3(pos.Sub(mgl64.Vec3{0, h})))
	_, topAir := top.(block.Air)
	_, bottomAir := bottom.(block.Air)
	if topAir {
		if _, ok := tx.Liquid(cube.PosFromVec3(pos.Add(mgl64.Vec3{0, h}))); ok {
			topAir = false
		}
	}
	if bottomAir {
		if _, ok := tx.Liquid(cube.PosFromVec3(pos.Sub(mgl64.Vec3{0, h}))); ok {
			bottomAir = false
		}
	}
	return !topAir && !bottomAir
}

// wallClimber reports whether m climbs walls (spiders: the navigation.climb
// flag that makes BDS set WALLCLIMBING, MobMovementClimb::canAutoClimbNow).
func wallClimber(m *mob.Mob) bool { return m.HasComponent("minecraft:navigation.climb") }

// climbableAt reports whether the block at the feet is climbable
// (MobMovementClimb::getAutoClimbBlockTestPos: x and z of the position, the
// bottom of the box).
func climbableAt(tx *world.Tx, pos mgl64.Vec3, box cube.BBox) bool {
	return Climbable(tx.Block(cube.Pos{int(math.Floor(pos[0])), int(math.Floor(box.Min()[1])), int(math.Floor(pos[2]))}))
}

// climbableNames are the climbable blocks Dragonfly has no type for
// (BlockProperty climbable in BDS).
var climbableNames = map[string]bool{
	"minecraft:scaffolding": true, "minecraft:twisting_vines": true, "minecraft:weeping_vines": true,
	"minecraft:cave_vines": true, "minecraft:cave_vines_body_with_berries": true,
	"minecraft:cave_vines_head_with_berries": true,
}

// Climbable reports whether a mob with minecraft:can_climb climbs b.
func Climbable(b world.Block) bool {
	switch b.(type) {
	case block.Air:
		return false
	case block.Ladder, block.Vines:
		return true
	}
	name, _ := b.EncodeBlock()
	return climbableNames[name]
}

// limitVelocity drops NaN and infinite parts and caps the rest (a broken
// velocity froze a world once; see entity.limitVelocity).
func limitVelocity(vel mgl64.Vec3) mgl64.Vec3 {
	for i, v := range vel {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			vel[i] = 0
			continue
		}
		vel[i] = min(max(v, -maxVelocity), maxVelocity)
	}
	return vel
}

// faller is the mob as blocks see it when it lands: it adds the
// FallDistance/ResetFallDistance methods block.EntityLander implementations
// look for (hay bale, slime, bed), backed by the landing distance.
type faller struct {
	*mob.Mob
	d *float64
}

func (f faller) FallDistance() float64 { return *f.d }
func (f faller) ResetFallDistance()    { *f.d = 0 }

// land runs the EntityLand of the first lander block under m and hurts it
// for the fall (Actor::checkFallDamage -> Mob::causeFallDamageToActor:
// ceil(distance - 3 - jump boost level), fall damage multiplier 1).
func land(m *mob.Mob, dist float64) {
	tx := m.Tx()
	box := m.BBox()
	inner := box.Grow(-0.0001)
	y := int(math.Floor(box.Min()[1] - 0.0001))
	minX, maxX := int(math.Floor(inner.Min()[0])), int(math.Floor(inner.Max()[0]))
	minZ, maxZ := int(math.Floor(inner.Min()[2])), int(math.Floor(inner.Max()[2]))
search:
	for x := minX; x <= maxX; x++ {
		for z := minZ; z <= maxZ; z++ {
			pos := cube.Pos{x, y, z}
			if l, ok := tx.Block(pos).(block.EntityLander); ok {
				l.EntityLand(pos, tx, faller{Mob: m, d: &dist}, &dist)
				break search
			}
		}
	}
	jb := 0.0
	if e, ok := m.Effect(effect.JumpBoost); ok {
		jb = float64(e.Level())
	}
	if dmg := math.Ceil(dist - 3 - jb); dmg > 0 {
		m.Hurt(dmg, entity.FallDamageSource{})
	}
}
