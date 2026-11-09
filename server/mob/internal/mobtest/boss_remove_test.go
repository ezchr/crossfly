//go:build crossfly_full

package mobtest_test

import (
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type barViewer struct {
	world.NopViewer
	player *world.EntityHandle
	mu     sync.Mutex
	shown  map[*world.EntityHandle]bool
}

func (v *barViewer) MobBossBarViewer() *world.EntityHandle { return v.player }
func (v *barViewer) ShowMobBossBar(e world.Entity, _ string, _ float64, _ uint8, _ bool) {
	v.mu.Lock()
	v.shown[e.H()] = true
	v.mu.Unlock()
}
func (v *barViewer) UpdateMobBossBar(world.Entity, float64) {}
func (v *barViewer) HideMobBossBar(e world.Entity) {
	v.mu.Lock()
	delete(v.shown, e.H())
	v.mu.Unlock()
}
func (v *barViewer) count() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return len(v.shown)
}

// TestBossBarHiddenOnRemove: a wither removed without dying (a command, a despawn) must take its
// boss bar with it; Java clients otherwise keep the bar (and the dark sky) for good.
func TestBossBarHiddenOnRemove(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 6, block.Stone{}) })
	p := w.AddPlayer("viewer", mgl64.Vec3{0.5, 1, 0.5})
	r := w.Spawn("minecraft:wither", mgl64.Vec3{0.5, 1, 8.5})
	v := &barViewer{player: p.H(), shown: map[*world.EntityHandle]bool{}}
	var l *world.Loader
	w.Do(func(tx *world.Tx) {
		l = world.NewLoader(2, tx.World(), v)
		l.Move(tx, mgl64.Vec3{0.5, 1, 0.5})
		l.Load(tx, 25)
	})
	defer w.Do(func(tx *world.Tx) { l.Close(tx) })
	w.Tick(25)
	if v.count() != 1 {
		t.Fatalf("boss bar not shown: %d bars", v.count())
	}
	r.With(func(m *mob.Mob) { _ = m.Close() })
	if v.count() != 0 {
		t.Fatal("boss bar still shown after the wither was removed")
	}
}
