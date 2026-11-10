//go:build crossfly_full

package hostile

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// spawnSlime spawns a slime of a size.
func spawnSlime(t *testing.T, w *mobtest.World, size int, pos mgl64.Vec3) *mobtest.Ref {
	t.Helper()
	var ref *mobtest.Ref
	w.Do(func(tx *world.Tx) {
		m, err := javamob.Spawn(tx, slimeKind.ID, pos, javamob.SpawnCommand, javamob.SpawnOptions{})
		if err != nil {
			t.Fatal(err)
		}
		c, _ := cubeOf(m)
		c.SetSize(m, size, true)
		ref = w.Track(m.H())
	})
	return ref
}

// TestSlimeSize: a size-4 slime has 16 health, 4 attack, speed 0.6, a box
// 2.08 wide, and shows its size as the variant.
func TestSlimeSize(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
	s := spawnSlime(t, w, 4, mgl64.Vec3{0.5, 1, 0.5})
	withJ(t, w, s, func(m *javamob.Mob) {
		if m.MaxHealth() != 16 || m.Health() != 16 {
			t.Errorf("health %v/%v, want 16", m.Health(), m.MaxHealth())
		}
		if m.Attr(javamob.AttrAttackDamage) != 4 {
			t.Errorf("attack %v", m.Attr(javamob.AttrAttackDamage))
		}
		if v := m.Attr(javamob.AttrMovementSpeed); v < 0.599 || v > 0.601 {
			t.Errorf("speed %v", v)
		}
		if w := m.Width(); w < 2.079 || w > 2.081 {
			t.Errorf("width %v", w)
		}
		if m.Meta().Variant != 4 {
			t.Errorf("variant %v", m.Meta().Variant)
		}
	})
}

// TestSlimeHops: a slime hops around on its own.
func TestSlimeHops(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 16, block.Stone{}) })
	s := spawnSlime(t, w, 2, mgl64.Vec3{0.5, 1, 0.5})
	start := s.Pos()
	jumped := false
	w.Each(200, func(tx *world.Tx, _ int) bool {
		if s.PosTx(tx)[1] > 1.3 {
			jumped = true
		}
		return true
	})
	if !jumped {
		t.Errorf("the slime never jumped")
	}
	if d := s.Pos().Sub(start); d[0]*d[0]+d[2]*d[2] < 1 {
		t.Errorf("the slime did not move: %v", d)
	}
}

// TestSlimeSplits: a dead size-4 slime leaves 2..4 size-2 slimes when its
// body goes; a dead size-1 slime leaves none.
func TestSlimeSplits(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 16, block.Stone{}) })
	big := spawnSlime(t, w, 4, mgl64.Vec3{0.5, 1, 0.5})
	small := spawnSlime(t, w, 1, mgl64.Vec3{8.5, 1, 8.5})
	w.Do(func(tx *world.Tx) {
		for _, r := range []*mobtest.Ref{big, small} {
			m, _ := r.Mob(tx)
			m.Kill(entityAttack(m))
		}
	})
	w.Tick(25)
	slimes := w.Mobs(slimeKind.ID)
	if n := len(slimes); n < 2 || n > 4 {
		t.Fatalf("%d slimes after the split, want 2..4", n)
	}
	for _, r := range slimes {
		withJ(t, w, r, func(m *javamob.Mob) {
			if c, _ := cubeOf(m); c.Size() != 2 {
				t.Errorf("child size %d, want 2", c.Size())
			}
		})
	}
}

// TestSlimeContactDamage: a size-2 slime hurts a player standing against
// it; a size-1 slime does not.
func TestSlimeContactDamage(t *testing.T) {
	for _, tc := range []struct {
		size int
		hurt bool
	}{{2, true}, {1, false}} {
		t.Run(map[bool]string{true: "big", false: "tiny"}[tc.hurt], func(t *testing.T) {
			w := mobtest.New(t, mobtest.Options{})
			w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 12, block.Stone{}) })
			p := w.AddPlayer("tester", mgl64.Vec3{1.5, 1, 0.5})
			spawnSlime(t, w, tc.size, mgl64.Vec3{0.5, 1, 0.5})
			before := p.Health()
			w.Tick(200)
			if hurt := p.Health() < before; hurt != tc.hurt {
				t.Errorf("size %d: player hurt %v, want %v", tc.size, hurt, tc.hurt)
			}
		})
	}
}
