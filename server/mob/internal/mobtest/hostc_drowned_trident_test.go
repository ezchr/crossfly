//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestDrownedThrowsTrident: a ranged drowned (as_ranged_adult: trident,
// ranged_mode, shooter minecraft:thrown_trident) throws tridents at a
// player out of melee range, and a hit hurts (thrown_trident impact
// damage 8).
func TestDrownedThrowsTrident(t *testing.T) {
	needs(t, true, true, "minecraft:behavior.ranged_attack", "minecraft:shooter")
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Fill(tx, cube.Pos{-14, 0, -14}, cube.Pos{14, 14, 14}, block.Stone{})
		w.Clear(tx, cube.Pos{-13, 1, -13}, cube.Pos{13, 13, 13})
	})
	d := w.Spawn("minecraft:drowned", mgl64.Vec3{0.5, 1, -4.5}, mob.SpawnOptions{Event: "minecraft:as_ranged_adult"})
	p := w.AddPlayer("target", mgl64.Vec3{0.5, 1, 3.5})
	p.SetMaxHealth(200)
	w.Tick(1)
	d.With(func(m *mob.Mob) {
		pe, _ := p.Entity(m.Tx())
		m.SetTarget(pe)
	})
	if n, ok := w.TickUntil(300, func(*world.Tx) bool { return projectiles(w, "minecraft:thrown_trident") > 0 }); !ok {
		t.Fatalf("drowned threw no trident in %d ticks:\n%v", n, w.Dump(d))
	}
	if n, ok := w.TickUntil(300, func(tx *world.Tx) bool { return p.HealthTx(tx) < 200 }); !ok {
		t.Fatalf("no trident hit in %d ticks:\n%v", n, w.Dump(d))
	}
	if lost := 200 - p.Health(); lost != 8 {
		t.Errorf("trident hit for %v, want 8", lost)
	}
}
