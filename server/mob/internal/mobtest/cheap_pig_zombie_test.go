//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/mob"
	ccombat "github.com/df-mc/dragonfly/server/mob/component/combat"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestPigTurnsIntoZombifiedPiglin: pig.json transforms the pig struck by
// lightning into "minecraft:pig_zombie", which BDS resolves to the
// zombified piglin (the zombie_pigman actor mapping's alternate name).
func TestPigTurnsIntoZombifiedPiglin(t *testing.T) {
	if got := ccombat.ActorID("minecraft:pig_zombie"); got != "minecraft:zombie_pigman" {
		t.Errorf("ActorID(pig_zombie) = %v", got)
	}
	if got := ccombat.ActorID("minecraft:pig"); got != "minecraft:pig" {
		t.Errorf("ActorID(pig) = %v", got)
	}
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 6, block.Stone{}) })
	p := w.Spawn("minecraft:pig", mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(2)
	p.With(func(m *mob.Mob) { m.Fire("become_zombie", nil) })
	if _, ok := w.TickUntil(40, func(*world.Tx) bool { return len(w.Mobs("minecraft:zombie_pigman")) == 1 }); !ok {
		t.Fatalf("no zombified piglin after the pig_transform delay: %v", w.Dump(p))
	}
	w.Tick(1)
	if !p.Gone() {
		t.Errorf("the pig is still there")
	}
}
