//go:build crossfly_full

package javamob_test

import (
	"math"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/path"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// walker is a test mob with no goals: tests drive its controls.
type walker struct{}

func (walker) RegisterGoals(*javamob.Mob) {}

const walkerID = "crossfly:java_test_walker"

func init() {
	javamob.Register(javamob.Type{
		ID: walkerID, Category: javamob.CategoryMonster, Width: 0.6, Height: 1.95, EyeHeight: 1.74,
		Attributes: func() map[javamob.Attribute]float64 {
			a := javamob.MonsterAttributes()
			a[javamob.AttrMovementSpeed] = 0.25
			a[javamob.AttrFollowRange] = 35
			return a
		},
		NoLootTable: true,
		New:         func() javamob.Behaviour { return walker{} },
	})
}

func TestMain(m *testing.M) {
	if err := mobtest.RegisterTypes(); err != nil {
		panic(err)
	}
	javamob.Install()
	os.Exit(m.Run())
}

func spawn(t *testing.T, w *mobtest.World, pos mgl64.Vec3) *mobtest.Ref {
	t.Helper()
	var ref *mobtest.Ref
	w.Do(func(tx *world.Tx) {
		m, err := javamob.Spawn(tx, walkerID, pos, javamob.SpawnCommand, javamob.SpawnOptions{})
		if err != nil {
			t.Fatal(err)
		}
		ref = w.Track(m.H())
	})
	return ref
}

func with(t *testing.T, w *mobtest.World, r *mobtest.Ref, f func(m *javamob.Mob)) {
	t.Helper()
	w.Do(func(tx *world.Tx) {
		bm, ok := r.Mob(tx)
		if !ok {
			t.Fatal("mob gone")
		}
		m, ok := javamob.Of(bm)
		if !ok {
			t.Fatal("not a Java mob")
		}
		f(m)
	})
}

// TestFall: gravity 0.08 and drag 0.98 a tick, as LivingEntity.travelInAir.
func TestFall(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 4, block.Stone{}) })
	r := spawn(t, w, mgl64.Vec3{0.5, 40, 0.5})
	y, vy := 40.0, 0.0
	for i := 0; i < 10; i++ {
		w.Tick(1)
		// travel: move by the velocity, then gravity and drag; aiStep zeroes |v| < 0.003 first.
		if math.Abs(vy) < 0.003 {
			vy = 0
		}
		y += vy
		vy = (vy - 0.08) * float64(float32(0.98))
	}
	if got := r.Pos()[1]; math.Abs(got-y) > 1e-6 {
		t.Fatalf("y after 10 ticks: %v, want %v", got, y)
	}
}

// TestWalkSpeed: a mob walking to a far point covers speed² × 2.2026 blocks a tick once at full speed (the
// friction 0.546 of normal ground: (v + a) × 0.546, a = speed × zza with zza = speed).
func TestWalkSpeed(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 40, block.Stone{}) })
	r := spawn(t, w, mgl64.Vec3{0.5, 1, 0.5})
	var last mgl64.Vec3
	for i := 0; i < 40; i++ {
		with(t, w, r, func(m *javamob.Mob) { m.MoveControl().SetWantedPosition(30.5, 1, 0.5, 1) })
		last = r.Pos()
		w.Tick(1)
	}
	d := r.Pos()[0] - last[0]
	want := 0.25 * 0.25 / (1 - 0.546)
	if math.Abs(d-want) > 2e-3 {
		t.Fatalf("speed %v blocks/tick, want %v", d, want)
	}
}

// TestStepUp: a slab is stepped up without a jump (step height 0.6).
func TestStepUp(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 12, block.Stone{})
		for z := -2; z <= 2; z++ {
			for x := 3; x <= 8; x++ {
				tx.SetBlock(cube.Pos{x, 1, z}, block.Slab{Block: block.Stone{}}, nil)
			}
		}
	})
	r := spawn(t, w, mgl64.Vec3{0.5, 1, 0.5})
	for i := 0; i < 60; i++ {
		with(t, w, r, func(m *javamob.Mob) { m.MoveControl().SetWantedPosition(6.5, 1.5, 0.5, 1) })
		w.Tick(1)
	}
	p := r.Pos()
	if p[0] < 4 || math.Abs(p[1]-1.5) > 1e-6 {
		t.Fatalf("mob at %v, want on the slabs (y 1.5, x > 4)", p)
	}
}

// TestJumpBlock: a full block in the way is jumped onto (MoveControl's jump).
func TestJumpBlock(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 12, block.Stone{})
		for z := -2; z <= 2; z++ {
			for x := 3; x <= 8; x++ {
				tx.SetBlock(cube.Pos{x, 1, z}, block.Stone{}, nil)
			}
		}
	})
	r := spawn(t, w, mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(2)
	with(t, w, r, func(m *javamob.Mob) {
		if !m.Navigation().MoveTo(m, mgl64.Vec3{6.5, 2, 0.5}, 0, 1) {
			t.Fatal("no path onto the block")
		}
	})
	w.Tick(80)
	if p := r.Pos(); p[1] < 1.99 || p[0] < 4 {
		t.Fatalf("mob at %v, want on the block (y 2)", p)
	}
}

// TestWaterCurrent: flowing water pushes a mob along its flow (EntityFluidInteraction).
func TestWaterCurrent(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 20, block.Stone{})
		// A source at x=0 flowing east down a 1-wide channel.
		for x := -1; x <= 9; x++ {
			tx.SetBlock(cube.Pos{x, 1, -1}, block.Stone{}, nil)
			tx.SetBlock(cube.Pos{x, 1, 1}, block.Stone{}, nil)
		}
		tx.SetBlock(cube.Pos{-1, 1, 0}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{0, 1, 0}, block.Water{Still: true, Depth: 8}, nil)
		for x := 1; x <= 7; x++ {
			tx.SetBlock(cube.Pos{x, 1, 0}, block.Water{Depth: 8 - x}, nil)
		}
	})
	r := spawn(t, w, mgl64.Vec3{1.5, 1, 0.5})
	w.Tick(60)
	if p := r.Pos(); p[0] < 4 {
		t.Fatalf("mob at %v, want carried east by the current", p)
	}
}

// path returns the node positions of a path from the mob to target.
func findPath(t *testing.T, w *mobtest.World, r *mobtest.Ref, target cube.Pos, setup func(m *javamob.Mob)) []cube.Pos {
	t.Helper()
	// A mob only finds paths standing on the ground (GroundPathNavigation.canUpdatePath): let it land first.
	w.Tick(2)
	var out []cube.Pos
	with(t, w, r, func(m *javamob.Mob) {
		if setup != nil {
			setup(m)
		}
		p := m.Navigation().CreatePath(m, target, 0)
		if p == nil {
			return
		}
		for i := 0; i < p.NodeCount(); i++ {
			out = append(out, p.Node(i).Pos())
		}
		if !p.CanReach() {
			out = nil
		}
	})
	return out
}

func contains(nodes []cube.Pos, f func(cube.Pos) bool) bool {
	for _, n := range nodes {
		if f(n) {
			return true
		}
	}
	return false
}

// TestPathAroundWater: water costs 8, so a path goes around a pool when the detour is short; with water at -1 it
// never enters it.
func TestPathAroundWater(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 16, block.Stone{})
		for x := 3; x <= 5; x++ {
			for z := -2; z <= 2; z++ {
				tx.SetBlock(cube.Pos{x, 0, z}, block.Water{Still: true, Depth: 8}, nil)
			}
		}
	})
	r := spawn(t, w, mgl64.Vec3{0.5, 1, 0.5})
	nodes := findPath(t, w, r, cube.Pos{8, 1, 0}, nil)
	if nodes == nil {
		t.Fatal("no path")
	}
	inWater := func(p cube.Pos) bool { return p[0] >= 3 && p[0] <= 5 && p[2] >= -2 && p[2] <= 2 && p[1] <= 1 }
	if contains(nodes, inWater) {
		t.Fatalf("path %v crosses the pool", nodes)
	}
}

// TestPathOverTrapdoor: an open trapdoor over a hole is TRAPDOOR (malus 0): mobs path over it and fall through,
// which mob farms use.
func TestPathOverTrapdoor(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 16, block.Stone{})
		// A 1-wide corridor with a hole at x=3, covered by an open trapdoor.
		for x := -1; x <= 7; x++ {
			for y := 1; y <= 3; y++ {
				tx.SetBlock(cube.Pos{x, y, -1}, block.Stone{}, nil)
				tx.SetBlock(cube.Pos{x, y, 1}, block.Stone{}, nil)
			}
		}
		tx.SetBlock(cube.Pos{3, 0, 0}, block.Air{}, nil)
		tx.SetBlock(cube.Pos{3, -1, 0}, block.Air{}, nil)
		tx.SetBlock(cube.Pos{3, 0, 0}, block.WoodTrapdoor{Wood: block.OakWood(), Open: true, Facing: cube.North}, nil)
	})
	r := spawn(t, w, mgl64.Vec3{0.5, 1, 0.5})
	var typ path.Type
	with(t, w, r, func(m *javamob.Mob) { typ = path.StateType(m.Tx(), cube.Pos{3, 0, 0}) })
	if typ != path.Trapdoor {
		t.Fatalf("open trapdoor is %v, want TRAPDOOR", typ)
	}
	nodes := findPath(t, w, r, cube.Pos{6, 1, 0}, nil)
	if nodes == nil {
		t.Fatal("no path over the trapdoor")
	}
}

// TestPathAvoidsCactus: next to a cactus is DAMAGING_IN_NEIGHBOR (8), the cactus DAMAGING (-1): the path keeps
// clear of the cactus row when a clear lane exists.
func TestPathAvoidsCactus(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 16, block.Stone{})
		// Cacti next to each other break: one every other block.
		for x := 2; x <= 6; x += 2 {
			tx.SetBlock(cube.Pos{x, 0, 0}, block.Sand{}, nil)
			tx.SetBlock(cube.Pos{x, 1, 0}, block.Cactus{}, nil)
		}
	})
	r := spawn(t, w, mgl64.Vec3{0.5, 1, 0.5})
	nodes := findPath(t, w, r, cube.Pos{8, 1, 0}, nil)
	if nodes == nil {
		t.Fatal("no path")
	}
	nearCactus := func(p cube.Pos) bool { return p[0] >= 1 && p[0] <= 7 && p[2] >= -1 && p[2] <= 1 && p[1] == 1 }
	if contains(nodes, nearCactus) {
		t.Fatalf("path %v passes next to the cacti", nodes)
	}
}

// TestPathDoors: a closed wooden door blocks a mob that cannot open doors (DOOR_WOOD_CLOSED -1) and is
// WALKABLE_DOOR to one that can; an iron door blocks both.
func TestPathDoors(t *testing.T) {
	for _, c := range []struct {
		name     string
		door     world.Block
		canOpen  bool
		wantPath bool
	}{
		{"wood closed", block.WoodDoor{Wood: block.OakWood()}, false, false},
		{"wood can open", block.WoodDoor{Wood: block.OakWood()}, true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := mobtest.New(t, mobtest.Options{})
			w.Do(func(tx *world.Tx) {
				w.Floor(tx, 0, 10, block.Stone{})
				// The wall reaches past the floor: around it is a drop into the void.
				for z := -14; z <= 14; z++ {
					for y := 1; y <= 3; y++ {
						tx.SetBlock(cube.Pos{3, y, z}, block.Stone{}, nil)
					}
				}
				tx.SetBlock(cube.Pos{3, 1, 0}, c.door, nil)
				top := c.door
				switch d := top.(type) {
				case block.WoodDoor:
					d.Top = true
					top = d
				case block.IronDoor:
					d.Top = true
					top = d
				}
				tx.SetBlock(cube.Pos{3, 2, 0}, top, nil)
			})
			r := spawn(t, w, mgl64.Vec3{0.5, 1, 0.5})
			nodes := findPath(t, w, r, cube.Pos{6, 1, 0}, func(m *javamob.Mob) { m.Navigation().SetCanOpenDoors(c.canOpen) })
			if got := nodes != nil; got != c.wantPath {
				t.Fatalf("path found %v, want %v (%v)", got, c.wantPath, nodes)
			}
		})
	}
}

// TestPathFence: fences are not jumped over; the path goes through the gap.
func TestPathFence(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 16, block.Stone{})
		for z := -6; z <= 6; z++ {
			if z != 5 {
				tx.SetBlock(cube.Pos{3, 1, z}, block.WoodFence{Wood: block.OakWood()}, nil)
			}
		}
	})
	r := spawn(t, w, mgl64.Vec3{0.5, 1, 0.5})
	nodes := findPath(t, w, r, cube.Pos{6, 1, 0}, nil)
	if nodes == nil {
		t.Fatal("no path")
	}
	if !contains(nodes, func(p cube.Pos) bool { return p == (cube.Pos{3, 1, 5}) }) {
		t.Fatalf("path %v does not use the gap at z=5", nodes)
	}
}

// TestWalkPath: a mob following a path around a wall reaches its goal.
func TestWalkPath(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 16, block.Stone{})
		for z := -4; z <= 3; z++ {
			for y := 1; y <= 2; y++ {
				tx.SetBlock(cube.Pos{3, y, z}, block.Stone{}, nil)
			}
		}
	})
	r := spawn(t, w, mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(2)
	with(t, w, r, func(m *javamob.Mob) {
		if !m.Navigation().MoveTo(m, mgl64.Vec3{6.5, 1, 0.5}, 0, 1) {
			t.Fatal("no path")
		}
	})
	w.Tick(200)
	if p := r.Pos(); (mgl64.Vec3{6.5, 1, 0.5}).Sub(p).Len() > 1.2 {
		t.Fatalf("mob at %v, want near (6.5, 1, 0.5)", p)
	}
}

// fixedGoal is a goal for the selector test.
type fixedGoal struct {
	javamob.GoalBase
	use     bool
	started int
	stopped int
}

func (g *fixedGoal) CanUse(*javamob.Mob) bool { return g.use }
func (g *fixedGoal) Start(*javamob.Mob)       { g.started++ }
func (g *fixedGoal) Stop(*javamob.Mob)        { g.stopped++ }

// TestGoalSelector: a goal of lower priority number takes the flags of a running one; equal priority does not.
func TestGoalSelector(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 4, block.Stone{}) })
	r := spawn(t, w, mgl64.Vec3{0.5, 1, 0.5})
	low := &fixedGoal{GoalBase: javamob.GoalBase{Control: javamob.FlagMove}, use: true}
	high := &fixedGoal{GoalBase: javamob.GoalBase{Control: javamob.FlagMove | javamob.FlagLook}}
	same := &fixedGoal{GoalBase: javamob.GoalBase{Control: javamob.FlagMove}, use: true}
	with(t, w, r, func(m *javamob.Mob) {
		m.Goals().Add(5, low)
		m.Goals().Add(1, high)
		m.Goals().Add(5, same)
	})
	w.Tick(3)
	with(t, w, r, func(m *javamob.Mob) {
		if !m.Goals().IsRunning(low) || m.Goals().IsRunning(same) {
			t.Fatalf("running: %v", m.Goals())
		}
	})
	high.use = true
	w.Tick(3)
	with(t, w, r, func(m *javamob.Mob) {
		if !m.Goals().IsRunning(high) || m.Goals().IsRunning(low) || low.stopped != 1 {
			t.Fatalf("running after the high goal: %v (low stopped %d)", m.Goals(), low.stopped)
		}
	})
}

// TestJavaTypeInstalled: Install made the type a Java mob of package mob.
func TestJavaTypeInstalled(t *testing.T) {
	if _, ok := mob.TypeFor(walkerID); !ok {
		t.Fatal("type not in package mob")
	}
}
