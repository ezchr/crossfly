//go:build crossfly_full

package locomotion_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/mob"
	cloco "github.com/df-mc/dragonfly/server/mob/component/locomotion"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestFlightSpeeds flies bees, allays, parrots and bats to a point 30
// blocks away (bats wander, float_wander) and measures the average speed
// over 5 s of flight. Ranges are vanilla feel: bee about 2.7 (2..4), allay
// about 1.8 (1.2..3), parrot (BDS FlyMoveControl, movement 0.4) about 4.4
// (2.5..6), bat (float_wander) 2..12 blocks/s.
func TestFlightSpeeds(t *testing.T) {
	mobtest.RequireDefinitions(t)
	for _, c := range []struct {
		id       string
		lo, hi   float64
		directed bool
	}{
		{"minecraft:bee", 2, 4, true},
		{"minecraft:allay", 1.2, 3, true},
		{"minecraft:parrot", 2.5, 6, true},
		{"minecraft:bat", 2, 12, false},
	} {
		t.Run(c.id, func(t *testing.T) {
			w := mobtest.New(t, mobtest.Options{Time: 6000})
			w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 50, block.Stone{}) })
			f := w.Spawn(c.id, mgl64.Vec3{0.5, 6, 0.5})
			// A player nearby keeps the mob from despawning and active.
			w.AddPlayer("watcher", mgl64.Vec3{0.5, 1, 6.5})
			goal := mgl64.Vec3{30.5, 8, 0.5}
			if c.directed {
				f.With(func(m *mob.Mob) {
					keepGoals(m)
				})
			}
			// Let the flight control swap in and the mob take off.
			w.Tick(2)
			if c.directed {
				f.With(func(m *mob.Mob) {
					if _, ok := m.MoveControl().(*cloco.FlyControl); !ok {
						t.Fatalf("move control is %T, want *FlyControl", m.MoveControl())
					}
				})
			}
			prev := f.Pos()
			dist := 0.0
			w.Each(100, func(tx *world.Tx, _ int) bool {
				m, ok := f.Mob(tx)
				if !ok {
					t.Fatal("mob gone")
				}
				if c.directed {
					m.Navigation().MoveTo(m, goal, 1)
					if m.Navigation().Done() {
						m.MoveControl().SetWantedPosition(goal, 1)
					}
				}
				p := m.Position()
				dist += p.Sub(prev).Len()
				prev = p
				return true
			})
			speed := dist / 5
			t.Logf("%s: %.2f blocks/s over 5 s", c.id, speed)
			if speed < c.lo || speed > c.hi {
				t.Errorf("%s flies at %.2f blocks/s, want %.1f..%.1f:\n%v", c.id, speed, c.lo, c.hi, w.Dump(f))
			}
		})
	}
}
