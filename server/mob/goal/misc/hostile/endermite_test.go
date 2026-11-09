//go:build crossfly_full

package hostile_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestEndermiteLifetime: Endermite::aiStep removes an endermite 2400 ticks
// after it appeared, persistent or not, but not a named one.
func TestEndermiteLifetime(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 4, block.Stone{}) })
	plain := w.Spawn("minecraft:endermite", mgl64.Vec3{0.5, 1, 0.5})
	named := w.Spawn("minecraft:endermite", mgl64.Vec3{2.5, 1, 0.5})
	for _, r := range []*mobtest.Ref{plain, named} {
		r.With(func(m *mob.Mob) {
			m.Goals().ClearAll()
			m.SetPersistent(true)
		})
	}
	named.With(func(m *mob.Mob) { m.SetNameTag("Mite") })
	w.Tick(2390)
	if plain.Gone() {
		t.Fatal("endermite removed before 2400 ticks")
	}
	w.Tick(20)
	if !plain.Gone() {
		t.Fatal("endermite still there after 2400 ticks")
	}
	if named.Gone() {
		t.Fatal("named endermite removed")
	}
}
