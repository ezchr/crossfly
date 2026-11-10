//go:build crossfly_full

package nether

import (
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
)

// The shulker bullet (ShulkerBullet): it flies one axis at a time towards
// its target at 0.15 blocks a tick (each tick 1.025x faster, eased in by
// 0.2), picks a new axis every 10-50 ticks or when it lines up with the
// target or meets a block, falls (gravity 0.04) without a target, passes
// through blocks (noPhysics) but stops at the first one it would hit, and
// is destroyed by any hit.

// bulletSpeed is ShulkerBullet.SPEED.
const bulletSpeed = 0.15

// tickBullet is ShulkerBullet.tick.
func (p *javaProjectile) tickBullet(e *entity.Ent, tx *world.Tx) {
	// super.tick: Projectile.tick (checkLeftOwner) and Entity.tick
	// (tickCount++) run first here.
	p.checkLeftOwner(tx, e, p.data.Vel)
	p.ticks++
	if difficultyID(tx) == 0 {
		// checkDespawn: no bullets on peaceful.
		p.discard(e)
		return
	}
	target := p.targetEntity(tx)
	vel := p.data.Vel
	if target != nil && targetAlive(target) {
		for i := range p.targetDelta {
			p.targetDelta[i] = clamp(p.targetDelta[i]*1.025, -1, 1)
		}
		vel = vel.Add(p.targetDelta.Sub(vel).Mul(0.2))
	} else {
		vel[1] -= 0.04
	}
	pos := p.data.Pos
	hit, ok := p.hitResult(tx, e, pos, vel)
	p.move(e, tx, pos.Add(vel), vel)
	if p.belowWorld(tx) {
		p.discard(e)
		return
	}
	if ok {
		p.hitOrDeflect(e, tx, hit)
		if p.removed {
			return
		}
	}
	if target == nil {
		return
	}
	if p.flightSteps > 0 {
		p.flightSteps--
		if p.flightSteps == 0 {
			p.selectNextMoveDirection(tx, p.data.Pos, p.moveAxis(), target)
		}
	}
	if p.hasMoveDir {
		cur := cube.PosFromVec3(p.data.Pos)
		axis := p.moveDir.Axis()
		if canStandOn(tx, cur.Side(p.moveDir)) {
			p.selectNextMoveDirection(tx, p.data.Pos, &axis, target)
		} else {
			tp := cube.PosFromVec3(target.Position())
			if axis == cube.X && cur.X() == tp.X() || axis == cube.Z && cur.Z() == tp.Z() || axis == cube.Y && cur.Y() == tp.Y() {
				p.selectNextMoveDirection(tx, p.data.Pos, &axis, target)
			}
		}
	}
}

// moveAxis is the axis of the current direction, nil without one.
func (p *javaProjectile) moveAxis() *cube.Axis {
	if !p.hasMoveDir {
		return nil
	}
	a := p.moveDir.Axis()
	return &a
}

// targetEntity is the bullet's target in tx (finalTarget), dropping it when
// gone.
func (p *javaProjectile) targetEntity(tx *world.Tx) world.Entity {
	if p.target == nil {
		return nil
	}
	t, ok := p.target.Entity(tx)
	if !ok {
		p.target = nil
		return nil
	}
	return t
}

// targetAlive: alive and not a spectator.
func targetAlive(t world.Entity) bool {
	if l, ok := t.(entity.Living); ok && l.Dead() {
		return false
	}
	if g, ok := t.(interface{ GameMode() world.GameMode }); ok && !g.GameMode().Visible() {
		return false
	}
	return true
}

// canStandOn is Level.loadedAndEntityCanStandOn: the block's top face is
// full.
func canStandOn(tx *world.Tx, pos cube.Pos) bool {
	return tx.Block(pos).Model().FaceSolid(pos, cube.FaceUp, tx)
}

// emptyBlock is Level.isEmptyBlock: air.
func emptyBlock(tx *world.Tx, pos cube.Pos) bool {
	_, ok := tx.Block(pos).(block.Air)
	return ok
}

// selectNextMoveDirection is ShulkerBullet.selectNextMoveDirection: a step
// along an axis other than avoid that brings the bullet closer to the
// target (half its height up), through air, or a random direction; then
// the speed towards that point and 10-50 ticks before choosing again.
func (p *javaProjectile) selectNextMoveDirection(tx *world.Tx, pos mgl64.Vec3, avoid *cube.Axis, target world.Entity) {
	yOffset := 0.5
	var tp cube.Pos
	if target == nil {
		tp = cube.PosFromVec3(pos).Side(cube.FaceDown)
	} else {
		yOffset = target.H().Type().BBox(target).Height() * 0.5
		t := target.Position()
		tp = cube.PosFromVec3(mgl64.Vec3{t[0], t[1] + yOffset, t[2]})
	}
	tx0, ty0, tz0 := float64(tp.X())+0.5, float64(tp.Y())+yOffset, float64(tp.Z())+0.5
	p.hasMoveDir = false
	if tp.Vec3Centre().Sub(pos).LenSqr() >= 4 {
		cur := cube.PosFromVec3(pos)
		var options []cube.Face
		if avoid == nil || *avoid != cube.X {
			if cur.X() < tp.X() && emptyBlock(tx, cur.Side(cube.FaceEast)) {
				options = append(options, cube.FaceEast)
			} else if cur.X() > tp.X() && emptyBlock(tx, cur.Side(cube.FaceWest)) {
				options = append(options, cube.FaceWest)
			}
		}
		if avoid == nil || *avoid != cube.Y {
			if cur.Y() < tp.Y() && emptyBlock(tx, cur.Side(cube.FaceUp)) {
				options = append(options, cube.FaceUp)
			} else if cur.Y() > tp.Y() && emptyBlock(tx, cur.Side(cube.FaceDown)) {
				options = append(options, cube.FaceDown)
			}
		}
		if avoid == nil || *avoid != cube.Z {
			if cur.Z() < tp.Z() && emptyBlock(tx, cur.Side(cube.FaceSouth)) {
				options = append(options, cube.FaceSouth)
			} else if cur.Z() > tp.Z() && emptyBlock(tx, cur.Side(cube.FaceNorth)) {
				options = append(options, cube.FaceNorth)
			}
		}
		sel := cube.Faces()[p.r.IntN(6)]
		if len(options) == 0 {
			for attempts := 5; !emptyBlock(tx, cur.Side(sel)) && attempts > 0; attempts-- {
				sel = cube.Faces()[p.r.IntN(6)]
			}
		} else {
			sel = options[p.r.IntN(len(options))]
		}
		step := cube.Pos{}.Side(sel).Vec3()
		tx0, ty0, tz0 = pos[0]+step[0], pos[1]+step[1], pos[2]+step[2]
		p.moveDir, p.hasMoveDir = sel, true
	}
	d := mgl64.Vec3{tx0 - pos[0], ty0 - pos[1], tz0 - pos[2]}
	if l := d.Len(); l == 0 {
		p.targetDelta = mgl64.Vec3{}
	} else {
		p.targetDelta = d.Mul(bulletSpeed / l)
	}
	p.flightSteps = 10 + p.r.IntN(5)*10
}

// bulletHit is ShulkerBullet.onHitEntity/onHitBlock + destroy: 4 damage
// (mob_projectile) and Levitation for 10 s on the entity hit; particles
// and the hit sound on a block.
func (p *javaProjectile) bulletHit(e *entity.Ent, tx *world.Tx, hit trace.Result) {
	switch r := hit.(type) {
	case entityHit:
		owner := p.ownerEntity(tx)
		src := entity.ProjectileDamageSource{Projectile: e, Owner: owner}
		if hurt(r.e, 4, src) {
			if l, ok := r.e.(entity.Living); ok && !l.Dead() {
				l.AddEffect(effect.New(effect.Levitation, 1, 200*time.Second/20))
			}
		}
	case trace.BlockResult:
		tx.AddParticle(p.data.Pos, particle.Named{Bedrock: "minecraft:large_explosion", Java: "explosion"})
		tx.PlaySound(p.data.Pos, entitySound{event: "bullet.hit", id: ShulkerBulletType.id})
	}
	p.discard(e)
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }
