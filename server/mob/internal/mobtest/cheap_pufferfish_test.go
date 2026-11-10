//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// puffState is the pufferfish's actor data 86 (PUFFED_STATE), -1 when unset.
func puffState(m *mob.Mob) int8 {
	v, ok := m.ActorData(protocol.EntityDataKeyPuffedState)
	if !ok {
		return -1
	}
	return v.(int8)
}

// TestPufferfishPuffsWhenHurt: Pufferfish::_hurt fires to_full_puff when the
// fish survives a hit, and Pufferfish::normalTick mirrors the puff variant
// into actor data 86; with nothing near, the JSON timers deflate it to half
// (1) after 3 seconds and to normal (0) 2 seconds later.
func TestPufferfishPuffsWhenHurt(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 6, block.Stone{})
		w.Water(tx, cube.Pos{-5, 1, -5}, cube.Pos{5, 5, 5})
	})
	f := w.Spawn("minecraft:pufferfish", mgl64.Vec3{0.5, 3, 0.5})
	w.Tick(2)
	f.With(func(m *mob.Mob) {
		if s := puffState(m); s != 0 {
			t.Fatalf("new pufferfish puff state %v, want 0", s)
		}
		m.Hurt(1, effect.InstantDamageSource{})
		if v := m.Meta().Variant; v != 2 {
			t.Errorf("hurt pufferfish variant %v, want 2 (full puff)", v)
		}
		if s := puffState(m); s != 2 {
			t.Errorf("hurt pufferfish puff state %v, want 2", s)
		}
	})
	state := func(want int8) func(tx *world.Tx) bool {
		return func(tx *world.Tx) bool {
			m, ok := f.Mob(tx)
			return ok && puffState(m) == want
		}
	}
	n, ok := w.TickUntil(100, state(1))
	if !ok || n < 55 {
		t.Fatalf("pufferfish half deflated after %v ticks (%v), want about 60", n, ok)
	}
	if n, ok := w.TickUntil(60, state(0)); !ok || n < 35 {
		t.Errorf("pufferfish deflated after %v more ticks (%v), want about 40", n, ok)
	}
}
