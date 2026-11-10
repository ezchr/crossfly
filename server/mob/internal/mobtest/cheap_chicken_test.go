//go:build crossfly_full

package mobtest_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestChickenSlowFall: MobMovementDrag::tickApplyWingFlapVerticalDrag keeps
// 0.6 of a falling chicken's downwards speed after gravity and drag, so it
// falls at (v - 0.08) x 0.98 x 0.6, settling at -0.04704/0.412 blocks a
// tick, while a pig keeps speeding up.
func TestChickenSlowFall(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 6, block.Stone{}) })
	c := w.Spawn("minecraft:chicken", mgl64.Vec3{0.5, 60, 0.5})
	p := w.Spawn("minecraft:pig", mgl64.Vec3{3.5, 60, 0.5})
	w.Tick(20)
	var cv, pv float64
	var cy float64
	c.With(func(m *mob.Mob) { cv, cy = m.Vel()[1], m.Pos()[1] })
	p.With(func(m *mob.Mob) { pv = m.Vel()[1] })
	if want := -0.04704 / 0.412; math.Abs(cv-want) > 1e-3 {
		t.Errorf("chicken falls at %v blocks a tick, want %v", cv, want)
	}
	if pv > -0.5 {
		t.Errorf("pig falls at %v blocks a tick, want faster than -0.5", pv)
	}
	if cy < 56 {
		t.Errorf("chicken fell to y %v in 20 ticks, want above 56", cy)
	}
	// On the ground the drag stops: it lands and stands still.
	if _, ok := w.TickUntil(800, func(tx *world.Tx) bool {
		m, _ := c.Mob(tx)
		return m.OnGround()
	}); !ok {
		t.Fatalf("chicken never landed: %v", w.Dump(c))
	}
}
