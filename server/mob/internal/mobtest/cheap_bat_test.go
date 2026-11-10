//go:build crossfly_full

package mobtest_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/animal"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestBatHangsFromCeiling: a bat starts resting (Bat constructor) and keeps
// hanging under a ceiling, still, its top at the ceiling
// (Bat::postNormalTick); a player within 4 blocks wakes it
// (Bat::newServerAiStep) with the takeoff sound; a bat in the open wakes at
// once, and a hit wakes a resting one (Bat::_hurt).
func TestBatHangsFromCeiling(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	v := &shakeViewer{}
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 12, block.Stone{})
		w.Fill(tx, cube.Pos{-4, 6, -4}, cube.Pos{4, 6, 4}, block.Stone{})
		l := world.NewLoader(4, tx.World(), v)
		l.Move(tx, mgl64.Vec3{0.5, 1, 0.5})
		l.Load(tx, 1000)
	})
	hang := w.Spawn("minecraft:bat", mgl64.Vec3{0.5, 5.3, 0.5})
	open := w.Spawn("minecraft:bat", mgl64.Vec3{9.5, 8, 9.5})
	w.Tick(40)
	hang.With(func(m *mob.Mob) {
		if !animal.BatResting(m) {
			t.Fatalf("bat under the ceiling not resting: %v", w.Dump(hang))
		}
		if y := m.Pos()[1]; math.Abs(y-5.1) > 1e-6 {
			t.Errorf("resting bat at y %v, want 5.1 (ceiling 6 - height 0.9)", y)
		}
		if p := m.Pos(); math.Abs(p[0]-0.5) > 1e-6 || math.Abs(p[2]-0.5) > 1e-6 {
			t.Errorf("resting bat moved to %v", p)
		}
	})
	open.With(func(m *mob.Mob) {
		if animal.BatResting(m) {
			t.Errorf("bat without a ceiling still resting")
		}
	})
	v.sounds = nil
	pl := w.AddPlayer("p", mgl64.Vec3{0.5, 1, 2.5})
	w.Tick(1)
	hang.With(func(m *mob.Mob) {
		if animal.BatResting(m) {
			t.Errorf("bat still resting with a player %v blocks from its eyes", m.Position().Sub(pl.Pos().Add(mgl64.Vec3{0, 1.62})).Len())
		}
	})
	w.Tick(1)
	takeoff := false
	for _, s := range v.sounds {
		takeoff = takeoff || s == "takeoff"
	}
	if !takeoff {
		t.Errorf("no takeoff sound: %v", v.sounds)
	}
	// A hit wakes a resting bat.
	hit := w.Spawn("minecraft:bat", mgl64.Vec3{-3.5, 5.3, -3.5})
	w.Tick(1)
	hit.With(func(m *mob.Mob) {
		if !animal.BatResting(m) {
			t.Fatalf("second bat not resting")
		}
		m.Hurt(1, effect.InstantDamageSource{})
		if animal.BatResting(m) {
			t.Errorf("hurt bat still resting")
		}
	})
}
