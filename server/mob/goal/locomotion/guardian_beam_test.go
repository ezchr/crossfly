//go:build crossfly_full

package locomotion_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	_ "github.com/df-mc/dragonfly/server/mob/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestGuardianBeamDamage: the beam is an attack hit plus a magic hit on top,
// both scaled by difficulty against players: a guardian deals 6 on normal
// and 12 on hard, an elder guardian 8 and 15.
func TestGuardianBeamDamage(t *testing.T) {
	mobtest.RequireDefinitions(t)
	cases := []struct {
		id   string
		diff world.Difficulty
		want float64
	}{
		{"minecraft:guardian", world.DifficultyNormal, 6},
		{"minecraft:guardian", world.DifficultyHard, 12},
		{"minecraft:elder_guardian", world.DifficultyNormal, 8},
		{"minecraft:elder_guardian", world.DifficultyHard, 15},
	}
	for _, c := range cases {
		w := mobtest.New(t, mobtest.Options{Difficulty: c.diff})
		pool(w, 8, 6)
		g := w.Spawn(c.id, mgl64.Vec3{-4.5, 2, 0.5})
		p := w.AddPlayer("target", mgl64.Vec3{4.5, 2, 0.5})
		w.Tick(1)
		var target world.Entity
		w.Do(func(tx *world.Tx) { target, _ = p.Entity(tx) })
		g.With(func(m *mob.Mob) { m.SetTarget(target) })
		n, ok := w.TickUntil(400, func(tx *world.Tx) bool { return p.HealthTx(tx) < 20 })
		if !ok {
			t.Fatalf("%v did not hurt the player in %d ticks:\n%v", c.id, n, w.Dump(g))
		}
		w.Tick(2)
		if got := 20 - p.Health(); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%v beam on difficulty %v dealt %.2f, want %.0f", c.id, c.diff, got, c.want)
		}
	}
}
