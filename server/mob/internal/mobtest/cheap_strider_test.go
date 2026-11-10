//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	goalcore "github.com/df-mc/dragonfly/server/mob/goal/core"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestStriderWalkTarget: Strider::_getWalkTargetValue is 10 in lava and 0
// elsewhere, so a strider on a grass shore picks stroll targets in the lava
// next to it, where an animal would pick the grass.
func TestStriderWalkTarget(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Fill(tx, cube.Pos{-14, -1, -14}, cube.Pos{14, -1, 14}, block.Stone{})
		w.Fill(tx, cube.Pos{-1, 0, -14}, cube.Pos{14, 0, 14}, block.Grass{})
		w.Fill(tx, cube.Pos{-14, 0, -14}, cube.Pos{-2, 0, 14}, block.Lava{Still: true, Depth: 8})
	})
	s := w.Spawn("minecraft:strider", mgl64.Vec3{0.5, 1, 0.5})
	c := w.Spawn("minecraft:cow", mgl64.Vec3{0.5, 1, 3.5})
	w.Tick(1)
	w.Do(func(tx *world.Tx) {
		sm, _ := s.Mob(tx)
		cm, _ := c.Mob(tx)
		lava, grass := cube.Pos{-5, 0, 0}, cube.Pos{5, 1, 0}
		if v := goalcore.WalkTargetValue(sm, lava); v != 10 {
			t.Errorf("strider walk target value in lava %v, want 10", v)
		}
		if v := goalcore.WalkTargetValue(sm, grass); v != 0 {
			t.Errorf("strider walk target value on grass %v, want 0", v)
		}
		if v := goalcore.WalkTargetValue(cm, grass); v != 10 {
			t.Errorf("cow walk target value on grass %v, want 10", v)
		}
		inLava := func(m *mob.Mob) int {
			n := 0
			for i := 0; i < 20; i++ {
				if p, ok := goalcore.RandomPos(m, 10, 7, 0, nil, 10); ok && p[0] < -1 {
					n++
				}
			}
			return n
		}
		sm.SetRandSeed(1, 2)
		cm.SetRandSeed(1, 2)
		if n := inLava(sm); n < 18 {
			t.Errorf("strider picked %v of 20 stroll targets in the lava, want nearly all", n)
		}
		if n := inLava(cm); n > 2 {
			t.Errorf("cow picked %v of 20 stroll targets in the lava", n)
		}
	})
}
