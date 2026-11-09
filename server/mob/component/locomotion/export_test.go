//go:build crossfly_full

package locomotion

import "github.com/df-mc/dragonfly/server/mob"

// SetMoveMods sets m's movement modifiers as the friction_modifier,
// air_drag_modifier and bounciness components would (tests).
func SetMoveMods(m *mob.Mob, friction, airDrag, bounce float64) {
	s := modsOf(m)
	s.friction, s.airDrag, s.bounce = friction, airDrag, bounce
	s.set++
}

// Bribed reports minecraft:bribeable's state (tests).
func Bribed(m *mob.Mob) bool {
	c, ok := mob.ComponentOf[*bribeable](m)
	return ok && c.Bribed()
}

// DryingRemaining returns the drying_out_timer ticks left (tests).
func DryingRemaining(m *mob.Mob) (int, bool) {
	c, ok := mob.ComponentOf[*dryingOutTimer](m)
	if !ok {
		return 0, false
	}
	return c.Remaining(), true
}
