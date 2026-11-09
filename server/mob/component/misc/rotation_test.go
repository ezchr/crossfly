//go:build crossfly_full

package misc_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/misc"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

func TestAxisYaw(t *testing.T) {
	for in, want := range map[float64]float64{0: 0, 44: 0, 46: 90, -44: 0, -46: -90, 135.1: 180, -170: -180, 260: 270} {
		if got := misc.AxisYaw(in); got != want {
			t.Errorf("AxisYaw(%v) = %v, want %v", in, got, want)
		}
	}
}

// TestHappyGhastStill: a happy ghast's body follows its head. A player on
// top makes it stand still (its entity_sensor fires become_immobile): it is
// collidable, its body blocked and on a block axis, with the actor flags for
// the client. Once the player is gone it moves again and the flags go.
func TestHappyGhastStill(t *testing.T) {
	mobtest.RequireDefinitions(t)
	mobtest.RequireFilters(t)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 10, block.Stone{}) })
	g := w.Spawn("minecraft:happy_ghast", mgl64.Vec3{0.5, 6, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_adult"})
	w.Tick(2)
	g.With(func(m *mob.Mob) {
		if !m.ActorFlag(protocol.EntityDataFlagBodyRotationAlwaysFollowsHead) {
			t.Error("no body_rotation_always_follows_head flag")
		}
		if m.ActorFlag(protocol.EntityDataFlagCollidable) || m.ActorFlag(protocol.EntityDataFlagBodyRotationAxisAligned) {
			t.Error("a moving happy ghast is collidable / axis aligned")
		}
		if m.BodyYaw() != m.HeadYaw() {
			t.Errorf("body yaw %v, head yaw %v", m.BodyYaw(), m.HeadYaw())
		}
	})
	p := w.AddPlayer("rider", g.Pos().Add(mgl64.Vec3{0, 4, 0}))
	still := func(m *mob.Mob) bool {
		return m.ActorFlag(protocol.EntityDataFlagCollidable) && m.ActorFlag(protocol.EntityDataFlagBodyRotationAxisAligned)
	}
	if _, ok := w.TickUntil(20, func(tx *world.Tx) bool { m, ok := g.Mob(tx); return ok && still(m) }); !ok {
		t.Fatalf("a player on top did not stop the happy ghast: %v", w.Dump(g))
	}
	w.Tick(2)
	g.With(func(m *mob.Mob) {
		if y := m.BodyYaw(); y != misc.AxisYaw(y) || m.Yaw() != y {
			t.Errorf("standing still: body yaw %v, yaw %v, want a multiple of 90", y, m.Yaw())
		}
	})
	p.Teleport(mgl64.Vec3{0.5, 1, 9.5})
	if _, ok := w.TickUntil(20, func(tx *world.Tx) bool {
		m, ok := g.Mob(tx)
		return ok && !m.ActorFlag(protocol.EntityDataFlagCollidable) && !m.ActorFlag(protocol.EntityDataFlagBodyRotationAxisAligned)
	}); !ok {
		t.Errorf("flags kept after the player left: %v", w.Dump(g))
	}
}

// TestNotPickableFromInside: the heart-bound creaking's flag.
func TestNotPickableFromInside(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{})
	c := w.Spawn("minecraft:creaking", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:entity_spawned_by_creaking_heart"})
	c.With(func(m *mob.Mob) {
		if !m.ActorFlag(protocol.EntityDataFlagNotPickableFromInside) {
			t.Error("no not_pickable_from_inside flag")
		}
	})
}
