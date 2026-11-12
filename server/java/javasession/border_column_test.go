package javasession

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

// playerBox is a Java player's box (0.6 wide, 1.8 tall) with its feet at pos.
func playerBox(pos mgl64.Vec3) cube.BBox {
	return cube.Box(-0.3, 0, -0.3, 0.3, 1.8, 0.3).Translate(pos)
}

// wallX is a border wall along Z in the column x = 5.
func wallX(x, _ int) bool { return x == 5 }

func near(a, b mgl64.Vec3) bool { return a.Sub(b).Len() < 1e-6 }

func TestBorderMoveStopsAtColumn(t *testing.T) {
	// Walking +X into the wall: stopped flush against it, Y kept.
	got := clipBorderMove(playerBox(mgl64.Vec3{4.5, 64, 0.5}), mgl64.Vec3{0.4, -0.1, 0}, wallX)
	if !near(got, mgl64.Vec3{0.2, -0.1, 0}) {
		t.Fatalf("into the wall: %v", got)
	}
	// From the other side, -X.
	got = clipBorderMove(playerBox(mgl64.Vec3{6.5, 64, 0.5}), mgl64.Vec3{-0.4, 0, 0}, wallX)
	if !near(got, mgl64.Vec3{-0.2, 0, 0}) {
		t.Fatalf("into the wall from +X: %v", got)
	}
	// A fast move right through the column (a sprint jump, elytra) is stopped too.
	got = clipBorderMove(playerBox(mgl64.Vec3{3.5, 64, 0.5}), mgl64.Vec3{3, 0, 0}, wallX)
	if !near(got, mgl64.Vec3{1.2, 0, 0}) {
		t.Fatalf("through the wall: %v", got)
	}
	// At every height: the column is a wall from the bottom to the top of the world and beyond.
	for _, y := range []float64{-64, 0, 320, 1000} {
		if got := clipBorderMove(playerBox(mgl64.Vec3{4.5, y, 0.5}), mgl64.Vec3{1, 0, 0}, wallX); got[0] > 0.2+1e-6 {
			t.Fatalf("y %v: %v", y, got)
		}
	}
}

func TestBorderMoveFlushStaysOut(t *testing.T) {
	// Flush against the wall, with float error either way: pushing in moves nothing.
	for _, x := range []float64{4.7, 4.7 - 1e-12, 4.7 + 1e-12, math.Nextafter(4.7, 5)} {
		got := clipBorderMove(playerBox(mgl64.Vec3{x, 64, 0.5}), mgl64.Vec3{0.3, 0, 0}, wallX)
		if end := x + got[0] + 0.3; end > 5+borderInside {
			t.Fatalf("flush at %v went in to %v", x, end)
		}
	}
}

func TestBorderMoveAlongWall(t *testing.T) {
	// Walking diagonally into the wall slides along it: X stops, Z is kept.
	pos := mgl64.Vec3{4.6, 64, 0.5}
	for i := 0; i < 20; i++ {
		d := clipBorderMove(playerBox(pos), mgl64.Vec3{0.2, 0, 0.2}, wallX)
		if math.Abs(d[2]-0.2) > 1e-9 {
			t.Fatalf("step %d: Z not kept: %v", i, d)
		}
		pos = pos.Add(d)
		if pos[0]+0.3 > 5+borderInside {
			t.Fatalf("step %d: in the wall at %v", i, pos)
		}
	}
	if !near(pos, mgl64.Vec3{4.7, 64, 4.5}) {
		t.Fatalf("ended at %v", pos)
	}
	// Walking straight along it, flush, is not touched at all.
	d := clipBorderMove(playerBox(mgl64.Vec3{4.7, 64, 0.5}), mgl64.Vec3{0, 0, 0.3}, wallX)
	if d != (mgl64.Vec3{0, 0, 0.3}) {
		t.Fatalf("along the wall: %v", d)
	}
}

func TestBorderMoveCorner(t *testing.T) {
	// A single column met diagonally at its corner: never through it.
	one := func(x, z int) bool { return x == 5 && z == 5 }
	pos := mgl64.Vec3{4.6, 64, 4.6}
	d := clipBorderMove(playerBox(pos), mgl64.Vec3{0.3, 0, 0.3}, one)
	if inBorderColumn(playerBox(pos.Add(d)), 5, 5) {
		t.Fatalf("entered the corner column: %v", d)
	}
}

func TestBorderMoveInsideWalksOut(t *testing.T) {
	// Already inside a column (a border block placed on the player, a teleport): free to move, no pull back.
	pos := mgl64.Vec3{5.5, 64, 0.5}
	for _, d := range []mgl64.Vec3{{0.3, 0, 0}, {-0.3, 0, 0}, {0, 0, 0.3}} {
		if got := clipBorderMove(playerBox(pos), d, wallX); got != d {
			t.Fatalf("inside, %v: %v", d, got)
		}
	}
	// Outside, next to a second wall, walking into it stops.
	two := func(x, _ int) bool { return x == 5 || x == 7 }
	got := clipBorderMove(playerBox(mgl64.Vec3{6.5, 64, 0.5}), mgl64.Vec3{0.5, 0, 0}, two)
	if !near(got, mgl64.Vec3{0.2, 0, 0}) {
		t.Fatalf("out into the next wall: %v", got)
	}
}

func TestBorderMoveNoBorder(t *testing.T) {
	none := func(int, int) bool { return false }
	d := mgl64.Vec3{0.7, 0.42, -0.3}
	if got := clipBorderMove(playerBox(mgl64.Vec3{4.5, 64, 0.5}), d, none); got != d {
		t.Fatalf("no border: %v", got)
	}
	// A vertical move is never clipped, even next to the wall.
	d = mgl64.Vec3{0, -3, 0}
	if got := clipBorderMove(playerBox(mgl64.Vec3{4.7, 64, 0.5}), d, wallX); got != d {
		t.Fatalf("falling along the wall: %v", got)
	}
}
