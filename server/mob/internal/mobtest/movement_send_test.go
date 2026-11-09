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

// javaMoveCounter is a moveCounter that is a Java Edition viewer.
type javaMoveCounter struct{ moveCounter }

func (*javaMoveCounter) EntityUpdateInterval(world.Entity) int { return 3 }

// TestMovementSentPerEdition: a mob that moves every tick is shown to a
// Bedrock viewer every tick (BDS) and to a Java viewer every third tick, its
// Java update interval (ServerEntity), unless it was pushed (needsSync). Its
// velocity changing by small steps is shown to Java viewers, not to Bedrock
// ones (BDS sends motion only on a change of more than 0.2 a tick).
func TestMovementSentPerEdition(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Fill(tx, cube.Pos{-8, 0, -8}, cube.Pos{40, 0, 8}, block.Stone{})
	})
	r := w.Spawn("minecraft:cow", mgl64.Vec3{0.5, 1, 0.5})
	r.With(func(m *mob.Mob) {
		m.Goals().StopAll(m)
		m.Goals().ClearAll()
	})
	bedrock, java := &moveCounter{}, &javaMoveCounter{}
	w.Do(func(tx *world.Tx) {
		for _, v := range []world.Viewer{bedrock, java} {
			l := world.NewLoader(3, tx.World(), v)
			l.Move(tx, mgl64.Vec3{0.5, 1, 0})
			l.Load(tx, 100)
		}
	})
	w.Tick(40)
	for _, c := range []*moveCounter{bedrock, &java.moveCounter} {
		c.moves.Store(0)
		c.motions.Store(0)
	}
	for i := range 30 {
		r.With(func(m *mob.Mob) {
			m.SetPosition(mgl64.Vec3{0.5 + float64(i+1)*0.1, 1, 0.5})
			m.SetVelocity(mgl64.Vec3{0.1 + float64(i)*0.001, 0, 0})
		})
		w.Tick(1)
	}
	if n := bedrock.moves.Load(); n < 29 {
		t.Errorf("Bedrock viewer: shown %d moves in 30 ticks of moving, want every tick", n)
	}
	if n := java.moves.Load(); n < 9 || n > 11 {
		t.Errorf("Java viewer: shown %d moves in 30 ticks of moving, want every 3rd tick", n)
	}
	if n := java.motions.Load(); n < 9 || n > 11 {
		t.Errorf("Java viewer: shown %d motions in 30 ticks, want every 3rd tick", n)
	}
	if n := bedrock.motions.Load(); n > 1 {
		t.Errorf("Bedrock viewer: shown %d motions for a velocity changing 0.001 a tick, want at most 1", n)
	}
	java.moves.Store(0)
	for i := range 6 {
		r.With(func(m *mob.Mob) {
			m.SetPosition(mgl64.Vec3{3.5 + float64(i+1)*0.1, 1, 0.5})
			m.SyncMovement()
		})
		w.Tick(1)
	}
	if n := java.moves.Load(); n < 6 {
		t.Errorf("Java viewer: a mob pushed every tick was shown %d moves in 6 ticks, want 6", n)
	}
}
