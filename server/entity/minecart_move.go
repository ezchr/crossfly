//go:build crossfly_full

package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// This file holds the parts of Java Edition's Entity.move a minecart needs, ported without the epsilons of
// MovementComputer: a minecart braking on a powered rail or nudged by its rider moves by less than 0.001 a tick.

// minecartCollideEpsilon is Java Edition's VoxelShape collision epsilon (Shapes.collide).
const minecartCollideEpsilon = 1e-7

// mthEqual is Java Edition's Mth.equal for doubles.
func mthEqual(a, b float64) bool { return math.Abs(b-a) < float64(float32(1e-5)) }

// minecartMove is Entity.move(MoverType.SELF, delta) for a minecart, followed by the inside-block effects Java
// Edition's old minecart movement runs after every move.
func (b *minecartBehaviour) move(e *Ent, tx *world.Tx, delta mgl64.Vec3) {
	pos := e.data.Pos
	box := minecartBox(pos)
	moved := minecartCollide(tx, box, delta)
	movedLen := moved.LenSqr()
	if movedLen > 1e-7 || delta.LenSqr()-movedLen < 1e-7 {
		pos = pos.Add(moved)
		e.data.Pos = pos
	}
	xCollision, zCollision := !mthEqual(delta[0], moved[0]), !mthEqual(delta[2], moved[2])
	b.horizontalCollision = xCollision || zCollision
	// A minecart is simulated by the server alone, so its ground state is updated by every move, even one without
	// vertical movement.
	verticalCollision := delta[1] != moved[1]
	b.onGround = verticalCollision && delta[1] < 0

	// Entity.checkFallDamage.
	if !b.inWater && moved[1] < 0 {
		b.fallDistance -= moved[1]
	}
	if b.onGround {
		if b.fallDistance > 0 {
			b.fallOn(e, tx, b.fallDistance)
		}
		b.fallDistance = 0
	}
	if b.removed {
		return
	}
	if (math.Abs(delta[1]) > 0 && verticalCollision) || b.horizontalCollision {
		// restituteMovementAfterCollisions: a minecart does not bounce off walls.
		vel := e.data.Vel
		if xCollision {
			vel[0] = -vel[0] * 0
		}
		if zCollision {
			vel[2] = -vel[2] * 0
		}
		if verticalCollision {
			vel[1] = -vel[1] * 0
		}
		e.data.Vel = vel
	}
	f := minecartBlockSpeedFactor(tx, pos)
	e.data.Vel[0] *= f
	e.data.Vel[2] *= f
	b.insideBlocks(e, tx)
}

// minecartBlockSpeedFactor is AbstractMinecart.getBlockSpeedFactor: 1 on rails, otherwise the factor of the block
// the minecart is in or, failing that, the block under it (soul sand slows it down).
func minecartBlockSpeedFactor(tx *world.Tx, pos mgl64.Vec3) float64 {
	here := cube.PosFromVec3(pos)
	if _, ok := railShapeAt(tx, here); ok {
		return 1
	}
	if _, ok := tx.Block(here).(block.SoulSand); ok {
		return float64(float32(0.4))
	}
	below := cube.Pos{here[0], int(math.Floor(pos[1] - float64(float32(0.500001)))), here[2]}
	if _, ok := tx.Block(below).(block.SoulSand); ok {
		return float64(float32(0.4))
	}
	return 1
}

// minecartCollide is Java Edition's Entity.collide for an entity that does not step up and collides with blocks
// only: Y first, then the larger horizontal axis.
func minecartCollide(tx *world.Tx, box cube.BBox, delta mgl64.Vec3) mgl64.Vec3 {
	if delta.LenSqr() == 0 {
		return delta
	}
	boxes := minecartColliders(tx, box.Extend(delta))
	if len(boxes) == 0 {
		return delta
	}
	order := [3]int{1, 0, 2}
	if math.Abs(delta[0]) < math.Abs(delta[2]) {
		order = [3]int{1, 2, 0}
	}
	var resolved mgl64.Vec3
	for _, axis := range order {
		if delta[axis] == 0 {
			continue
		}
		resolved[axis] = collideAxis(boxes, box.Translate(resolved), axis, delta[axis])
	}
	return resolved
}

// minecartColliders returns the collision boxes of the blocks around a box.
func minecartColliders(tx *world.Tx, area cube.BBox) []cube.BBox {
	lo, hi := area.Min(), area.Max()
	minX, minY, minZ := int(math.Floor(lo[0]-minecartCollideEpsilon))-1, int(math.Floor(lo[1]-minecartCollideEpsilon))-1, int(math.Floor(lo[2]-minecartCollideEpsilon))-1
	maxX, maxY, maxZ := int(math.Floor(hi[0]+minecartCollideEpsilon))+1, int(math.Floor(hi[1]+minecartCollideEpsilon))+1, int(math.Floor(hi[2]+minecartCollideEpsilon))+1
	r := tx.Range()
	minY, maxY = max(minY, r[0]), min(maxY, r[1])
	var boxes []cube.BBox
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			for z := minZ; z <= maxZ; z++ {
				pos := cube.Pos{x, y, z}
				for _, bb := range tx.Block(pos).Model().BBox(pos, tx) {
					boxes = append(boxes, bb.Translate(pos.Vec3()))
				}
			}
		}
	}
	return boxes
}

// collideAxis is Shapes.collide along one axis: how far box may move by d before it runs into one of boxes.
func collideAxis(boxes []cube.BBox, box cube.BBox, axis int, d float64) float64 {
	if math.Abs(d) < minecartCollideEpsilon {
		return 0
	}
	lo, hi := box.Min(), box.Max()
	a, c := (axis+1)%3, (axis+2)%3
	for _, other := range boxes {
		olo, ohi := other.Min(), other.Max()
		if !(olo[a] < hi[a]-minecartCollideEpsilon && ohi[a] > lo[a]+minecartCollideEpsilon) ||
			!(olo[c] < hi[c]-minecartCollideEpsilon && ohi[c] > lo[c]+minecartCollideEpsilon) {
			continue
		}
		if d > 0 {
			if dist := olo[axis] - hi[axis]; dist >= -minecartCollideEpsilon {
				d = min(d, dist)
			}
		} else if dist := ohi[axis] - lo[axis]; dist <= minecartCollideEpsilon {
			d = max(d, dist)
		}
		if math.Abs(d) < minecartCollideEpsilon {
			return 0
		}
	}
	return d
}

// Java Edition's float sine table (Mth.SIN) and fast atan2 (Mth.atan2), which minecart rotations and pushes use.
var (
	mthSinTable [65536]float32
	mthAsinTab  [257]float64
	mthCosTab   [257]float64
	mthFracBias = math.Float64frombits(4805340802404319232)
)

func init() {
	for i := range mthSinTable {
		mthSinTable[i] = float32(math.Sin(float64(i) / 10430.378350470453))
	}
	for i := range mthAsinTab {
		v := float64(i) / 256
		asin := math.Asin(v)
		mthCosTab[i] = math.Cos(asin)
		mthAsinTab[i] = asin
	}
}

// mthSin is Mth.sin.
func mthSin(v float64) float32 { return mthSinTable[int(int64(v*10430.378350470453)&65535)] }

// mthCos is Mth.cos.
func mthCos(v float64) float32 { return mthSinTable[int(int64(v*10430.378350470453+16384)&65535)] }

// mthAtan2 is Mth.atan2.
func mthAtan2(y, x float64) float64 {
	d2 := x*x + y*y
	if math.IsNaN(d2) {
		return math.NaN()
	}
	negY := y < 0
	if negY {
		y = -y
	}
	negX := x < 0
	if negX {
		x = -x
	}
	steep := y > x
	if steep {
		x, y = y, x
	}
	rinv := mthFastInvSqrt(d2)
	x *= rinv
	y *= rinv
	yp := mthFracBias + y
	index := int32(math.Float64bits(yp))
	phi, cPhi := mthAsinTab[index], mthCosTab[index]
	sPhi := yp - mthFracBias
	sd := y*cPhi - x*sPhi
	d := (6 + sd*sd) * sd * 0.16666666666666666
	theta := phi + d
	if steep {
		theta = math.Pi/2 - theta
	}
	if negX {
		theta = math.Pi - theta
	}
	if negY {
		theta = -theta
	}
	return theta
}

// mthFastInvSqrt is Mth.fastInvSqrt.
func mthFastInvSqrt(x float64) float64 {
	half := 0.5 * x
	i := int64(math.Float64bits(x))
	i = 6910469410427058090 - (i >> 1)
	x = math.Float64frombits(uint64(i))
	return x * (1.5 - half*x*x)
}

// mthWrapDegrees is Mth.wrapDegrees for floats.
func mthWrapDegrees(a float32) float32 {
	n := float32(math.Mod(float64(a), 360))
	if n >= 180 {
		n -= 360
	}
	if n < -180 {
		n += 360
	}
	return n
}

// horizontalLen is Vec3.horizontalDistance.
func horizontalLen(v mgl64.Vec3) float64 { return math.Sqrt(v[0]*v[0] + v[2]*v[2]) }

// horizontalLenSqr is Vec3.horizontalDistanceSqr.
func horizontalLenSqr(v mgl64.Vec3) float64 { return v[0]*v[0] + v[2]*v[2] }

// clampF is Mth.clamp for doubles.
func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	return min(v, hi)
}
