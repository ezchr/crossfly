package trace

import (
	"math"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

// within fails the test if f does not return within a few seconds.
func within(t *testing.T, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("did not return: hung on the input")
	}
}

func TestTraverseBlocksNonFinite(t *testing.T) {
	nan, inf := math.NaN(), math.Inf(1)
	for _, c := range [][2]mgl64.Vec3{
		{{0, 0, 0}, {nan, 0, 0}},
		{{nan, nan, nan}, {1, 2, 3}},
		{{0, 0, 0}, {0, -inf, 0}},
		{{-math.MaxFloat64, 0, 0}, {math.MaxFloat64, 0, 0}},
	} {
		n := 0
		within(t, func() { TraverseBlocks(c[0], c[1], func(cube.Pos) bool { n++; return true }) })
		if n > 3*MaxTraverseDistance+3 {
			t.Fatalf("%v -> %v visited %d blocks", c[0], c[1], n)
		}
	}
}

func TestTraverseBlocksFarEndCapped(t *testing.T) {
	n := 0
	within(t, func() {
		TraverseBlocks(mgl64.Vec3{0.5, 0.5, 0.5}, mgl64.Vec3{1e12, 0.5, 0.5}, func(cube.Pos) bool { n++; return true })
	})
	if n != MaxTraverseDistance+1 {
		t.Fatalf("visited %d blocks, want %d", n, MaxTraverseDistance+1)
	}
}

func TestTraverseBlocksSamePoint(t *testing.T) {
	var got []cube.Pos
	TraverseBlocks(mgl64.Vec3{1.5, 2.5, 3.5}, mgl64.Vec3{1.5, 2.5, 3.5}, func(p cube.Pos) bool { got = append(got, p); return true })
	if len(got) != 1 || got[0] != (cube.Pos{1, 2, 3}) {
		t.Fatalf("got %v, want only the start block", got)
	}
}
