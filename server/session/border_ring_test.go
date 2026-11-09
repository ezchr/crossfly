package session

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

func TestBorderRingCellInRange(t *testing.T) {
	const from, to = 31.0, 408.0
	cur, shown := 0, false
	check := func(v float64) {
		cur, shown = borderRingCell(from, to, v, cur, shown), true
		if d := math.Abs(v - world.BorderRingAnchor(from, cur)); d > 25 {
			t.Fatalf("at %v the anchor %d is %v away, outside its box", v, cur, d)
		}
	}
	for v := from - 4; v <= to+4; v += 0.25 {
		check(v)
	}
	for v := to + 4; v >= from-4; v -= 0.25 {
		check(v)
	}
	for v := from - 4; v <= to+4; v += 37 {
		cur, shown = 0, false
		check(v)
	}
}

func TestBorderRingKeepsAnchor(t *testing.T) {
	const from, to = 31.0, 408.0
	edge := world.BorderRingAnchor(from, 0) + world.BorderRingCell/2
	cur := borderRingCell(from, to, edge-1, 0, false)
	swaps := 0
	for i := range 100 {
		v := edge - 1 + float64(i%2)*2
		if n := borderRingCell(from, to, v, cur, true); n != cur {
			cur = n
			swaps++
		}
	}
	if swaps != 0 {
		t.Fatalf("%d swaps walking to and fro on a line", swaps)
	}
}

func TestBorderRingLevelInRange(t *testing.T) {
	cur, shown := 0, false
	for y := -24.0; y <= 310; y += 0.25 {
		cur, shown = borderRingLevel(y, cur, shown), true
		if d := math.Abs(y - float64(cur*borderRingStep)); d > 25 {
			t.Fatalf("at y %v level %d is %v away, outside its box", y, cur, d)
		}
	}
}
