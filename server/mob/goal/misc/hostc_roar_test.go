//go:build crossfly_full

package misc_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// TestRavagerRoarTargets: KnockbackRoarGoal::tick searches the ravager's
// box grown by knockback_range (a zombie in the box corner, 5.5 blocks off,
// is hit) and hurts only what it knocks back (another ravager fails
// knockback_filters, so the damage_filters it passes do not matter).
func TestRavagerRoarTargets(t *testing.T) {
	w := arena(t, 12)
	r := w.Spawn("minecraft:ravager", mgl64.Vec3{0.5, 1, 0.5})
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{4.4, 1, 4.4})
	r2 := w.Spawn("minecraft:ravager", mgl64.Vec3{-3, 1, 0.5})
	w.Tick(2)
	var zh, rh float64
	z.With(func(m *mob.Mob) { zh = m.Health() })
	r2.With(func(m *mob.Mob) { rh = m.Health() })
	r.With(func(m *mob.Mob) { m.Fire("minecraft:start_roar", nil) })
	if _, ok := flagSeen(w, r, 3, protocol.EntityDataFlagRoaring); !ok {
		t.Fatalf("no roaring flag: %v", w.Dump(r))
	}
	w.Tick(12)
	z.With(func(m *mob.Mob) {
		if zh-m.Health() != 6 {
			t.Errorf("zombie in the box corner lost %v health, want 6", zh-m.Health())
		}
	})
	r2.With(func(m *mob.Mob) {
		if m.Health() != rh {
			t.Errorf("other ravager hurt by the roar (%v -> %v)", rh, m.Health())
		}
	})
}
