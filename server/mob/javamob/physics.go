//go:build crossfly_full

package javamob

import (
	"math"
	"slices"
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob/javamob/path"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// updateFluidInteraction is Entity.updateFluidInteraction: the water and lava the mob is in, their currents
// (EntityFluidInteraction, entity.ApplyFluidCurrents), and the eyes in water.
func (m *Mob) updateFluidInteraction() {
	s := m.s
	water, lava := entity.ApplyFluidCurrents(m.Ent, m.Tx())
	s.waterHeight, s.lavaHeight = water, lava
	inWater := water > 0
	if inWater {
		m.ResetFallDistance()
	}
	s.inWater = inWater
	if s.firstTick {
		s.lavaHeight = 0 // Entity.isInLava is false on the first tick.
	}
	pos := m.Position()
	eye := m.EyeY()
	eyePos := cube.Pos{int(math.Floor(pos[0])), int(math.Floor(eye)), int(math.Floor(pos[2]))}
	s.eyeInWater = false
	if l, ok := m.Tx().Liquid(eyePos); ok && l.LiquidType() == "water" {
		top := float64(eyePos[1]) + liquidHeight(m.Tx(), eyePos, l)
		s.eyeInWater = eye <= top
	}
}

// liquidHeight is FluidState.getHeight: a full block under the same liquid, amount/9 otherwise.
func liquidHeight(tx *world.Tx, pos cube.Pos, l world.Liquid) float64 {
	if above, ok := tx.Liquid(pos.Side(cube.FaceUp)); ok && above.LiquidType() == l.LiquidType() {
		return 1
	}
	return float64(float32(l.LiquidDepth()) / 9)
}

// travel is LivingEntity.travel.
func (m *Mob) travel(input mgl64.Vec3) {
	if m.InLiquid() {
		m.travelInFluid(input)
		return
	}
	m.travelInAir(input)
}

// gravity is LivingEntity.getEffectiveGravity: the gravity attribute, 0.01 at most while falling with slow falling.
func (m *Mob) gravity() float64 {
	if m.Meta().NoGravity {
		return 0
	}
	g := m.Attr(AttrGravity)
	if m.Velocity()[1] <= 0 {
		if _, ok := m.Effect(effect.SlowFalling); ok {
			return math.Min(g, 0.01)
		}
	}
	return g
}

// modifiedFriction is LivingEntity.computeModifiedFriction.
func modifiedFriction(friction, modifier float32) float32 {
	return min(max(1-(1-friction)*modifier, 0), 1)
}

// travelInAir is LivingEntity.travelInAir.
func (m *Mob) travelInAir(input mgl64.Vec3) {
	s := m.s
	friction := float32(1)
	if s.onGround {
		friction = modifiedFriction(blockFriction(m.Tx().Block(m.onPos(0.500001))), float32(m.Attr(AttrFrictionModifier)))
	}
	m.moveRelative(m.frictionInfluencedSpeed(friction), input)
	m.SetVelocity(m.handleOnClimbable(m.Velocity()))
	m.move(m.Velocity())
	if m.gone() {
		return
	}
	v := m.Velocity()
	if (s.horizontalCollision || s.jumping) && m.OnClimbable() {
		v[1] = 0.2
	}
	vy := v[1]
	if lev, ok := m.Effect(effect.Levitation); ok {
		vy += (0.05*float64(lev.Level()) - v[1]) * 0.2
	} else {
		vy -= m.gravity()
	}
	drag := modifiedFriction(0.91, float32(m.Attr(AttrAirDragModifier)))
	f := float64(friction * drag)
	vf := float64(modifiedFriction(0.98, float32(m.Attr(AttrAirDragModifier))))
	m.SetVelocity(mgl64.Vec3{v[0] * f, vy * vf, v[2] * f})
}

// frictionInfluencedSpeed is LivingEntity.getFrictionInfluencedSpeed.
func (m *Mob) frictionInfluencedSpeed(friction float32) float64 {
	if !m.s.onGround {
		return float64(float32(0.02))
	}
	speed := float32(m.s.speed)
	if float64(friction) > 0.6 {
		return float64(speed * (0.21600002 / (friction * friction * friction)))
	}
	return float64(speed)
}

// travelInFluid is LivingEntity.travelInFluid (travelInWater, travelInLava).
func (m *Mob) travelInFluid(input mgl64.Vec3) {
	s := m.s
	falling := m.Velocity()[1] <= 0
	oldY := m.Position()[1]
	g := m.gravity()
	if s.inWater {
		slow := float32(0.8) // getWaterSlowDown
		speed := float32(0.02)
		eff := float32(m.Attr(AttrWaterMovementEfficiency))
		if !s.onGround {
			eff *= 0.5
		}
		if eff > 0 {
			slow += (0.54600006 - slow) * eff
			speed += (float32(s.speed) - speed) * eff
		}
		m.moveRelative(float64(speed), input)
		m.move(m.Velocity())
		if m.gone() {
			return
		}
		v := m.Velocity()
		if s.horizontalCollision && m.OnClimbable() {
			v[1] = 0.2
		}
		v = mgl64.Vec3{v[0] * float64(slow), v[1] * float64(float32(0.8)), v[2] * float64(slow)}
		m.SetVelocity(fluidFallingAdjusted(g, falling, v))
	} else {
		m.moveRelative(float64(float32(0.02)), input)
		m.move(m.Velocity())
		if m.gone() {
			return
		}
		if s.lavaHeight <= m.FluidJumpThreshold() {
			v := m.Velocity()
			v = mgl64.Vec3{v[0] * 0.5, v[1] * float64(float32(0.8)), v[2] * 0.5}
			m.SetVelocity(fluidFallingAdjusted(g, falling, v))
		} else {
			m.SetVelocity(m.Velocity().Mul(0.5))
		}
		if g != 0 {
			m.SetVelocity(m.Velocity().Add(mgl64.Vec3{0, -g / 4}))
		}
	}
	// jumpOutOfFluid.
	v := m.Velocity()
	if s.horizontalCollision && m.isFree(mgl64.Vec3{v[0], v[1] + float64(float32(0.6)) - m.Position()[1] + oldY, v[2]}) {
		m.SetVelocity(mgl64.Vec3{v[0], float64(float32(0.3)), v[2]})
	}
}

// fluidFallingAdjusted is LivingEntity.getFluidFallingAdjustedMovement.
func fluidFallingAdjusted(g float64, falling bool, v mgl64.Vec3) mgl64.Vec3 {
	if g == 0 {
		return v
	}
	if falling && math.Abs(v[1]-0.005) >= 0.003 && math.Abs(v[1]-g/16) < 0.003 {
		v[1] = -0.003
	} else {
		v[1] -= g / 16
	}
	return v
}

// isFree is Entity.isFree: the box moved by d touches no block and no liquid.
func (m *Mob) isFree(d mgl64.Vec3) bool {
	bb := m.BBox().Translate(d)
	if path.Collides(m.Tx(), bb) {
		return false
	}
	tx := m.Tx()
	for pos := range cube.Range3D(cube.PosFromVec3(bb.Min()), cube.PosFromVec3(bb.Max())) {
		if l, ok := tx.Liquid(pos); ok {
			top := float64(pos[1]) + liquidHeight(tx, pos, l)
			if top >= bb.Min()[1] {
				return false
			}
		}
	}
	return true
}

// moveRelative is Entity.moveRelative with getInputVector.
func (m *Mob) moveRelative(speed float64, input mgl64.Vec3) {
	l := input.LenSqr()
	if l < 1e-7 {
		return
	}
	mv := input
	if l > 1 {
		mv = input.Normalize()
	}
	mv = mv.Mul(speed)
	rad := float32(m.YRot()) * (math.Pi / 180)
	sin, cos := float64(mthSin(rad)), float64(mthCos(rad))
	m.SetVelocity(m.Velocity().Add(mgl64.Vec3{mv[0]*cos - mv[2]*sin, mv[1], mv[2]*cos + mv[0]*sin}))
}

// handleOnClimbable is LivingEntity.handleOnClimbable: at most 0.15 a tick sideways and down on a ladder.
func (m *Mob) handleOnClimbable(v mgl64.Vec3) mgl64.Vec3 {
	if !m.OnClimbable() {
		return v
	}
	m.ResetFallDistance()
	const max = float64(float32(0.15))
	return mgl64.Vec3{math.Max(-max, math.Min(max, v[0])), math.Max(v[1], -max), math.Max(-max, math.Min(max, v[2]))}
}

// OnClimbable is LivingEntity.onClimbable: in a climbable block (ladder, vines, scaffolding) or an open trapdoor
// over a ladder; or the behaviour climbs (Climber, a spider on a wall).
func (m *Mob) OnClimbable() bool {
	if c, ok := m.s.b.(Climber); ok && c.OnClimbable(m) {
		return true
	}
	tx := m.Tx()
	pos := cube.PosFromVec3(m.Position())
	b := tx.Block(pos)
	if climbable(b) {
		return true
	}
	if t, ok := b.(block.WoodTrapdoor); ok && t.Open {
		if l, ok := tx.Block(pos.Side(cube.FaceDown)).(block.Ladder); ok && l.Facing == t.Facing {
			return true
		}
	}
	return false
}

// climbable is BlockTags.CLIMBABLE.
func climbable(b world.Block) bool {
	switch b.(type) {
	case block.Ladder, block.Vines:
		return true
	}
	name, _ := b.EncodeBlock()
	switch name {
	case "minecraft:scaffolding", "minecraft:weeping_vines", "minecraft:twisting_vines", "minecraft:cave_vines", "minecraft:cave_vines_body_with_berries",
		"minecraft:cave_vines_head_with_berries":
		return true
	}
	return false
}

// blockFriction is Block.getFriction: 0.6 unless the block says otherwise (ice, slime).
func blockFriction(b world.Block) float32 {
	if f, ok := b.(block.Frictional); ok {
		return float32(f.Friction())
	}
	return 0.6
}

// blockSpeedFactor is Block.getSpeedFactor: soul sand and honey slow mobs to 0.4.
func blockSpeedFactor(b world.Block) float32 {
	if _, ok := b.(block.SoulSand); ok || isHoney(b) {
		return 0.4
	}
	return 1
}

// blockJumpFactor is Block.getJumpFactor: honey halves jumps.
func blockJumpFactor(b world.Block) float32 {
	if isHoney(b) {
		return 0.5
	}
	return 1
}

// blockJumpFactor is Entity.getBlockJumpFactor.
func (m *Mob) blockJumpFactor() float32 {
	tx := m.Tx()
	here := blockJumpFactor(tx.Block(cube.PosFromVec3(m.Position())))
	if here == 1 {
		return blockJumpFactor(tx.Block(m.onPos(0.500001)))
	}
	return here
}

// speedFactor is LivingEntity.getBlockSpeedFactor.
func (m *Mob) speedFactor() float64 {
	tx := m.Tx()
	pos := cube.PosFromVec3(m.Position())
	b := tx.Block(pos)
	f := blockSpeedFactor(b)
	if _, water := b.(block.Water); !water && f == 1 {
		f = blockSpeedFactor(tx.Block(m.onPos(0.500001)))
	}
	eff := float32(m.Attr(AttrMovementEfficiency))
	return float64(f + (1-f)*eff)
}

// onPos is Entity.getOnPos(offset): the block the mob stands on (its supporting block), or the one offset below
// its feet.
func (m *Mob) onPos(offset float32) cube.Pos {
	pos := m.Position()
	if sp := m.s.supporting; sp != nil {
		if offset <= 1e-5 {
			return *sp
		}
		below := m.Tx().Block(*sp)
		if (offset > 0.5 || !path.IsFence(below)) && !path.IsWallOrGate(below) {
			return cube.Pos{sp[0], int(math.Floor(pos[1] - float64(offset))), sp[2]}
		}
		return *sp
	}
	return cube.Pos{int(math.Floor(pos[0])), int(math.Floor(pos[1] - float64(offset))), int(math.Floor(pos[2]))}
}

// move is Entity.move(SELF, delta) for a mob.
func (m *Mob) move(delta mgl64.Vec3) {
	s := m.s
	tx := m.Tx()
	if s.stuck.LenSqr() > 1e-7 {
		delta = mgl64.Vec3{delta[0] * s.stuck[0], delta[1] * s.stuck[1], delta[2] * s.stuck[2]}
		s.stuck = mgl64.Vec3{}
		m.SetVelocity(mgl64.Vec3{})
	}
	mv := m.collide(delta)
	l := mv.LenSqr()
	if l > 1e-7 || delta.LenSqr()-l < 1e-7 {
		m.SetPosition(m.Position().Add(mv))
	}
	xColl := !mthEqual(delta[0], mv[0])
	zColl := !mthEqual(delta[2], mv[2])
	s.horizontalCollision = xColl || zColl
	if math.Abs(delta[1]) > 0 {
		s.verticalCollision = delta[1] != mv[1]
		s.verticalCollisionBelow = s.verticalCollision && delta[1] < 0
		s.onGround = s.verticalCollisionBelow
		m.checkSupportingBlock(mv)
	}
	s.minorHorizontalCollision = false
	effectPos := m.onPos(0.2)
	effect := tx.Block(effectPos)
	m.checkFallDamage(mv[1], effectPos, effect)
	if m.gone() {
		return
	}
	if s.verticalCollision && delta[1] != 0 || s.horizontalCollision {
		m.restitute(effect, xColl, zColl, mv)
	}
	f := m.speedFactor()
	v := m.Velocity()
	m.SetVelocity(mgl64.Vec3{v[0] * f, v[1], v[2] * f})
}

// restitute is Entity.restituteMovementAfterCollisions: a mob stopped by a wall loses that part of its speed, and
// one landing on a bouncy block (slime, bed) bounces.
func (m *Mob) restitute(on world.Block, xColl, zColl bool, mv mgl64.Vec3) {
	s := m.s
	v := m.Velocity()
	out := v
	restitution := m.Attr(AttrBounciness)
	if xColl {
		out[0] = -v[0] * restitution
	}
	if zColl {
		out[2] = -v[2] * restitution
	}
	if s.verticalCollision {
		if s.verticalCollisionBelow {
			if -v[1] > m.gravity() {
				restitution = math.Max(restitution, bounceRestitution(on))
			} else {
				restitution = 0
			}
		}
		comp, drag := 0.0, 1.0
		if restitution > 0 && v[1] != 0 {
			portion := mv[1] / v[1]
			comp = portion * m.gravity()
			drag = 1 + portion*(float64(float32(0.98))-1)
		}
		out[1] = (comp - v[1]) * drag * restitution
	}
	m.SetVelocity(out)
}

// bounceRestitution is BlockBehaviour.Properties.bounceRestitution of the blocks living entities bounce on.
func bounceRestitution(b world.Block) float64 {
	switch b.(type) {
	case block.Slime:
		return 1
	case block.Bed:
		return float64(float32(0.66))
	}
	return 0
}

// checkSupportingBlock is Entity.checkSupportingBlock: the block the mob stands on.
func (m *Mob) checkSupportingBlock(mv mgl64.Vec3) {
	s := m.s
	if !s.onGround {
		s.onGroundNoBlocks = false
		s.supporting = nil
		return
	}
	bb := m.BBox()
	test := cube.Box(bb.Min()[0], bb.Min()[1]-1e-6, bb.Min()[2], bb.Max()[0], bb.Min()[1], bb.Max()[2])
	sp, ok := m.findSupportingBlock(test)
	if ok || s.onGroundNoBlocks {
		if ok {
			s.supporting = &sp
		} else {
			s.supporting = nil
		}
	} else {
		sp, ok = m.findSupportingBlock(test.Translate(mgl64.Vec3{-mv[0], 0, -mv[2]}))
		if ok {
			s.supporting = &sp
		} else {
			s.supporting = nil
		}
	}
	s.onGroundNoBlocks = !ok
}

// findSupportingBlock is CollisionGetter.findSupportingBlock: of the blocks whose collision box meets box, the one
// closest to the mob.
func (m *Mob) findSupportingBlock(box cube.BBox) (cube.Pos, bool) {
	tx := m.Tx()
	pos := m.Position()
	var best cube.Pos
	bestD, found := math.MaxFloat64, false
	for p := range cube.Range3D(cube.PosFromVec3(box.Min().Sub(mgl64.Vec3{1e-7, 1e-7, 1e-7})), cube.PosFromVec3(box.Max())) {
		for _, b := range tx.Block(p).Model().BBox(p, tx) {
			if !overlaps(b.Translate(p.Vec3()), box) {
				continue
			}
			d := p.Vec3Centre().Sub(pos).LenSqr()
			if d < bestD || d == bestD && (p[1] > best[1] || p[1] == best[1] && (p[0] < best[0] || p[0] == best[0] && p[2] < best[2])) {
				best, bestD, found = p, d, true
			}
			break
		}
	}
	return best, found
}

// collide is Entity.collide: the move cut short by block collision boxes, stepping up a block edge when that gets
// the mob further.
func (m *Mob) collide(delta mgl64.Vec3) mgl64.Vec3 {
	s := m.s
	tx := m.Tx()
	bb := m.BBox()
	step := delta
	if delta.LenSqr() != 0 {
		s.boxes = collisionBoxes(s.boxes[:0], tx, bb.Extend(delta))
		step = collideWithBoxes(delta, bb, s.boxes)
	}
	xColl, yColl, zColl := delta[0] != step[0], delta[1] != step[1], delta[2] != step[2]
	landed := yColl && delta[1] < 0
	maxStep := float32(m.MaxUpStep())
	if maxStep > 0 && (landed || s.onGround) && (xColl || zColl) {
		grounded := bb
		if landed {
			grounded = bb.Translate(mgl64.Vec3{0, step[1]})
		}
		stepBox := grounded.Extend(mgl64.Vec3{delta[0], float64(maxStep), delta[2]})
		if !landed {
			stepBox = stepBox.Extend(mgl64.Vec3{0, -1e-5})
		}
		s.boxes = collisionBoxes(s.boxes[:0], tx, stepBox)
		skip := float32(step[1])
		var cands []float32
		for _, b := range s.boxes {
			for _, c := range [2]float64{b.Min()[1], b.Max()[1]} {
				rel := float32(c - grounded.Min()[1])
				if rel < 0 || rel == skip {
					continue
				}
				if rel > maxStep {
					break
				}
				if !slices.Contains(cands, rel) {
					cands = append(cands, rel)
				}
			}
		}
		slices.Sort(cands)
		for _, c := range cands {
			up := collideWithBoxes(mgl64.Vec3{delta[0], float64(c), delta[2]}, grounded, s.boxes)
			if up[0]*up[0]+up[2]*up[2] > step[0]*step[0]+step[2]*step[2] {
				return up.Sub(mgl64.Vec3{0, bb.Min()[1] - grounded.Min()[1]})
			}
		}
	}
	return step
}

// collisionBoxes appends the world-space collision boxes of the blocks around box.
func collisionBoxes(dst []cube.BBox, tx *world.Tx, box cube.BBox) []cube.BBox {
	return entity.AppendBlockBBoxesAround(dst, tx, box)
}

// collideWithBoxes is Entity.collideWithShapes: Y first, then the larger of X and Z.
func collideWithBoxes(d mgl64.Vec3, bb cube.BBox, boxes []cube.BBox) mgl64.Vec3 {
	if len(boxes) == 0 {
		return d
	}
	order := [3]int{1, 0, 2}
	if math.Abs(d[0]) < math.Abs(d[2]) {
		order = [3]int{1, 2, 0}
	}
	var out mgl64.Vec3
	for _, axis := range order {
		if d[axis] == 0 {
			continue
		}
		c := collideAxis(axis, bb.Translate(out), boxes, d[axis])
		out[axis] = c
	}
	return out
}

// collideAxis is Shapes.collide along one axis: how far bb moves by d before it hits one of boxes.
func collideAxis(axis int, bb cube.BBox, boxes []cube.BBox, d float64) float64 {
	const eps = 1e-7
	if math.Abs(d) < eps {
		return 0
	}
	o1, o2 := (axis+1)%3, (axis+2)%3
	for _, b := range boxes {
		if b.Max()[o1]-eps <= bb.Min()[o1] || b.Min()[o1]+eps >= bb.Max()[o1] ||
			b.Max()[o2]-eps <= bb.Min()[o2] || b.Min()[o2]+eps >= bb.Max()[o2] {
			continue
		}
		if d > 0 && b.Min()[axis] >= bb.Max()[axis]-eps {
			d = math.Min(d, b.Min()[axis]-bb.Max()[axis])
		} else if d < 0 && b.Max()[axis] <= bb.Min()[axis]+eps {
			d = math.Max(d, b.Max()[axis]-bb.Min()[axis])
		}
		if math.Abs(d) < eps {
			return 0
		}
	}
	return d
}

// checkFallDamage is LivingEntity.checkFallDamage and Entity.checkFallDamage: falling adds up the distance, landing
// hurts (Block.fallOn).
func (m *Mob) checkFallDamage(ya float64, pos cube.Pos, on world.Block) {
	s := m.s
	if !s.inWater && ya < 0 {
		s.fallDistance -= ya
	}
	if !s.onGround {
		return
	}
	if s.fallDistance > 0 {
		m.causeFallDamage(s.fallDistance, fallDamageMultiplier(on))
		if m.gone() {
			return
		}
	}
	m.ResetFallDistance()
}

// fallDamageMultiplier is the damage multiplier Block.fallOn passes (hay and honey 0.2, beds 0.5, slime and
// powder snow none).
func fallDamageMultiplier(b world.Block) float64 {
	switch b.(type) {
	case block.HayBale:
		return float64(float32(0.2))
	case block.Bed:
		return 0.5
	case block.Slime:
		return 0
	}
	if isHoney(b) {
		return float64(float32(0.2))
	}
	if name, _ := b.EncodeBlock(); strings.HasSuffix(name, "powder_snow") {
		return 0
	}
	return 1
}

// causeFallDamage is LivingEntity.causeFallDamage and calculateFallDamage.
func (m *Mob) causeFallDamage(dist, mul float64) {
	if f, ok := m.s.b.(FallListener); ok {
		f.OnFall(m, dist)
	}
	if mul == 0 {
		return
	}
	power := dist + 1e-6 - m.Attr(AttrSafeFallDistance)
	if e, ok := m.Effect(effect.JumpBoost); ok {
		power -= float64(e.Level())
	}
	dmg := math.Floor(power * mul * m.Attr(AttrFallDamageMultiplier))
	if dmg > 0 {
		m.Hurt(dmg, entity.FallDamageSource{})
	}
}

// applyEffectsFromBlocks is Entity.applyEffectsFromBlocks: the block stood on, then the blocks the mob is in.
func (m *Mob) applyEffectsFromBlocks() {
	s := m.s
	tx := m.Tx()
	if s.onGround {
		p := m.onPos(0.2)
		if st, ok := tx.Block(p).(block.EntityStepper); ok {
			st.EntityStepOn(p, tx, m)
			if m.gone() || m.Dead() {
				return
			}
		}
	}
	bb := m.BBox().Grow(-1e-5)
	moved := m.Position().Sub(s.old)
	for pos := range cube.Range3D(cube.PosFromVec3(bb.Min()), cube.PosFromVec3(bb.Max())) {
		b := tx.Block(pos)
		switch b.(type) {
		case block.Air:
		case block.Cobweb:
			if c, ok := s.b.(CobwebIgnorer); ok && c.IgnoresCobwebs() {
				continue
			}
			m.ResetFallDistance()
			s.stuck = mgl64.Vec3{0.25, float64(float32(0.05)), 0.25}
		case block.Cactus:
			m.Hurt(1, block.DamageSource{Block: b})
		default:
			if name, _ := b.EncodeBlock(); name == "minecraft:powder_snow" {
				s.inPowderSnow = true
				if math.Abs(moved[0]) > 0 || math.Abs(moved[2]) > 0 {
					s.stuck = mgl64.Vec3{float64(float32(0.9)), 1.5, float64(float32(0.9))}
				}
				continue
			}
			if in, ok := b.(block.EntityInsider); ok {
				in.EntityInside(pos, tx, m)
			}
		}
		if _, liquid := b.(world.Liquid); !liquid {
			if l, ok := tx.Liquid(pos); ok {
				if in, ok := l.(block.EntityInsider); ok {
					in.EntityInside(pos, tx, m)
				}
			}
		}
		if m.gone() || m.Dead() {
			return
		}
	}
	if m.inRain() && m.OnFireDuration() > 0 {
		m.Extinguish()
	}
}

// mthEqual is Mth.equal: within 1e-5.
func mthEqual(a, b float64) bool { return math.Abs(b-a) < 1e-5 }

// sinTable is Mth's sine table.
var sinTable = func() [65536]float32 {
	var t [65536]float32
	for i := range t {
		t[i] = float32(math.Sin(float64(i) * math.Pi * 2 / 65536))
	}
	return t
}()

// mthSin and mthCos are Mth.sin and Mth.cos.
func mthSin(a float32) float32 { return sinTable[int(int64(a*10430.378)&65535)] }
func mthCos(a float32) float32 { return sinTable[int(int64(a*10430.378+16384)&65535)] }

// isHoney reports whether b is a honey block (Dragonfly has no Go type for it).
func isHoney(b world.Block) bool {
	name, _ := b.EncodeBlock()
	return name == "minecraft:honey_block"
}

// suffocating is BlockStateBase.isSuffocating: a full, solid block (not glass, leaves or the like).
func suffocating(b world.Block) bool { return path.IsSuffocating(b) }
