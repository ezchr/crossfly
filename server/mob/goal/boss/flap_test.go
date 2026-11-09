//go:build crossfly_full

package boss_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/mob"
	bc "github.com/df-mc/dragonfly/server/mob/component/boss"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestMovementTrackingDefaults: emit_move and emit_swim default on,
// emit_flap off (GameEventMovementTrackingDefinition); the vanilla flyers
// flap and the vex makes no steps or swims.
func TestMovementTrackingDefaults(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:game_event_movement_tracking")
	for id, want := range map[string]bc.MovementTracking{
		"minecraft:bat":     {EmitMove: true, EmitSwim: true, EmitFlap: true},
		"minecraft:bee":     {EmitMove: true, EmitSwim: true, EmitFlap: true},
		"minecraft:chicken": {EmitMove: true, EmitSwim: true, EmitFlap: true},
		"minecraft:vex":     {EmitMove: false, EmitSwim: false, EmitFlap: false},
	} {
		r := w.Spawn(id, mgl64.Vec3{0.5, 3, 0.5})
		r.With(func(m *mob.Mob) {
			c, ok := mob.ComponentOf[*bc.MovementTracking](m)
			if !ok {
				t.Fatalf("%v: no game_event_movement_tracking", id)
			}
			if c.EmitMove != want.EmitMove || c.EmitSwim != want.EmitSwim || c.EmitFlap != want.EmitFlap {
				t.Errorf("%v: %+v, want %+v", id, *c, want)
			}
			_ = m.Close()
		})
	}
}

// TestWardenHearsFlapping: a bat flying 8 blocks from a warden flaps, and
// the warden hears it although the bat never steps.
func TestWardenHearsFlapping(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:vibration_listener", "minecraft:game_event_movement_tracking")
	r := w.Spawn(bc.WardenID, mgl64.Vec3{0.5, 1, 0.5})
	bat := w.Spawn("minecraft:bat", mgl64.Vec3{8.5, 4, 0.5})
	w.Tick(2)
	w.Each(60, func(tx *world.Tx, _ int) bool {
		m := mobOf(t, tx, bat)
		m.SetVelocity(mgl64.Vec3{0, 0.02, 0.05})
		return true
	})
	w.Do(func(tx *world.Tx) {
		l, _ := bc.ListenerOf(mobOf(t, tx, r))
		if l.Received == 0 {
			t.Fatalf("the flying bat was not heard")
		}
		if d := l.LastPos.Sub(mobOf(t, tx, bat).Position()).Len(); d > 3 {
			t.Errorf("heard something %.1f blocks from the bat", d)
		}
	})
}
