//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type nbtHookKey struct{}

func init() {
	mob.RegisterNBTHook(mob.NBTHook{
		Name: "mobtest/nbt_hook",
		Encode: func(m *mob.Mob) map[string]any {
			if v, ok := m.Data(nbtHookKey{}); ok {
				return map[string]any{"mobtest:hook": v.(int32)}
			}
			return nil
		},
		Decode: func(m *mob.Mob, d map[string]any) {
			if v, ok := d["mobtest:hook"].(int32); ok {
				m.SetData(nbtHookKey{}, v)
			}
		},
	})
}

// TestNBTHookRoundTrip: what an NBTHook saves comes back when the mob is
// loaded again (the trades a zombie villager carries use this).
func TestNBTHookRoundTrip(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{Persistent: true})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
	c := w.Spawn("minecraft:cow", mgl64.Vec3{0.5, 1, 0.5})
	c.With(func(m *mob.Mob) {
		m.SetPersistent(true)
		m.SetData(nbtHookKey{}, int32(42))
	})
	w.Tick(2)
	w.Reload()
	w.Tick(1)
	c.With(func(m *mob.Mob) {
		if v, ok := m.Data(nbtHookKey{}); !ok || v.(int32) != 42 {
			t.Errorf("hook data after reload: %v %v, want 42", v, ok)
		}
	})
}
