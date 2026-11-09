//go:build crossfly_full

package hostile_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/hostile"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestRavagerBlockedStrength: IllagerBeastBlockedComponent::onBlocked
// knocks the blocker back with knockback_strength 3 (0.4 x 3 = 1.2 away,
// 3 x 0.4 up capped at 0.4), half of it when the chosen reaction is
// become_stunned.
func TestRavagerBlockedStrength(t *testing.T) {
	w := arena(t, mobtest.Options{}, 16)
	r := w.Spawn("minecraft:ravager", mgl64.Vec3{0.5, 1, 0.5})
	p := w.AddPlayer("blocker", mgl64.Vec3{2.5, 1, 0.5})
	w.Tick(1)
	seen := map[bool]bool{}
	for i := 0; i < 40 && !(seen[true] && seen[false]); i++ {
		w.Do(func(tx *world.Tx) {
			m, _ := r.Mob(tx)
			pe, _ := p.H().Entity(tx)
			pl := pe.(*player.Player)
			c, ok := mob.ComponentOf[*hostile.RavagerBlocked](m)
			if !ok {
				return
			}
			if m.HasComponent("minecraft:is_stunned") { // undo a previous stun
				m.Fire("minecraft:start_roar", nil)
				m.Fire("minecraft:end_roar", nil)
			}
			before := m.HasComponent("minecraft:is_stunned")
			pl.SetVelocity(mgl64.Vec3{})
			c.Blocked(m, pl)
			stunned := !before && m.HasComponent("minecraft:is_stunned")
			want := 1.2
			if stunned {
				want = 0.6
			}
			v := pl.Velocity()
			if !near(v[0], want) || !near(v[1], 0.4) {
				t.Errorf("stunned %v: blocker velocity %v, want x %v y 0.4", stunned, v, want)
			}
			seen[stunned] = true
		})
	}
	if !seen[true] || !seen[false] {
		t.Logf("reactions seen: %v", seen)
	}
}
