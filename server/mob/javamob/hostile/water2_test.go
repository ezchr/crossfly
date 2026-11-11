//go:build crossfly_full

package hostile

import (
	"testing"

	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/go-gl/mathgl/mgl64"
)

// TestDolphinSwims: a dolphin swims about a deep pool on its smooth swimming
// controls, keeps its 4800 ticks of air under water and stays wet.
func TestDolphinSwims(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	pool(w, 8, 6)
	d := spawnJ(t, w, dolphinID, mgl64.Vec3{0.5, 2, 0.5})
	start := positionOf(t, w, d)
	moved := 0.0
	for i := 0; i < 30; i++ {
		w.Tick(10)
		p := positionOf(t, w, d)
		moved = max(moved, p.Sub(start).Len())
	}
	if moved < 2 {
		t.Errorf("dolphin moved %.2f blocks in 300 ticks, want it to swim", moved)
	}
	withJ(t, w, d, func(m *javamob.Mob) {
		if m.Health() != m.MaxHealth() {
			t.Errorf("dolphin hurt in the water: %v", m.Health())
		}
		if dl := dolphinOf(m.Mob); dl.Air() < 4800-300 || dl.Moistness() < dolphinMaxMoistness {
			t.Errorf("air %d moistness %d after 300 ticks in water", dl.Air(), dl.Moistness())
		}
	})
}
