//go:build crossfly_full

package javasession

import (
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"testing"
)

func TestPistonDisplacementPreservesPlayerVelocity(t *testing.T) {
	w, h, _ := testWorld(t)
	do(w, func(tx *world.Tx) {
		e, ok := h.Entity(tx)
		if !ok {
			t.Fatal("missing player")
		}
		p := e.(*player.Player)
		velocity := mgl64.Vec3{.2, -.1, .3}
		p.SetVelocity(velocity)
		before := p.Position()
		p.MoveByPiston(mgl64.Vec3{.5, 0, 0})
		if !p.Position().ApproxEqual(before.Add(mgl64.Vec3{.5, 0, 0})) {
			t.Fatal("piston did not displace player")
		}
		if !p.Velocity().ApproxEqual(velocity) {
			t.Fatalf("piston erased velocity: %v", p.Velocity())
		}
	})
}
