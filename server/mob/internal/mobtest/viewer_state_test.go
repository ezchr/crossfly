//go:build crossfly_full

package mobtest_test

import (
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type stateViewer struct {
	world.NopViewer
	mu   sync.Mutex
	seen []string
}

func (v *stateViewer) ViewEntityState(e world.Entity) {
	m, ok := e.(*mob.Mob)
	if !ok {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	s := "untamed"
	if m.Tamed() {
		s = "tamed"
	}
	if m.Sitting() {
		s += "+sitting"
	}
	v.seen = append(v.seen, s)
}

func (v *stateViewer) last() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.seen) == 0 {
		return ""
	}
	return v.seen[len(v.seen)-1]
}

// TestTameAndSitReachViewers: feeding a wolf until it is tame and making it sit must show the new
// state to viewers (Java draws the collar and the sitting pose only from what it is sent).
func TestTameAndSitReachViewers(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 6, block.Stone{}) })
	r := w.Spawn("minecraft:wolf", mgl64.Vec3{0.5, 1, 2.5}, mob.SpawnOptions{Event: "minecraft:entity_spawned"})
	p := w.AddPlayer("owner", mgl64.Vec3{0.5, 1, 0.5})
	v := &stateViewer{}
	var l *world.Loader
	w.Do(func(tx *world.Tx) {
		l = world.NewLoader(2, tx.World(), v)
		l.Move(tx, mgl64.Vec3{0.5, 1, 0.5})
		l.Load(tx, 25)
	})
	defer w.Do(func(tx *world.Tx) { l.Close(tx) })
	w.Tick(2)
	for i := 0; i < 40; i++ {
		tamed := false
		r.With(func(m *mob.Mob) { tamed = m.Tamed() })
		if tamed {
			break
		}
		p.Interact(r, item.NewStack(item.Bone{}, 1))
		w.Tick(1)
	}
	// A wolf sits down as it is tamed.
	if got := v.last(); got != "tamed+sitting" {
		t.Fatalf("after taming viewers last saw %q (all %v)", got, v.seen)
	}
	p.Interact(r, item.Stack{})
	w.Tick(1)
	if got := v.last(); got != "tamed" {
		t.Fatalf("after standing up viewers last saw %q", got)
	}
}
