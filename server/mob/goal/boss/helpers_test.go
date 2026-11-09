//go:build crossfly_full

package boss_test

import (
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	_ "github.com/df-mc/dragonfly/server/mob/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func setup(t *testing.T, opts mobtest.Options, keys ...string) *mobtest.World {
	t.Helper()
	mobtest.Require(t, keys...)
	mobtest.RequireDefinitions(t)
	mobtest.RequireFilters(t)
	w := mobtest.New(t, opts)
	w.Do(func(tx *world.Tx) { w.Arena(tx, 24, 4) })
	return w
}

func mobOf(t *testing.T, tx *world.Tx, r *mobtest.Ref) *mob.Mob {
	t.Helper()
	m, ok := r.Mob(tx)
	if !ok {
		t.Fatal("mob gone")
	}
	return m
}

// with runs f on the mob in a transaction.
func with(t *testing.T, w *mobtest.World, r *mobtest.Ref, f func(tx *world.Tx, m *mob.Mob)) {
	t.Helper()
	w.Do(func(tx *world.Tx) { f(tx, mobOf(t, tx, r)) })
}

// barViewer is a world.Viewer showing boss bars, at a test player.
type barViewer struct {
	world.NopViewer
	mu     sync.Mutex
	player *world.EntityHandle
	shown  map[*world.EntityHandle]float64
	shows  int
	hides  int
	name   string
	colour uint8
}

func (v *barViewer) MobBossBarViewer() *world.EntityHandle { return v.player }
func (v *barViewer) ShowMobBossBar(e world.Entity, name string, health float64, colour uint8, _ bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.shown == nil {
		v.shown = map[*world.EntityHandle]float64{}
	}
	v.shown[e.H()] = health
	v.shows++
	v.name, v.colour = name, colour
}
func (v *barViewer) UpdateMobBossBar(e world.Entity, health float64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.shown[e.H()] = health
}
func (v *barViewer) HideMobBossBar(e world.Entity) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.shown, e.H())
	v.hides++
}

func (v *barViewer) bar(h *world.EntityHandle) (float64, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	f, ok := v.shown[h]
	return f, ok
}

// addViewer adds a chunk viewer at p's position.
func addViewer(t *testing.T, w *mobtest.World, p *mobtest.Player) (*barViewer, *world.Loader) {
	t.Helper()
	v := &barViewer{}
	var l *world.Loader
	w.Do(func(tx *world.Tx) {
		e, ok := p.Entity(tx)
		if !ok {
			t.Fatal("no player")
		}
		v.player = e.H()
		l = world.NewLoader(4, tx.World(), v)
		l.Move(tx, e.Position())
		l.Load(tx, 200)
	})
	t.Cleanup(func() { w.Do(func(tx *world.Tx) { l.Close(tx) }) })
	return v, l
}

func near(a, b mgl64.Vec3, d float64) bool { return a.Sub(b).Len() <= d }
