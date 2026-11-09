//go:build crossfly_full

package locomotion_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	_ "github.com/df-mc/dragonfly/server/mob/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestBlazeRisesToTarget: a blaze whose target stands high up flies up
// towards its eye height (Blaze::preTravel, 0.1 a tick).
func TestBlazeRisesToTarget(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 12, block.Stone{})
		w.Fill(tx, cube.Pos{0, 1, 8}, cube.Pos{0, 15, 8}, block.Stone{})
	})
	p := w.AddPlayer("target", mgl64.Vec3{0.5, 16, 8.5})
	p.SetMaxHealth(1000)
	b := w.Spawn("minecraft:blaze", mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(2)
	var target world.Entity
	w.Do(func(tx *world.Tx) { target, _ = p.Entity(tx) })
	b.With(func(m *mob.Mob) {
		m.SetRandSeed(1, 2)
		m.SetTarget(target)
	})
	startY := b.Pos()[1]
	w.Tick(60)
	if rose := b.Pos()[1] - startY; rose < 4 {
		t.Errorf("blaze with a target 15 blocks up rose only %.2f blocks in 60 ticks:\n%v", rose, w.Dump(b))
	}
}

// TestBlazeFallsSlowly: without a target a blaze in the air keeps 0.6 of its
// fall speed every tick, so it sinks far slower than a falling mob.
func TestBlazeFallsSlowly(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 4, block.Stone{}) })
	b := w.Spawn("minecraft:blaze", mgl64.Vec3{0.5, 30, 0.5})
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{3.5, 30, 0.5})
	w.Tick(20)
	blazeDrop, zombieDrop := 30-b.Pos()[1], 30-z.Pos()[1]
	if blazeDrop < 1 || blazeDrop > 5 || zombieDrop < 8 {
		t.Errorf("in 20 ticks a blaze fell %.2f blocks (want 1-5), a zombie %.2f", blazeDrop, zombieDrop)
	}
}
