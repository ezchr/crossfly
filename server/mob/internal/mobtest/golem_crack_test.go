//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// golemCrackViewer counts the iron golem crack sounds it hears.
type golemCrackViewer struct {
	world.NopViewer
	cracks int
}

func (v *golemCrackViewer) ViewSound(_ mgl64.Vec3, s world.Sound) {
	if c, ok := s.(sound.Custom); ok && c.Name == "mob.irongolem.crack" {
		v.cracks++
	}
}

func TestIronGolemCracksAtQuarterHealthSteps(t *testing.T) {
	needs(t, true, false)
	w := mobtest.New(t, mobtest.Options{})
	viewer := &golemCrackViewer{}
	var loader *world.Loader
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 4, block.Stone{})
		loader = world.NewLoader(2, w.W, viewer)
		loader.Move(tx, mgl64.Vec3{0, 1, 0})
		loader.Load(tx, 25)
	})
	defer w.Do(func(tx *world.Tx) { loader.Close(tx) })
	golem := w.Spawn("minecraft:iron_golem", mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(1)
	hit := func(dmg float64) {
		golem.With(func(m *mob.Mob) { m.Hurt(dmg, entity.AttackDamageSource{}) })
		w.Tick(20) // past the hurt immunity
	}
	for i, step := range []struct {
		dmg    float64
		cracks int
	}{
		{20, 0}, // 100 -> 80
		{6, 1},  // 80 -> 74: below 75
		{20, 1}, // 74 -> 54
		{5, 2},  // 54 -> 49: below 50
		{30, 3}, // 49 -> 19: below 25
	} {
		hit(step.dmg)
		if viewer.cracks != step.cracks {
			var h float64
			golem.With(func(m *mob.Mob) { h = m.Health() })
			t.Fatalf("hit %d (%v damage, health now %v): %d crack sounds, want %d", i, step.dmg, h, viewer.cracks, step.cracks)
		}
	}
	// Other mobs never crack.
	zombie := w.Spawn("minecraft:zombie", mgl64.Vec3{2.5, 1, 0.5})
	w.Tick(1)
	zombie.With(func(m *mob.Mob) { m.Hurt(15, entity.AttackDamageSource{}) })
	if viewer.cracks != 3 {
		t.Fatalf("a zombie cracked: %d", viewer.cracks)
	}
}
