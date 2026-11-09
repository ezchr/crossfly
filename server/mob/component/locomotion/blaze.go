//go:build crossfly_full

package locomotion

import (
	"math"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
)

// blazeState is the hover state of Blaze (mHeightOffset, starting at 0.5,
// and mHeightOffsetUpdateTime). BDS does not save either.
type blazeState struct {
	heightOffset float64
	updateTime   int
}

type blazeKey struct{}

// blazePreTravel is BlazePreTravelSystem -> Blaze::preTravel, run after the
// controls and right before travel: every 100 ticks the blaze picks a new
// height offset (gaussian x 3 + 0.5). With a target its Y velocity is set to
// +0.1 or -0.1 towards the target's eye height (its own eye height plus the
// offset), or 0 within 0.05 of it; without one, a falling blaze in the air
// falls at 0.6 of its speed. Horizontal movement stays navigation.walk's.
func blazePreTravel(m *mob.Mob) {
	if m.Identifier() != "minecraft:blaze" {
		return
	}
	var s *blazeState
	if v, ok := m.Data(blazeKey{}); ok {
		s = v.(*blazeState)
	} else {
		s = &blazeState{heightOffset: 0.5}
		m.SetData(blazeKey{}, s)
	}
	if s.updateTime--; s.updateTime <= 0 {
		s.updateTime = 100
		s.heightOffset = m.Rand().NormFloat64()*3 + 0.5
	}
	v := m.Vel()
	if t, ok := m.Target(); ok {
		ty := sense.EyePos(t)[1]
		sy := m.EyePosition()[1] + s.heightOffset
		switch {
		case math.Abs(ty-sy) <= 0.05:
			v[1] = 0
		case ty <= sy:
			v[1] = -0.1
		default:
			v[1] = 0.1
		}
	} else if !m.OnGround() && v[1] < 0 {
		v[1] *= 0.6
	} else {
		return
	}
	m.SetVelocity(v)
}
