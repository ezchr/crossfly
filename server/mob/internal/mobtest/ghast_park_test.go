//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// TestHappyGhastParksUnderPlayer: a player standing on top of an adult happy ghast parks it (entity_sensor
// -> minecraft:become_immobile): it stops, can_move turns off, it is collidable (players stand on it) and
// faces an axis; once the player leaves it moves again.
func TestHappyGhastParksUnderPlayer(t *testing.T) {
	w, g, p := adultGhast(t, mgl64.Vec3{0.5, 3, 0.5})
	g.With(func(m *mob.Mob) {
		m.SetYaw(37)
		if m.ActorFlag(protocol.EntityDataFlagCollidable) {
			t.Fatal("a moving happy ghast is collidable")
		}
	})
	var top float64
	g.With(func(m *mob.Mob) { top = m.Pos()[1] + m.BBox().Height() })
	p.Teleport(mgl64.Vec3{g.Pos()[0], top, g.Pos()[2]})
	p.With(func(pl *player.Player) { pl.SetGameMode(world.GameModeCreative) }) // no falling through
	if n, ok := w.TickUntil(40, func(tx *world.Tx) bool {
		m, _ := g.Mob(tx)
		return m.ActorFlag(protocol.EntityDataFlagCollidable)
	}); !ok {
		t.Fatalf("happy ghast did not park under the player in %d ticks:\n%v", n, w.Dump(g))
	}
	g.With(func(m *mob.Mob) {
		if v, _ := m.Property("minecraft:can_move"); v != false {
			t.Error("a parked happy ghast can move")
		}
		if y := m.Yaw(); y != 0 && y != 90 && y != -90 && y != 180 && y != -180 {
			t.Errorf("parked happy ghast faces %.1f, not an axis", y)
		}
		if !m.ActorFlag(protocol.EntityDataFlagBodyRotationAxisAligned) {
			t.Error("parked happy ghast has no axis aligned body")
		}
	})
	p.Teleport(mgl64.Vec3{12.5, 1, 12.5})
	if n, ok := w.TickUntil(40, func(tx *world.Tx) bool {
		m, _ := g.Mob(tx)
		return !m.ActorFlag(protocol.EntityDataFlagCollidable)
	}); !ok {
		t.Fatalf("happy ghast stayed parked %d ticks after the player left:\n%v", n, w.Dump(g))
	}
}
