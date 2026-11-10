//go:build crossfly_full

package hostile

import (
	"math"
	"testing"

	"github.com/go-gl/mathgl/mgl64"
)

// TestBreezeJumpVector: LongJumpUtil.calculateJumpVectorForAngle (no
// collision check): the arc (no drag) passes the point half a block short of
// the target, the speed stays under the limit, out-of-reach targets fail.
func TestBreezeJumpVector(t *testing.T) {
	const g = 0.08
	maxV := float64(float32(breezeMaxJumpVelocity)) * BreezeFollowRange // 1.4
	from := mgl64.Vec3{0, 64, 0}
	target := mgl64.Vec3{8.5, 64, 0.5}
	for _, a := range breezeJumpAngles {
		v, ok := breezeJumpVector(from, target, maxV, a, g)
		if !ok {
			continue
		}
		if v.Len()/0.95 > maxV+1e-9 {
			t.Errorf("angle %d: speed %v over %v", a, v.Len()/0.95, maxV)
		}
		// Undo the 0.95 and follow the parabola to the aim point.
		u := v.Mul(1 / float64(float32(0.95)))
		aim := target.Sub(mgl64.Vec3{target[0], 0, target[2]}.Normalize().Mul(0.5))
		d := aim.Sub(from)
		r := math.Hypot(d[0], d[2])
		vr := math.Hypot(u[0], u[2])
		tt := r / vr
		if y := u[1]*tt - g*tt*tt/2; math.Abs(y-d[1]) > 1e-6 {
			t.Errorf("angle %d: arc height %v at the aim, want %v", a, y, d[1])
		}
	}
	if _, ok := breezeJumpVector(from, mgl64.Vec3{60, 64, 0}, maxV, 40, g); ok {
		t.Errorf("a jump of 60 blocks is within reach")
	}
	if _, ok := breezeJumpVector(from, mgl64.Vec3{1, 90, 0}, maxV, 40, g); ok {
		t.Errorf("a jump far up at a low angle has a solution")
	}
}

// TestBreezeTargets: who breezes attack, hurt and deflect.
func TestBreezeTargets(t *testing.T) {
	if !BreezeCanAttack("minecraft:player") || !BreezeCanAttack("minecraft:iron_golem") || BreezeCanAttack("minecraft:zombie") {
		t.Errorf("breeze targets")
	}
	if !BreezeDeflects("minecraft:arrow") || BreezeDeflects("minecraft:wind_charge") {
		t.Errorf("breeze deflection")
	}
	if !BreezeInvulnerableTo("minecraft:breeze") {
		t.Errorf("breezes hurt each other")
	}
}
