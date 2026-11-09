//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestExplosionCountsFromMobSide: Explosion::explode measures a mob's
// distance from its side (minus half its width): a ravager (1.95 wide)
// whose feet are 6.5 blocks from a power 3 blast (range 6) is still hit,
// both by an entities-only creeper blast and a block-breaking one.
func TestExplosionCountsFromMobSide(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 12, block.Stone{}) })
	r := w.Spawn("minecraft:ravager", mgl64.Vec3{7, 1, 0.5})
	c := w.Spawn("minecraft:creeper", mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(1)
	health := func() (h float64) {
		r.With(func(m *mob.Mob) { h = m.Health() })
		return h
	}
	h0 := health()
	w.Do(func(tx *world.Tx) {
		cm, _ := c.Mob(tx)
		if d := combat.ExplosionDistance(cm, 6.5); d != 6.5-cm.BBox().Width()/2 {
			t.Errorf("creeper distance %v", d)
		}
		combat.ExplodeEntitiesOnly(tx, cm, 3)
	})
	h1 := health()
	if h1 >= h0 {
		t.Fatalf("entities-only blast missed the ravager (health %v -> %v)", h0, h1)
	}
	w.Tick(12) // hurt immunity
	w.Do(func(tx *world.Tx) {
		if m, ok := r.Mob(tx); ok {
			m.SetPosition(mgl64.Vec3{7, 1, 0.5})
		}
		block.ExplosionConfig{}.Explode(tx, world.BlockExplosionSource{Block: block.TNT{}, Pos: cube.Pos{0, 1, 0}, ExplosionSize: 3})
	})
	if h2 := health(); h2 >= h1 {
		t.Fatalf("block explosion missed the ravager (health %v -> %v)", h1, h2)
	}
}
