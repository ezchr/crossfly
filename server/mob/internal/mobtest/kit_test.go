//go:build crossfly_full

package mobtest

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// These tests check the kit itself and pass with only Phase 0 linked.

func TestKitMobFallsOntoFloor(t *testing.T) {
	w := New(t, Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 4, block.Stone{}) })
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{0.5, 6, 0.5})
	n, ok := w.TickUntil(100, func(tx *world.Tx) bool {
		m, ok := z.Mob(tx)
		return ok && m.OnGround()
	})
	if !ok {
		t.Fatalf("zombie never landed: %v", w.Dump(z))
	}
	if y := z.Pos()[1]; math.Abs(y-1) > 1e-6 {
		t.Fatalf("zombie landed at y %v after %d ticks, want 1", y, n)
	}
	if w.Ticks() != n {
		t.Fatalf("Ticks = %d, want %d", w.Ticks(), n)
	}
}

func TestKitStairs(t *testing.T) {
	w := New(t, Options{})
	var top cube.Pos
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 8, block.Stone{})
		top = w.Stairs(tx, cube.Pos{0, 1, 0}, cube.North, 4, 2, block.Planks{}, block.Stone{})
		for i := range 4 {
			p := cube.Pos{0, 1 + i, -i}
			if s, ok := tx.Block(p).(block.Stairs); !ok || s.Facing != cube.North {
				t.Errorf("step %d at %v is %#v", i, p, tx.Block(p))
			}
			if _, ok := tx.Block(p.Side(cube.FaceEast)).(block.Stairs); !ok {
				t.Errorf("step %d is not 2 wide", i)
			}
			if i > 0 {
				if _, ok := tx.Block(cube.Pos{0, i, -i}).(block.Stone); !ok {
					t.Errorf("no fill under step %d", i)
				}
			}
		}
	})
	if want := (cube.Pos{0, 5, -3}); top != want {
		t.Fatalf("top = %v, want %v", top, want)
	}
}

func TestKitPlayer(t *testing.T) {
	w := New(t, Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 4, block.Stone{}) })
	p := w.AddPlayer("stand-in", mgl64.Vec3{0.5, 1, 0.5})
	p.Hold(item.NewStack(item.Wheat{}, 3))
	w.Tick(20)
	if h := p.Health(); h != 20 {
		t.Fatalf("player health %v", h)
	}
	if y := p.Pos()[1]; math.Abs(y-1) > 1e-6 {
		t.Fatalf("player at y %v, want 1", y)
	}
	n := w.Count(func(e world.Entity) bool { return e.H() == p.H() })
	if n != 1 {
		t.Fatalf("player counted %d times", n)
	}
}

func TestKitReload(t *testing.T) {
	w := New(t, Options{Persistent: true})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 20, block.Stone{}) })
	p := w.AddPlayer("stand-in", mgl64.Vec3{18.5, 1, 18.5})
	p.Hold(item.NewStack(item.Wheat{}, 1))
	cow := w.Spawn("minecraft:cow", mgl64.Vec3{-18.5, 1, -18.5})
	cow.With(func(m *mob.Mob) {
		m.SetMaxHealth(10)
		m.SetHealth(4)
		m.SetPersistent(true)
		m.SetHome(cube.Pos{1, 2, 3})
	})
	w.Tick(5)
	before := cow.Pos()
	id := cow.UUID()

	w.Reload()

	if cow.H() == nil {
		t.Fatal("cow not found after reload")
	}
	if cow.UUID() != id {
		t.Fatal("uuid changed")
	}
	w.Tick(1) // initialises the loaded mob
	cow.With(func(m *mob.Mob) {
		if !m.Initialised() {
			t.Error("not initialised after a tick")
		}
		if m.Health() != 4 {
			t.Errorf("health %v, want 4", m.Health())
		}
		if !m.Persistent() {
			t.Error("lost persistence")
		}
		if h, ok := m.Home(); !ok || h != (cube.Pos{1, 2, 3}) {
			t.Errorf("home %v %v", h, ok)
		}
		if d := m.Pos().Sub(before).Len(); d > 0.5 {
			t.Errorf("moved %v blocks over the reload", d)
		}
	})
	if p.H() == nil {
		t.Fatal("player not added back")
	}
	p.With(func(pl *player.Player) {
		if h, _ := pl.HeldItems(); h.Count() != 1 {
			t.Errorf("player holds %v after reload", h)
		}
	})
	if n := len(w.Mobs("minecraft:cow")); n != 1 {
		t.Fatalf("%d cows after reload", n)
	}
}

func TestKitRequire(t *testing.T) {
	t.Run("skips", func(t *testing.T) {
		Require(t, "minecraft:behavior.no_such_goal")
		t.Fatal("did not skip")
	})
}

func TestKitComponentJSON(t *testing.T) {
	var swell struct {
		Start float64 `json:"start_distance"`
		Stop  float64 `json:"stop_distance"`
	}
	Decode(t, Component(t, "creeper.json", "", "minecraft:behavior.swell"), &swell)
	if swell.Start != 2.5 || swell.Stop != 6 {
		t.Fatalf("swell %+v", swell)
	}
}
