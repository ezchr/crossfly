//go:build crossfly_full

package mobtest_test

import (
	"sync/atomic"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type moveCounter struct {
	world.NopViewer
	moves, motions atomic.Int64
}

func (v *moveCounter) ViewEntityMovement(world.Entity, mgl64.Vec3, cube.Rotation, bool) {
	v.moves.Add(1)
}
func (v *moveCounter) ViewEntityVelocity(world.Entity, mgl64.Vec3) { v.motions.Add(1) }

// TestMovementSentFarFromOrigin: a mob 3000 blocks out that moves a tenth of
// a block a tick is shown every move, as near the origin, and a mob that
// stands still is not shown again and again.
func TestMovementSentFarFromOrigin(t *testing.T) {
	for _, x := range []float64{4.5, 3000.5} {
		w := mobtest.New(t, mobtest.Options{})
		w.Do(func(tx *world.Tx) {
			w.Fill(tx, cube.Pos{int(x) - 8, 0, -8}, cube.Pos{int(x) + 8, 0, 8}, block.Stone{})
		})
		r := w.Spawn("minecraft:cow", mgl64.Vec3{x, 1, 0.5})
		// No goals: it only moves when the test moves it.
		r.With(func(m *mob.Mob) {
			m.Goals().StopAll(m)
			m.Goals().ClearAll()
		})
		v := &moveCounter{}
		w.Do(func(tx *world.Tx) {
			l := world.NewLoader(2, tx.World(), v)
			l.Move(tx, mgl64.Vec3{x, 1, 0})
			l.Load(tx, 100)
		})
		w.Tick(40)
		v.moves.Store(0)
		v.motions.Store(0)
		w.Tick(20)
		if n := v.moves.Load(); n > 0 {
			t.Errorf("x %v: a mob standing still was shown moving %d times in 20 ticks", x, n)
		}
		for i := range 20 {
			r.With(func(m *mob.Mob) { m.SetPosition(mgl64.Vec3{x + float64(i+1)*0.1, 1, 0.5}) })
			w.Tick(1)
		}
		if n := v.moves.Load(); n < 19 {
			t.Errorf("x %v: a mob moving 0.1 a tick for 20 ticks was shown moving %d times", x, n)
		}
	}
}
