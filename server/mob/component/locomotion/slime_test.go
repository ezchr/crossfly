//go:build crossfly_full

package locomotion_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	_ "github.com/df-mc/dragonfly/server/mob/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// TestSlimeSquishEvents: a hopping slime sends client event 2 when it jumps
// and 1 when it lands (actor data 0x18).
func TestSlimeSquishEvents(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Arena(tx, 12, 3) })
	s := w.Spawn("minecraft:slime", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "spawn_medium"})
	seen := map[int8]bool{}
	w.Each(200, func(tx *world.Tx, _ int) bool {
		if m, ok := s.Mob(tx); ok {
			if v, ok := m.ActorData(protocol.EntityDataKeyClientEvent); ok {
				seen[v.(int8)] = true
			}
		}
		return true
	})
	if !seen[1] || !seen[2] {
		t.Errorf("slime client events seen %v, want both 1 (landed) and 2 (jumped)", seen)
	}
}

// TestSlimeSplitsAfterDeathAnimation: a killed slime splits when its corpse
// is removed (Slime::remove), not at once.
func TestSlimeSplitsAfterDeathAnimation(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Arena(tx, 12, 3) })
	s := w.Spawn("minecraft:slime", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "spawn_large"})
	w.Tick(2)
	s.With(func(m *mob.Mob) { m.Kill(entity.VoidDamageSource{}) })
	count := func() int {
		n := 0
		w.Do(func(tx *world.Tx) {
			for _, k := range w.Mobs("minecraft:slime") {
				if m, ok := k.Mob(tx); ok && !m.Dead() {
					n++
				}
			}
		})
		return n
	}
	w.Tick(15)
	if n := count(); n != 0 {
		t.Errorf("%d slimes 15 ticks after the death, want none before the corpse goes", n)
	}
	w.Tick(10)
	if n := count(); n < 2 || n > 4 {
		t.Errorf("%d slimes after the corpse went, want 2-4", n)
	}
	if !s.Gone() {
		t.Errorf("dead slime still there after 27 ticks")
	}
}
