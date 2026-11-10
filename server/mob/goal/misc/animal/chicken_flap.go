//go:build crossfly_full

package animal

import "github.com/df-mc/dragonfly/server/mob"

// The chicken's slow fall is hardcoded in BDS: the Chicken constructor adds
// a WingFlapComponent, and chicken.json has no component for it, so it is
// gated by identifier.
const chickenID = "minecraft:chicken"

// wingFlapDrag is MobMovementDrag::tickApplyWingFlapVerticalDrag's factor.
const wingFlapDrag = 0.6

func init() {
	mob.RegisterSystem(mob.System{Name: "animal/chicken_wing_flap", Stage: mob.StageTravel, Order: 30, Tick: chickenWingFlap})
}

// airborneKey keeps whether the chicken was off the ground after its last
// travel, i.e. whether this tick's travel was air travel.
type airborneKey struct{}

// chickenWingFlap is MobMovementDrag::tickApplyWingFlapVerticalDrag, the
// "WingFlapVerticalDrag" system BDS runs after gravity and drag
// (VanillaSystemsRegistration::registerActorTravelSystems) for entities with
// a WingFlapComponent on air travel (AirTravelFlag: the tick started off the
// ground and out of water and lava): a falling chicken keeps 0.6 of its
// downwards speed, so it flutters down at about 0.11 blocks a tick.
func chickenWingFlap(m *mob.Mob) {
	if m.Identifier() != chickenID || m.Riding() {
		return
	}
	p := m.Physics()
	st, ok := m.Data(airborneKey{})
	if !ok {
		st = new(bool)
		m.SetData(airborneKey{}, st)
	}
	airborne := st.(*bool)
	airTravel := *airborne
	*airborne = !p.OnGround
	if !airTravel || p.InWater || p.InLava {
		return
	}
	if vel := m.Vel(); vel[1] < 0 {
		vel[1] *= wingFlapDrag
		m.SetVelocity(vel)
	}
}
