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

// TestMobsBeyondSimulationDistanceFreeze: with a viewer in the world, a mob
// further than the tick range (simulation distance) from it does not tick
// at all, while one within it does; moving the viewer close wakes it.
func TestMobsBeyondSimulationDistanceFreeze(t *testing.T) {
	needs(t, true, true, walkers...)
	w := mobtest.New(t, mobtest.Options{})
	const far = 16 * 12
	w.Do(func(tx *world.Tx) {
		tx.World().SetTickRange(4)
		w.Floor(tx, 0, 6, block.Stone{})
		w.Fill(tx, cube.Pos{far - 6, 0, -6}, cube.Pos{far + 6, 0, 6}, block.Stone{})
	})
	near := w.Spawn("minecraft:cow", mgl64.Vec3{0.5, 1, 0.5})
	away := w.Spawn("minecraft:cow", mgl64.Vec3{far + 0.5, 1, 0.5})
	var l *world.Loader
	w.Do(func(tx *world.Tx) {
		l = world.NewLoader(2, tx.World(), world.NopViewer{})
		l.Move(tx, mgl64.Vec3{0, 1, 0})
	})
	ticks := func(r *mobtest.Ref) (n int64) {
		r.With(func(m *mob.Mob) { n = m.Ticks() })
		return n
	}
	n0, a0 := ticks(near), ticks(away)
	w.Tick(40)
	if got := ticks(near) - n0; got != 40 {
		t.Errorf("mob near the viewer ticked %d times in 40 ticks", got)
	}
	if got := ticks(away) - a0; got != 0 {
		t.Errorf("mob %d chunks from the viewer ticked %d times, want 0", far/16, got)
	}
	w.Do(func(tx *world.Tx) { l.Move(tx, mgl64.Vec3{far, 1, 0}) })
	a1 := ticks(away)
	w.Tick(20)
	if got := ticks(away) - a1; got != 20 {
		t.Errorf("mob next to the moved viewer ticked %d times in 20 ticks", got)
	}
	w.Do(func(tx *world.Tx) { l.Close(tx) })
}
