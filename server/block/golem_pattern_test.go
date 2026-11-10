package block

import (
	"context"
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// golemSpawn is a mob spawned through a fakeGolemHost.
type golemSpawn struct {
	id   string
	pos  mgl64.Vec3
	opts MobSpawnOptions
}

// fakeGolemHost is a MobHost that records the mobs blocks spawn.
type fakeGolemHost struct {
	spawns []golemSpawn
	fail   bool
}

func (h *fakeGolemHost) SpawnMob(_ *world.Tx, id string, pos mgl64.Vec3, opts MobSpawnOptions) (world.Entity, bool) {
	if h.fail {
		return nil, false
	}
	h.spawns = append(h.spawns, golemSpawn{id: id, pos: pos, opts: opts})
	return nil, true
}

// withGolemHost installs a fakeGolemHost for the test.
func withGolemHost(t *testing.T) *fakeGolemHost {
	t.Helper()
	old := mobHost
	h := &fakeGolemHost{}
	mobHost = h
	t.Cleanup(func() { mobHost = old })
	return h
}

func golemWorld(t *testing.T, f func(tx *world.Tx)) {
	t.Helper()
	w := world.Config{Dim: world.End, Synchronous: true}.New()
	defer func() { _ = w.Close() }()
	if err := w.Do(f).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func golemNear(a, b mgl64.Vec3) bool { return a.Sub(b).Len() < 1e-9 }

func golemOnlySpawn(t *testing.T, h *fakeGolemHost, id string) golemSpawn {
	t.Helper()
	if len(h.spawns) != 1 || h.spawns[0].id != id {
		t.Fatalf("spawns = %+v, want one %v", h.spawns, id)
	}
	return h.spawns[0]
}

func golemRequireAir(t *testing.T, tx *world.Tx, positions ...cube.Pos) {
	t.Helper()
	for _, p := range positions {
		if b := tx.Block(p); !isGolemAir(b) {
			t.Errorf("block at %v is %#v, want air", p, b)
		}
	}
}

func TestSnowGolemBuiltFromCarvedPumpkin(t *testing.T) {
	h := withGolemHost(t)
	golemWorld(t, func(tx *world.Tx) {
		base := cube.Pos{3, 10, -4}
		tx.SetBlock(base, Snow{}, nil)
		tx.SetBlock(base.Add(cube.Pos{0, 1, 0}), Snow{}, nil)
		head := base.Add(cube.Pos{0, 2, 0})
		tx.SetBlock(head, Pumpkin{Carved: true, Facing: cube.East}, nil)
		if !spawnGolem(tx, head) {
			t.Fatal("no golem built")
		}
		s := golemOnlySpawn(t, h, "minecraft:snow_golem")
		// BDS: the BuildMatch position (the lowest cell) + (0.5, 0.05, 0.5), the default spawn event.
		if want := (mgl64.Vec3{3.5, 10.05, -3.5}); !golemNear(s.pos, want) || s.opts.Event != "" {
			t.Fatalf("snow golem at %v event %q, want %v and no event", s.pos, s.opts.Event, want)
		}
		golemRequireAir(t, tx, base, base.Add(cube.Pos{0, 1, 0}), head)
	})
}

func TestSnowGolemFromJackOLanternAndSideways(t *testing.T) {
	h := withGolemHost(t)
	golemWorld(t, func(tx *world.Tx) {
		// Lying along +x: BDS matches any orientation and raises the golem by two blocks.
		head := cube.Pos{0, 5, 0}
		tx.SetBlock(head, LitPumpkin{}, nil)
		tx.SetBlock(cube.Pos{1, 5, 0}, Snow{}, nil)
		tx.SetBlock(cube.Pos{2, 5, 0}, Snow{}, nil)
		if !spawnGolem(tx, head) {
			t.Fatal("no golem built")
		}
		s := golemOnlySpawn(t, h, "minecraft:snow_golem")
		if want := (mgl64.Vec3{0.5, 5.05, 0.5}); !golemNear(s.pos, want) {
			t.Fatalf("snow golem at %v, want %v", s.pos, want)
		}
	})
}

func TestNoGolemFromPlainPumpkinOrWithoutMobs(t *testing.T) {
	golemWorld(t, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 1, 0}, Snow{}, nil)
		tx.SetBlock(cube.Pos{0, 2, 0}, Snow{}, nil)
		tx.SetBlock(cube.Pos{0, 3, 0}, Pumpkin{Carved: true}, nil)
		old := mobHost
		mobHost = nil
		built := spawnGolem(tx, cube.Pos{0, 3, 0})
		mobHost = old
		if built {
			t.Fatal("golem built without mobs")
		}
		if _, ok := tx.Block(cube.Pos{0, 2, 0}).(Snow); !ok {
			t.Fatal("pattern removed without mobs")
		}
	})
	h := withGolemHost(t)
	golemWorld(t, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 1, 0}, Snow{}, nil)
		tx.SetBlock(cube.Pos{0, 2, 0}, Snow{}, nil)
		tx.SetBlock(cube.Pos{0, 3, 0}, Pumpkin{}, nil)
		if spawnGolem(tx, cube.Pos{0, 3, 0}) || len(h.spawns) != 0 {
			t.Fatalf("golem built from a plain pumpkin: %+v", h.spawns)
		}
	})
	h.fail = true
	golemWorld(t, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 1, 0}, Snow{}, nil)
		tx.SetBlock(cube.Pos{0, 2, 0}, Snow{}, nil)
		tx.SetBlock(cube.Pos{0, 3, 0}, Pumpkin{Carved: true}, nil)
		if spawnGolem(tx, cube.Pos{0, 3, 0}) {
			t.Fatal("golem reported built while the spawn failed")
		}
		if _, ok := tx.Block(cube.Pos{0, 1, 0}).(Snow); !ok {
			t.Fatal("pattern removed although no golem spawned")
		}
	})
}

// buildGolemIronT builds the iron T with its arms along x (alongX) or z under head.
func buildGolemIronT(tx *world.Tx, head cube.Pos, alongX bool) []cube.Pos {
	arm := cube.Pos{0, 0, 1}
	if alongX {
		arm = cube.Pos{1, 0, 0}
	}
	body := head.Add(cube.Pos{0, -1, 0})
	irons := []cube.Pos{body, body.Add(arm), body.Sub(arm), body.Add(cube.Pos{0, -1, 0})}
	for _, p := range irons {
		tx.SetBlock(p, Iron{}, nil)
	}
	return append(irons, head)
}

func TestIronGolemBuiltBothWays(t *testing.T) {
	for _, tc := range []struct {
		alongX bool
		pos    mgl64.Vec3
		yaw    float64
	}{
		// BDS: the BuildMatch position, not centred: forward south (yaw +90) when the arms run along x, west along z.
		{alongX: true, pos: mgl64.Vec3{10, 18, 21}, yaw: 90},
		{alongX: false, pos: mgl64.Vec3{11, 18, 20}, yaw: 0},
	} {
		h := withGolemHost(t)
		golemWorld(t, func(tx *world.Tx) {
			head := cube.Pos{10, 20, 20}
			cells := buildGolemIronT(tx, head, tc.alongX)
			tx.SetBlock(head, Pumpkin{Carved: true}, nil)
			if !spawnGolem(tx, head) {
				t.Fatal("no iron golem built")
			}
			s := golemOnlySpawn(t, h, "minecraft:iron_golem")
			if !golemNear(s.pos, tc.pos) || s.opts.Yaw != tc.yaw || s.opts.Event != "minecraft:from_player" {
				t.Fatalf("along x %v: iron golem at %v yaw %v event %q, want %v yaw %v from_player", tc.alongX, s.pos, s.opts.Yaw, s.opts.Event, tc.pos, tc.yaw)
			}
			golemRequireAir(t, tx, cells...)
		})
	}
}

func TestIronGolemNeedsAirBesideTheLeg(t *testing.T) {
	h := withGolemHost(t)
	golemWorld(t, func(tx *world.Tx) {
		head := cube.Pos{0, 3, 0}
		buildGolemIronT(tx, head, true)
		tx.SetBlock(cube.Pos{1, 1, 0}, Stone{}, nil)
		tx.SetBlock(head, Pumpkin{Carved: true}, nil)
		if spawnGolem(tx, head) || len(h.spawns) != 0 {
			t.Fatalf("iron golem built with a block beside its leg: %+v", h.spawns)
		}
		// The empty corners above the arms must be air as well.
		tx.SetBlock(cube.Pos{1, 1, 0}, Air{}, nil)
		tx.SetBlock(cube.Pos{-1, 3, 0}, Stone{}, nil)
		if spawnGolem(tx, head) || len(h.spawns) != 0 {
			t.Fatalf("iron golem built with a block beside its head: %+v", h.spawns)
		}
		tx.SetBlock(cube.Pos{-1, 3, 0}, Air{}, nil)
		if !spawnGolem(tx, head) {
			t.Fatal("no iron golem built once the corners are clear")
		}
	})
}

func TestCopperGolemFollowsCopperOxidation(t *testing.T) {
	for _, o := range OxidationTypes() {
		for _, waxed := range []bool{false, true} {
			h := withGolemHost(t)
			golemWorld(t, func(tx *world.Tx) {
				copper, head := cube.Pos{4, 6, 4}, cube.Pos{4, 7, 4}
				tx.SetBlock(copper, Copper{Type: NormalCopper(), Oxidation: o, Waxed: waxed}, nil)
				tx.SetBlock(head, Pumpkin{Carved: true, Facing: cube.West}, nil)
				if !spawnGolem(tx, head) {
					t.Fatalf("%v waxed %v: no copper golem built", o, waxed)
				}
				s := golemOnlySpawn(t, h, "minecraft:copper_golem")
				want := copperGolemEvents[o.Uint8()]
				// At the head's block, centred, looking where the head looked (west: yaw 90).
				if s.opts.Event != want || !golemNear(s.pos, mgl64.Vec3{4.5, 7, 4.5}) || math.Abs(s.opts.Yaw-90) > 1e-9 {
					t.Fatalf("%v waxed %v: copper golem %+v, want event %v at (4.5, 7, 4.5) yaw 90", o, waxed, s, want)
				}
				// Without copper chests registered the copper block is used up.
				golemRequireAir(t, tx, copper, head)
			})
		}
	}
}

func TestNoCopperGolemOnCutCopper(t *testing.T) {
	h := withGolemHost(t)
	golemWorld(t, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 1, 0}, Copper{Type: CutCopper()}, nil)
		tx.SetBlock(cube.Pos{0, 2, 0}, Pumpkin{Carved: true}, nil)
		if spawnGolem(tx, cube.Pos{0, 2, 0}) || len(h.spawns) != 0 {
			t.Fatalf("copper golem built on cut copper: %+v", h.spawns)
		}
	})
}

func TestGolemYawFollowsBDS(t *testing.T) {
	for d, want := range map[cube.Direction]float64{cube.South: 0, cube.West: 90, cube.North: -180, cube.East: -90} {
		if got := golemYaw(d); math.Abs(got-want) > 1e-9 {
			t.Errorf("golemYaw(%v) = %v, want %v", d, got, want)
		}
	}
}
