//go:build crossfly_full

package nav

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// The tests build small structures in a synchronous, generator-less world and check the paths the
// port finds. Where a test pins an exact node list, the list is the port's output for that layout,
// checked by hand against the BDS rules it exercises.

var (
	ground = block.Dirt{}
	stone  = block.Cobblestone{}
)

func withWorld(t *testing.T, f func(tx *world.Tx)) {
	t.Helper()
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	w.Do(f)
}

func fill(tx *world.Tx, x0, y0, z0, x1, y1, z1 int, b world.Block) {
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z++ {
				tx.SetBlock(cube.Pos{x, y, z}, b, nil)
			}
		}
	}
}

// floor builds a dirt floor at y=0 over the square [-r, r].
func floorAt(tx *world.Tx, r int) { fill(tx, -r, 0, -r, r, 0, r, ground) }

func zombie(pos mgl64.Vec3) Request {
	return Request{
		Pos: pos, Width: 0.6, Height: 1.9, Walker: true,
		Options: DefaultOptions(),
		Traits:  Traits{StepHeight: 0.5625, FollowRange: 35},
	}
}

func spiderReq(pos mgl64.Vec3) Request {
	r := zombie(pos)
	r.Width, r.Height = 1.4, 0.9
	return r
}

func pathString(p *Path) string {
	if p == nil {
		return "<nil>"
	}
	var sb strings.Builder
	for i, n := range p.Nodes() {
		if i > 0 {
			sb.WriteString(" ")
		}
		fmt.Fprintf(&sb, "(%d,%d,%d)%s", n.Pos[0], n.Pos[1], n.Pos[2], n.Type)
	}
	if p.Complete() {
		sb.WriteString(" [complete]")
	} else {
		sb.WriteString(" [partial]")
	}
	return sb.String()
}

func find(t *testing.T, tx *world.Tx, r Request, target cube.Pos) *Path {
	t.Helper()
	f := NewFinder()
	p := f.FindPathToBlock(tx, r, target, r.maxDist())
	t.Logf("%s: %d expansions, %d blocks read", pathString(p), f.Expansions, f.Lookups)
	return p
}

func mustComplete(t *testing.T, p *Path, target cube.Pos) {
	t.Helper()
	if p == nil || !p.Complete() {
		t.Fatalf("expected a complete path, got %s", pathString(p))
	}
	if p.LastPos() != target {
		t.Fatalf("path ends at %v, want %v", p.LastPos(), target)
	}
}

func mustNotComplete(t *testing.T, p *Path) {
	t.Helper()
	if p != nil && p.Complete() {
		t.Fatalf("expected no complete path, got %s", pathString(p))
	}
}

func TestFlatLine(t *testing.T) {
	withWorld(t, func(tx *world.Tx) {
		floorAt(tx, 12)
		p := find(t, tx, zombie(mgl64.Vec3{0.5, 1, 0.5}), cube.Pos{6, 1, 0})
		mustComplete(t, p, cube.Pos{6, 1, 0})
		want := "(0,1,0)walkable (1,1,0)open (2,1,0)open (3,1,0)open (4,1,0)open (5,1,0)open (6,1,0)open [complete]"
		if got := pathString(p); got != want {
			t.Fatalf("got  %s\nwant %s", got, want)
		}
	})
}

func TestDiagonal(t *testing.T) {
	withWorld(t, func(tx *world.Tx) {
		floorAt(tx, 12)
		p := find(t, tx, zombie(mgl64.Vec3{0.5, 1, 0.5}), cube.Pos{4, 1, 4})
		mustComplete(t, p, cube.Pos{4, 1, 4})
		if p.Len() != 5 {
			t.Fatalf("expected a straight diagonal of 5 nodes, got %s", pathString(p))
		}
	})
}

func TestAroundWall(t *testing.T) {
	withWorld(t, func(tx *world.Tx) {
		floorAt(tx, 16)
		fill(tx, 4, 1, -4, 4, 3, 4, stone)
		p := find(t, tx, zombie(mgl64.Vec3{0.5, 1, 0.5}), cube.Pos{8, 1, 0})
		mustComplete(t, p, cube.Pos{8, 1, 0})
		for _, n := range p.Nodes() {
			if n.Pos[0] == 4 && n.Pos[2] >= -4 && n.Pos[2] <= 4 {
				t.Fatalf("path goes through the wall at %v", n.Pos)
			}
		}
	})
}

func TestOneBlockStep(t *testing.T) {
	withWorld(t, func(tx *world.Tx) {
		floorAt(tx, 12)
		fill(tx, 3, 1, -12, 12, 1, 12, stone)
		p := find(t, tx, zombie(mgl64.Vec3{0.5, 1, 0.5}), cube.Pos{6, 2, 0})
		mustComplete(t, p, cube.Pos{6, 2, 0})
		want := "(0,1,0)walkable (1,1,0)open (2,1,0)open (3,2,0)open (4,2,0)open (5,2,0)open (6,2,0)open [complete]"
		if got := pathString(p); got != want {
			t.Fatalf("got  %s\nwant %s", got, want)
		}
	})
}

func TestTwoBlockStepIsTooHigh(t *testing.T) {
	withWorld(t, func(tx *world.Tx) {
		floorAt(tx, 12)
		// A two block high cliff across the whole floor: the only way up is a jump of 2.
		fill(tx, 3, 1, -12, 12, 2, 12, stone)
		p := find(t, tx, zombie(mgl64.Vec3{0.5, 1, 0.5}), cube.Pos{6, 3, 0})
		mustNotComplete(t, p)
		for _, n := range p.Nodes() {
			if n.Pos[1] > 1 {
				t.Fatalf("path climbs the 2 block step at %v", n.Pos)
			}
		}
	})
}

func TestStairs(t *testing.T) {
	withWorld(t, func(tx *world.Tx) {
		floorAt(tx, 12)
		// A staircase up three levels to a platform at y=3 (stand at y=4). Each stair has full
		// blocks below it.
		for i := 0; i < 3; i++ {
			x := 3 + i
			fill(tx, x, 1, -1, x, i, 1, stone)
			fill(tx, x, 1+i, -1, x, 1+i, 1, block.Stairs{Block: block.Planks{Wood: block.OakWood()}, Facing: cube.East})
		}
		fill(tx, 6, 1, -1, 9, 3, 1, stone)
		// Walls on both sides so the stairs are the only way.
		fill(tx, 0, 1, -2, 9, 5, -2, stone)
		fill(tx, 0, 1, 2, 9, 5, 2, stone)
		p := find(t, tx, zombie(mgl64.Vec3{0.5, 1, 0.5}), cube.Pos{8, 4, 0})
		mustComplete(t, p, cube.Pos{8, 4, 0})
		for _, n := range p.Nodes() {
			if n.Type == NodeStairs || n.Type < 0 {
				t.Fatalf("stairs node in path: %s", pathString(p))
			}
		}
	})
}

func TestSlabs(t *testing.T) {
	withWorld(t, func(tx *world.Tx) {
		floorAt(tx, 12)
		slab := block.Slab{Block: block.Planks{Wood: block.OakWood()}}
		// Bottom slab, then a full block with a bottom slab on it: a half-block staircase.
		fill(tx, 3, 1, -12, 3, 1, 12, slab)
		fill(tx, 4, 1, -12, 12, 1, 12, stone)
		fill(tx, 4, 2, -12, 12, 2, 12, slab)
		p := find(t, tx, zombie(mgl64.Vec3{0.5, 1, 0.5}), cube.Pos{6, 2, 0})
		mustComplete(t, p, cube.Pos{6, 2, 0})
		want := "(0,1,0)walkable (1,1,0)open (2,1,0)open (3,1,0)open (4,2,0)open (5,2,0)open (6,2,0)open [complete]"
		if got := pathString(p); got != want {
			t.Fatalf("got  %s\nwant %s", got, want)
		}
	})
}

// corridor builds an east-west corridor at z=-1..1 with 3 high walls, floor at y=0.
func corridor(tx *world.Tx, length int) {
	fill(tx, -2, 0, -3, length, 0, 3, ground)
	fill(tx, -2, 1, -2, length, 3, -2, stone)
	fill(tx, -2, 1, 2, length, 3, 2, stone)
	fill(tx, -2, 1, -1, -2, 3, 1, stone)
}

func TestFenceBlocks(t *testing.T) {
	withWorld(t, func(tx *world.Tx) {
		corridor(tx, 12)
		fill(tx, 5, 1, -1, 5, 1, 1, block.WoodFence{Wood: block.OakWood()})
		p := find(t, tx, zombie(mgl64.Vec3{0.5, 1, 0.5}), cube.Pos{9, 1, 0})
		mustNotComplete(t, p)
		for _, n := range p.Nodes() {
			if n.Pos[0] >= 5 {
				t.Fatalf("path crosses the fence at %v", n.Pos)
			}
		}
	})
}

func TestFenceGate(t *testing.T) {
	for _, open := range []bool{false, true} {
		t.Run(fmt.Sprintf("open=%v", open), func(t *testing.T) {
			withWorld(t, func(tx *world.Tx) {
				corridor(tx, 12)
				fill(tx, 5, 1, -1, 5, 1, 1, block.WoodFence{Wood: block.OakWood()})
				tx.SetBlock(cube.Pos{5, 1, 0}, block.WoodFenceGate{Wood: block.OakWood(), Facing: cube.East, Open: open}, nil)
				p := find(t, tx, zombie(mgl64.Vec3{0.5, 1, 0.5}), cube.Pos{9, 1, 0})
				if open {
					mustComplete(t, p, cube.Pos{9, 1, 0})
				} else {
					mustNotComplete(t, p)
				}
			})
		})
	}
}

func TestDoors(t *testing.T) {
	cases := []struct {
		name     string
		open     bool
		canOpen  bool
		complete bool
	}{
		{"closed", false, false, false},
		{"closed, can_open_doors", false, true, true},
		{"open", true, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withWorld(t, func(tx *world.Tx) {
				corridor(tx, 12)
				fill(tx, 5, 1, -1, 5, 3, 1, stone)
				door := block.WoodDoor{Wood: block.OakWood(), Facing: cube.East, Open: c.open}
				tx.SetBlock(cube.Pos{5, 1, 0}, door, nil)
				door.Top = true
				tx.SetBlock(cube.Pos{5, 2, 0}, door, nil)
				r := zombie(mgl64.Vec3{0.5, 1, 0.5})
				r.Options.CanOpenDoors = c.canOpen
				p := find(t, tx, r, cube.Pos{9, 1, 0})
				if c.complete {
					mustComplete(t, p, cube.Pos{9, 1, 0})
				} else {
					mustNotComplete(t, p)
				}
			})
		})
	}
}

func TestIronDoorNeedsIronFlag(t *testing.T) {
	withWorld(t, func(tx *world.Tx) {
		corridor(tx, 12)
		fill(tx, 5, 1, -1, 5, 3, 1, stone)
		tx.SetBlock(cube.Pos{5, 1, 0}, block.IronDoor{Facing: cube.East}, nil)
		tx.SetBlock(cube.Pos{5, 2, 0}, block.IronDoor{Facing: cube.East, Top: true}, nil)
		r := zombie(mgl64.Vec3{0.5, 1, 0.5})
		r.Options.CanOpenDoors = true
		r.Traits.DoorOpener = true
		mustNotComplete(t, find(t, tx, r, cube.Pos{9, 1, 0}))
		r.Options.CanOpenIronDoors = true
		mustComplete(t, find(t, tx, r, cube.Pos{9, 1, 0}), cube.Pos{9, 1, 0})
	})
}

// platform builds a pillar platform x<=4 of the given height above the y=0 floor.
func dropWorld(tx *world.Tx, height int) {
	floorAt(tx, 12)
	fill(tx, -12, 1, -12, 4, height, 12, stone)
}

func TestThreeBlockDrop(t *testing.T) {
	withWorld(t, func(tx *world.Tx) {
		dropWorld(tx, 3)
		p := find(t, tx, zombie(mgl64.Vec3{0.5, 4, 0.5}), cube.Pos{8, 1, 0})
		mustComplete(t, p, cube.Pos{8, 1, 0})
		// The node off the ledge lands directly on the ground.
		for i, n := range p.Nodes() {
			if n.Pos[0] == 5 && n.Pos[1] != 1 {
				t.Fatalf("node %d at %v did not fall to the ground", i, n.Pos)
			}
		}
	})
}

func TestFourBlockDropIsTooFar(t *testing.T) {
	withWorld(t, func(tx *world.Tx) {
		dropWorld(tx, 4)
		p := find(t, tx, zombie(mgl64.Vec3{0.5, 5, 0.5}), cube.Pos{8, 1, 0})
		mustNotComplete(t, p)
	})
}

func TestPreferredPathMaxFall(t *testing.T) {
	withWorld(t, func(tx *world.Tx) {
		dropWorld(tx, 4)
		r := zombie(mgl64.Vec3{0.5, 5, 0.5})
		r.Traits.Preferred = &PreferredPath{MaxFallBlocks: 5}
		mustComplete(t, find(t, tx, r, cube.Pos{8, 1, 0}), cube.Pos{8, 1, 0})
	})
}

func poolWorld(tx *world.Tx, liquid world.Block) {
	floorAt(tx, 14)
	fill(tx, 3, 0, -3, 7, 0, 3, liquid)
	fill(tx, 3, -1, -3, 7, -1, 3, ground)
}

func overPool(p cube.Pos) bool { return p[0] >= 3 && p[0] <= 7 && p[2] >= -3 && p[2] <= 3 }

func TestWater(t *testing.T) {
	water := block.Water{Still: true, Depth: 8}
	t.Run("walks over", func(t *testing.T) {
		withWorld(t, func(tx *world.Tx) {
			poolWorld(tx, water)
			p := find(t, tx, zombie(mgl64.Vec3{0.5, 1, 0.5}), cube.Pos{10, 1, 0})
			mustComplete(t, p, cube.Pos{10, 1, 0})
			over := false
			for _, n := range p.Nodes() {
				over = over || overPool(n.Pos)
			}
			if !over {
				t.Fatalf("expected the straight path over the water, got %s", pathString(p))
			}
		})
	})
	t.Run("avoid_water", func(t *testing.T) {
		withWorld(t, func(tx *world.Tx) {
			poolWorld(tx, water)
			r := zombie(mgl64.Vec3{0.5, 1, 0.5})
			r.Options.AvoidWater = true
			p := find(t, tx, r, cube.Pos{10, 1, 0})
			mustComplete(t, p, cube.Pos{10, 1, 0})
			for _, n := range p.Nodes() {
				if overPool(n.Pos) {
					t.Fatalf("avoid_water path over the pool at %v", n.Pos)
				}
			}
		})
	})
}

func TestLava(t *testing.T) {
	withWorld(t, func(tx *world.Tx) {
		poolWorld(tx, block.Lava{Still: true, Depth: 8})
		p := find(t, tx, zombie(mgl64.Vec3{0.5, 1, 0.5}), cube.Pos{10, 1, 0})
		mustComplete(t, p, cube.Pos{10, 1, 0})
		for _, n := range p.Nodes() {
			if overPool(n.Pos) {
				t.Fatalf("path over lava at %v", n.Pos)
			}
		}
	})
}

func TestCactus(t *testing.T) {
	withWorld(t, func(tx *world.Tx) {
		floorAt(tx, 14)
		fill(tx, 5, 1, -4, 5, 1, 4, block.Cactus{})
		p := find(t, tx, zombie(mgl64.Vec3{0.5, 1, 0.5}), cube.Pos{9, 1, 0})
		mustComplete(t, p, cube.Pos{9, 1, 0})
		for _, n := range p.Nodes() {
			if n.Pos[0] == 5 && n.Pos[2] >= -4 && n.Pos[2] <= 4 {
				t.Fatalf("path over the cactus at %v", n.Pos)
			}
		}
	})
}

func TestMagmaIsHot(t *testing.T) {
	withWorld(t, func(tx *world.Tx) {
		corridor(tx, 12)
		fill(tx, 4, 0, -1, 6, 0, 1, block.Magma{})
		p := find(t, tx, zombie(mgl64.Vec3{0.5, 1, 0.5}), cube.Pos{9, 1, 0})
		mustNotComplete(t, p)
		r := zombie(mgl64.Vec3{0.5, 1, 0.5})
		r.Traits.FireImmune = true
		mustComplete(t, find(t, tx, r, cube.Pos{9, 1, 0}), cube.Pos{9, 1, 0})
	})
}

func TestBlocksToAvoid(t *testing.T) {
	withWorld(t, func(tx *world.Tx) {
		corridor(tx, 12)
		fill(tx, 5, 1, -1, 5, 1, 1, block.Carpet{Colour: item.ColourWhite()})
		r := zombie(mgl64.Vec3{0.5, 1, 0.5})
		mustComplete(t, find(t, tx, r, cube.Pos{9, 1, 0}), cube.Pos{9, 1, 0})
		opts, err := ParseOptions([]byte(`{"blocks_to_avoid":[{"name":"minecraft:white_carpet"}]}`))
		if err != nil {
			t.Fatal(err)
		}
		r.Options = opts
		mustNotComplete(t, find(t, tx, r, cube.Pos{9, 1, 0}))
	})
}

func TestExpansionCap(t *testing.T) {
	withWorld(t, func(tx *world.Tx) {
		floorAt(tx, 30)
		// A long U-shaped wall around the start makes the goal unreachable within 140 expansions.
		fill(tx, 3, 1, -20, 3, 3, 20, stone)
		f := NewFinder()
		r := zombie(mgl64.Vec3{0.5, 1, 0.5})
		r.Traits.FollowRange = 64
		p := f.FindPathToBlock(tx, r, cube.Pos{12, 1, 0}, 64)
		if f.Expansions != maxExpansions {
			t.Fatalf("expected the search to stop at %d expansions, got %d", maxExpansions, f.Expansions)
		}
		mustNotComplete(t, p)
		if p == nil || !p.Valid() {
			t.Fatalf("expected a partial path to the closest node, got %s", pathString(p))
		}
	})
}

func TestFollowRange(t *testing.T) {
	withWorld(t, func(tx *world.Tx) {
		floorAt(tx, 30)
		r := zombie(mgl64.Vec3{0.5, 1, 0.5})
		r.Traits.FollowRange = 4
		// The start is 10 blocks from the goal: every neighbour is out of range of the goal.
		p := NewFinder().FindPathToBlock(tx, r, cube.Pos{10, 1, 0}, r.maxDist())
		if p != nil {
			t.Fatalf("expected no path, got %s", pathString(p))
		}
	})
}

func TestSpiderFootprint(t *testing.T) {
	withWorld(t, func(tx *world.Tx) {
		corridor(tx, 12)
		// A 1 wide gap in a wall across the corridor: a zombie fits, a 1.4 wide spider does not.
		fill(tx, 5, 1, -1, 5, 3, 1, stone)
		tx.SetBlock(cube.Pos{5, 1, 0}, block.Air{}, nil)
		tx.SetBlock(cube.Pos{5, 2, 0}, block.Air{}, nil)
		mustComplete(t, find(t, tx, zombie(mgl64.Vec3{0.5, 1, 0.5}), cube.Pos{9, 1, 0}), cube.Pos{9, 1, 0})
		mustNotComplete(t, find(t, tx, spiderReq(mgl64.Vec3{0, 1, 0}), cube.Pos{9, 1, 0}))
	})
}

func TestFlier(t *testing.T) {
	withWorld(t, func(tx *world.Tx) {
		floorAt(tx, 12)
		fill(tx, 4, 1, -12, 4, 3, 12, stone)
		r := Request{Pos: mgl64.Vec3{0.5, 3, 0.5}, Width: 0.55, Height: 0.5, Flier: true, Options: DefaultOptions(), Traits: Traits{FollowRange: 24}}
		p := find(t, tx, r, cube.Pos{8, 3, 0})
		mustComplete(t, p, cube.Pos{8, 3, 0})
		maxY := 0
		for _, n := range p.Nodes() {
			if n.Type != NodeFlyable && n.Type != NodeOpen {
				t.Fatalf("flier node of type %v", n.Type)
			}
			maxY = max(maxY, n.Pos[1])
		}
		if maxY < 4 {
			t.Fatalf("flier did not go over the wall: %s", pathString(p))
		}
	})
}

func TestSwimmer(t *testing.T) {
	withWorld(t, func(tx *world.Tx) {
		fill(tx, -6, -6, -6, 12, -6, 6, ground)
		fill(tx, -6, -5, -6, 12, 0, 6, block.Water{Still: true, Depth: 8})
		fill(tx, 4, -5, -6, 4, -1, 6, stone)
		r := Request{Pos: mgl64.Vec3{0.5, -3, 0.5}, Width: 0.5, Height: 0.3, Swimmer: true, InWater: true, Options: DefaultOptions(), Traits: Traits{FollowRange: 24}}
		p := find(t, tx, r, cube.Pos{8, -3, 0})
		mustComplete(t, p, cube.Pos{8, -3, 0})
		for _, n := range p.Nodes() {
			if n.Pos[0] == 4 && n.Pos[1] <= -1 {
				t.Fatalf("swimmer path through the wall at %v", n.Pos)
			}
		}
	})
}

// fakeMob is a minimal Agent with a crude movement model, used to drive Navigation.Tick.
type fakeMob struct {
	pos            mgl64.Vec3
	w, h           float64
	onGround       bool
	horizontal     bool
	climbing       bool
	climbed        bool
	wanted         mgl64.Vec3
	hasWanted      bool
	speed          float64
	solid          func(cube.Pos) bool
	stepsWithWants int
}

func (m *fakeMob) Position() mgl64.Vec3      { return m.pos }
func (m *fakeMob) Size() (float64, float64)  { return m.w, m.h }
func (m *fakeMob) OnGround() bool            { return m.onGround }
func (m *fakeMob) InWater() bool             { return false }
func (m *fakeMob) InLava() bool              { return false }
func (m *fakeMob) HorizontalCollision() bool { return m.horizontal }
func (m *fakeMob) SetWantedPosition(p mgl64.Vec3, s float64) {
	m.wanted, m.hasWanted, m.speed = p, true, s
}
func (m *fakeMob) ClearWantedPosition() { m.hasWanted = false }
func (m *fakeMob) SetClimbing(c bool) {
	m.climbing = c
	m.climbed = m.climbed || c
}

// step moves the mob 0.25 blocks towards the wanted position, stopping at solid blocks, climbing
// when the navigation says so and falling otherwise.
func (m *fakeMob) step() {
	m.horizontal = false
	if m.hasWanted {
		m.stepsWithWants++
		d := mgl64.Vec3{m.wanted[0] - m.pos[0], 0, m.wanted[2] - m.pos[2]}
		if l := d.Len(); l > 0.25 {
			d = d.Mul(0.25 / l)
		}
		// Move, sliding along walls one axis at a time.
		for _, mv := range []mgl64.Vec3{d, {d[0], 0, 0}, {0, 0, d[2]}} {
			if next := m.pos.Add(mv); !m.blocked(next) {
				m.pos = next
				break
			}
			m.horizontal = true
		}
		if m.wanted[1] > m.pos[1]+0.5 && m.onGround && !m.blocked(m.pos.Add(mgl64.Vec3{0, 1.1, 0})) {
			m.pos[1] += 1 // jump up a step
		}
	}
	switch {
	case m.climbing:
		m.pos[1] += 0.2
	case !m.blocked(m.pos.Add(mgl64.Vec3{0, -0.05, 0})):
		m.pos[1] = math.Max(math.Floor(m.pos[1]-0.4), m.pos[1]-0.4)
	}
	m.onGround = m.blocked(m.pos.Add(mgl64.Vec3{0, -0.05, 0}))
	if m.onGround {
		m.pos[1] = math.Ceil(m.pos[1] - 0.05)
	}
}

func (m *fakeMob) blocked(p mgl64.Vec3) bool {
	for _, x := range []float64{p[0] - m.w/2 + 0.01, p[0] + m.w/2 - 0.01} {
		for _, z := range []float64{p[2] - m.w/2 + 0.01, p[2] + m.w/2 - 0.01} {
			for _, y := range []float64{p[1] + 0.01, p[1] + m.h - 0.01} {
				if m.solid(cube.PosFromVec3(mgl64.Vec3{x, y, z})) {
					return true
				}
			}
		}
	}
	return false
}

type target mgl64.Vec3

func (t target) Position() mgl64.Vec3 { return mgl64.Vec3(t) }

func solidIn(tx *world.Tx) func(cube.Pos) bool {
	return func(p cube.Pos) bool {
		_, air := tx.Block(p).(block.Air)
		return !air
	}
}

func TestNavigationWalksPath(t *testing.T) {
	withWorld(t, func(tx *world.Tx) {
		floorAt(tx, 16)
		fill(tx, 4, 1, -3, 4, 3, 3, stone)
		m := &fakeMob{pos: mgl64.Vec3{0.5, 1, 0.5}, w: 0.6, h: 1.9, onGround: true, solid: solidIn(tx)}
		n := New(KindWalk, DefaultOptions())
		n.Traits = Traits{StepHeight: 0.5625, FollowRange: 35}
		if !n.MoveTo(tx, m, mgl64.Vec3{8.5, 1, 0.5}, 1) {
			t.Fatal("MoveTo failed")
		}
		for i := 0; i < 400 && !n.Done(); i++ {
			n.Tick(tx, m)
			m.step()
		}
		if !n.Done() {
			t.Fatalf("navigation not done, mob at %v, path %s", m.pos, pathString(n.Path()))
		}
		if d := distSq(m.pos, mgl64.Vec3{8.5, 1, 0.5}); d > 1 {
			t.Fatalf("mob ended at %v, far from the target", m.pos)
		}
	})
}

func TestNavigationStuck(t *testing.T) {
	withWorld(t, func(tx *world.Tx) {
		floorAt(tx, 16)
		m := &fakeMob{pos: mgl64.Vec3{0.5, 1, 0.5}, w: 0.6, h: 1.9, onGround: true, solid: solidIn(tx)}
		n := New(KindWalk, DefaultOptions())
		n.Traits = Traits{StepHeight: 0.5625, FollowRange: 35}
		if !n.MoveTo(tx, m, mgl64.Vec3{8.5, 1, 0.5}, 1) {
			t.Fatal("MoveTo failed")
		}
		// The mob never moves: the path must be dropped after the 100 tick stuck interval.
		ticks := 0
		for ; ticks < 300 && !n.Done(); ticks++ {
			n.Tick(tx, m)
		}
		if !n.Done() || ticks < 100 || ticks > 103 {
			t.Fatalf("expected the stuck check to stop the path after ~101 ticks, done=%v ticks=%d", n.Done(), ticks)
		}
	})
}

func TestSpiderClimbsWall(t *testing.T) {
	withWorld(t, func(tx *world.Tx) {
		floorAt(tx, 16)
		// A 4 high wall with a platform on top; the target stands on it.
		fill(tx, 5, 1, -16, 12, 4, 16, stone)
		m := &fakeMob{pos: mgl64.Vec3{0.5, 1, 0.5}, w: 1.4, h: 0.9, onGround: true, solid: solidIn(tx)}
		n := New(KindClimb, DefaultOptions())
		n.Traits = Traits{StepHeight: 0.5625, FollowRange: 16}
		tgt := target{8.5, 5, 0.5}
		if !n.MoveToEntity(tx, m, tgt, 1) {
			t.Fatal("MoveToEntity failed")
		}
		for i := 0; i < 400 && m.pos[1] < 5; i++ {
			n.Tick(tx, m)
			m.step()
			if n.Done() && !n.breach && m.pos[1] < 5 {
				n.MoveToEntity(tx, m, tgt, 1)
			}
		}
		if !m.climbed {
			t.Fatal("spider never climbed")
		}
		if m.pos[1] < 5 {
			t.Fatalf("spider did not get on top of the wall, at %v", m.pos)
		}
	})
}

// tickedSource is a BlockSource with a tick counter the test controls.
type tickedSource struct {
	*world.Tx
	tick int64
}

func (s *tickedSource) CurrentTick() int64 { return s.tick }

func TestBudgetDefersSearches(t *testing.T) {
	withWorld(t, func(tx *world.Tx) {
		floorAt(tx, 16)
		src := &tickedSource{Tx: tx}
		b := NewBudget(1, 0)
		a := &fakeMob{pos: mgl64.Vec3{0.5, 1, 0.5}, w: 0.6, h: 1.9, onGround: true, solid: solidIn(tx)}
		c := &fakeMob{pos: mgl64.Vec3{0.5, 1, 2.5}, w: 0.6, h: 1.9, onGround: true, solid: solidIn(tx)}
		na, nc := New(KindWalk, DefaultOptions()), New(KindWalk, DefaultOptions())
		na.Budget, nc.Budget = b, b
		if !na.MoveTo(src, a, mgl64.Vec3{6.5, 1, 0.5}, 1) || na.Pending() {
			t.Fatal("first search should run")
		}
		if !nc.MoveTo(src, c, mgl64.Vec3{6.5, 1, 2.5}, 1) || !nc.Pending() {
			t.Fatal("second search should be deferred")
		}
		if nc.Done() {
			t.Fatal("a deferred request is not done")
		}
		nc.Tick(src, c)
		if !nc.Pending() {
			t.Fatal("budget should still be used up in the same tick")
		}
		src.tick++
		nc.Tick(src, c)
		if nc.Pending() || nc.Path() == nil || !nc.Path().Complete() {
			t.Fatalf("deferred search did not run: pending=%v path=%s", nc.Pending(), pathString(nc.Path()))
		}
		if _, searches, _, _ := b.Stats(); searches != 1 {
			t.Fatalf("expected 1 search in the new tick, got %d", searches)
		}
	})
}

func TestParseOptions(t *testing.T) {
	o, err := ParseOptions([]byte(`{"can_float": true, "avoid_sun": true, "can_pass_doors": false, "blocks_to_avoid": ["cactus", {"name": "minecraft:sweet_berry_bush"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !o.CanFloat || !o.AvoidSun || o.CanPassDoors || !o.CanSink || !o.CanJump || !o.CanWalk {
		t.Fatalf("unexpected options %+v", o)
	}
	if len(o.BlocksToAvoid) != 2 || o.BlocksToAvoid[0].Name != "minecraft:cactus" {
		t.Fatalf("unexpected blocks_to_avoid %+v", o.BlocksToAvoid)
	}
	o, _ = ParseOptions([]byte(`{"can_float": true, "can_path_over_water": false}`))
	if o.CanFloat {
		t.Fatal("can_path_over_water should override can_float")
	}
	pp, err := ParsePreferredPath([]byte(`{"max_fall_blocks": 1, "jump_cost": 5, "default_block_cost": 1.5, "preferred_path_blocks": [{"cost": 0, "blocks": ["grass_path"]}]}`))
	if err != nil || pp.MaxFallBlocks != 1 || pp.JumpCost != 5 || pp.DefaultBlockCost != 1.5 || len(pp.Blocks) != 1 {
		t.Fatalf("unexpected preferred path %+v %v", pp, err)
	}
}

func TestAvoidSunTrimsPath(t *testing.T) {
	withWorld(t, func(tx *world.Tx) {
		floorAt(tx, 16)
		// A roof over the start; the target is in the open.
		fill(tx, -4, 4, -4, 3, 4, 4, stone)
		m := &fakeMob{pos: mgl64.Vec3{0.5, 1, 0.5}, w: 0.6, h: 1.9, onGround: true, solid: solidIn(tx)}
		opts := DefaultOptions()
		opts.AvoidSun = true
		n := New(KindWalk, opts)
		n.Traits = Traits{StepHeight: 0.5625, FollowRange: 35}
		if !n.MoveTo(tx, m, mgl64.Vec3{9.5, 1, 0.5}, 1) {
			t.Fatal("MoveTo failed")
		}
		t.Logf("trimmed: %s", pathString(n.Path()))
		for _, nd := range n.Path().Nodes() {
			if nd.Pos[1] > tx.HighestLightBlocker(nd.Pos[0], nd.Pos[2]) {
				t.Fatalf("node %v is under the open sky", nd.Pos)
			}
		}
		if n.Path().LastPos()[0] >= 4 {
			t.Fatalf("path not trimmed: %s", pathString(n.Path()))
		}
	})
}
