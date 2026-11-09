//go:build crossfly_full

package physics

import (
	"math"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/go-gl/mathgl/mgl64"
)

// KnockBack is Mob.KnockBack (installed with mob.SetKnockBack), after BDS
// Mob::knockback and KnockbackRules:
//   - a mob with knockback_resistance >= 1 is not moved at all;
//   - otherwise its velocity is halved, then pushed away from src by force
//     and up by height, both scaled by 1 - knockback_resistance;
//   - the upwards velocity is capped at height (the heightCap argument,
//     which Dragonfly callers don't pass separately).
//
// force and height are Dragonfly's values (0.45 / 0.3608 for a melee hit):
// they are taken as the impulse itself, so a hit moves a mob as far as it
// moves a player in Dragonfly.
func KnockBack(m *mob.Mob, src mgl64.Vec3, force, height float64) {
	res := m.Attr(mob.AttrKnockbackResistance)
	if res >= 1 {
		return
	}
	k := math.Max(1-res, 0)
	d := m.Pos().Sub(src)
	d[1] = 0
	if l := d.Len(); l >= 1e-4 {
		d = d.Mul(1 / l)
	} else {
		d = mgl64.Vec3{}
	}
	v := limitVelocity(m.Vel()).Mul(0.5)
	v = v.Add(mgl64.Vec3{d[0] * force * k, height * k, d[2] * force * k})
	if v[1] > height {
		v[1] = height
	}
	m.SetVelocity(v)
}
