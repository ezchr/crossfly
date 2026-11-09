//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// TestHoglinFleesOnlyWhenOutnumbered: a hoglin avoids hunting piglins
// (has_target) only while their group_size is larger than its own
// (avoid_mob_type check_if_outnumbered, AvoidMobTypeGoal::
// findNearestEntityToAvoid): one piglin is no reason to run, two are.
func TestHoglinFleesOnlyWhenOutnumbered(t *testing.T) {
	needs(t, true, true, append(walkers, "minecraft:behavior.avoid_mob_type", "minecraft:group_size")...)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 24, block.Stone{}) })
	p := w.AddPlayer("p", mgl64.Vec3{12.5, 1, 0.5})
	h := w.Spawn("minecraft:hoglin", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "spawn_adult"})
	piglin := func(pos mgl64.Vec3) *mobtest.Ref {
		return w.Spawn("minecraft:piglin", pos, mob.SpawnOptions{Event: "spawn_adult_melee_no_hunting"})
	}
	pigs := []*mobtest.Ref{piglin(mgl64.Vec3{4.5, 1, 0.5})}
	const key = "minecraft:behavior.avoid_mob_type"
	avoiding := func(tx *world.Tx) bool {
		for _, r := range pigs {
			// Keep the piglins hunting so has_target holds.
			pm, ok := r.Mob(tx)
			pe, ok2 := p.Entity(tx)
			if ok && ok2 {
				if _, has := pm.Target(); !has {
					pm.SetTarget(pe)
				}
			}
		}
		m, _ := h.Mob(tx)
		return m.Goals().Running(key)
	}
	if n, ok := w.TickUntil(60, avoiding); ok {
		t.Fatalf("hoglin flees a single piglin after %d ticks:\n%v", n, w.Dump(h))
	}
	pigs = append(pigs, piglin(mgl64.Vec3{4.5, 1, 2.5}))
	if n, ok := w.TickUntil(60, avoiding); !ok {
		t.Fatalf("hoglin does not flee two piglins after %d ticks:\n%v", n, w.Dump(h))
	}
	h.With(func(m *mob.Mob) {
		if !m.ActorFlag(protocol.EntityDataFlagAvoidingMobs) {
			t.Error("avoiding_mobs flag not set while fleeing")
		}
		if _, ok := m.Target(); ok {
			t.Error("remove_target: hoglin kept its target while fleeing")
		}
	})
}
